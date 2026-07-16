package tasks

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// ErrStateLockBusy indicates another live process holds the task state lock.
var ErrStateLockBusy = errors.New("task state update in progress")

const (
	stateLockRetries    = 100
	stateLockRetryDelay = 5 * time.Millisecond
	stateLockFileName   = "tasks-state.lock"
)

// StateLockMetadata is persisted in the global task state lock file.
type StateLockMetadata struct {
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
}

// StateLock is a held global task state lock.
type StateLock struct {
	path string
}

// Release removes the task state lock file.
func (l *StateLock) Release() error {
	if l == nil || l.path == "" {
		return nil
	}
	err := os.Remove(l.path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// StateLockPathWith returns the global task state lock file path.
func StateLockPathWith(d *Deps) string {
	if xdgData := d.FS.Getenv("XDG_DATA_HOME"); xdgData != "" {
		return filepath.Join(xdgData, "pop", stateLockFileName)
	}
	home, err := d.FS.UserHomeDir()
	if err != nil {
		return filepath.Join("/tmp", "pop", stateLockFileName)
	}
	return filepath.Join(home, ".local", "share", "pop", stateLockFileName)
}

func noticeWriter(d *Deps) io.Writer {
	if d != nil && d.NoticeOut != nil {
		return d.NoticeOut
	}
	return io.Discard
}

// UpdateGlobalStateWith acquires the global state lock, re-reads registration
// from the store, merges changes, persists, and releases the lock. The store
// rewrite is atomic; the lock serialises load-merge-save across processes so a
// concurrent update to another repository is never lost. statePath is the
// retired state.json path, kept for call-site compatibility and ignored by the
// store-backed loaders.
func UpdateGlobalStateWith(d *Deps, statePath string, merge func(*GlobalState) error) error {
	return withStateLock(d, func() error {
		state, err := LoadGlobalStateWith(d, statePath)
		if err != nil {
			return err
		}
		if err := merge(state); err != nil {
			return err
		}
		return state.SaveWith(d)
	})
}

// withStateLock runs fn while holding the global task state lock, retrying while
// another live process holds it. It serialises every registration mutation —
// store-backed (UpdateGlobalStateWith) and the retired-file pruning
// (updateLegacyGlobalState) alike — across processes.
func withStateLock(d *Deps, fn func() error) error {
	noticeOut := noticeWriter(d)
	var lastErr error
	for attempt := 0; attempt < stateLockRetries; attempt++ {
		lock, err := acquireStateLock(d, noticeOut, false)
		if err != nil {
			if errors.Is(err, ErrStateLockBusy) && attempt < stateLockRetries-1 {
				lastErr = err
				time.Sleep(stateLockRetryDelay)
				continue
			}
			return err
		}

		err = func() error {
			defer lock.Release()
			return fn()
		}()
		if err != nil {
			return err
		}
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("acquire task state lock: exceeded retry limit")
}

func acquireStateLock(d *Deps, noticeOut io.Writer, retried bool) (*StateLock, error) {
	if noticeOut == nil {
		noticeOut = io.Discard
	}
	out := outputFor(noticeOut)

	lockPath := StateLockPathWith(d)
	lockDir := filepath.Dir(lockPath)
	if err := d.FS.MkdirAll(lockDir, 0o755); err != nil {
		return nil, fmt.Errorf("create task state lock directory: %w", err)
	}

	meta := StateLockMetadata{
		PID:       os.Getpid(),
		StartedAt: time.Now().UTC(),
	}
	payload, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode task state lock: %w", err)
	}

	created, err := createStateLockFile(d, lockPath, payload)
	if err == nil && created {
		return &StateLock{path: lockPath}, nil
	}
	if !os.IsExist(err) {
		return nil, fmt.Errorf("acquire task state lock: %w", err)
	}

	existing, readErr := d.FS.ReadFile(lockPath)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return acquireStateLock(d, noticeOut, false)
		}
		out.line(ansiYellow, "Removing unreadable task state lock at %s", lockPath)
		_ = os.Remove(lockPath)
		if retried {
			return nil, fmt.Errorf("acquire task state lock after recovery: %w", readErr)
		}
		return acquireStateLock(d, noticeOut, true)
	}

	existingMeta, parseErr := parseStateLockMetadata(existing)
	if parseErr != nil {
		out.line(ansiYellow, "Removing malformed task state lock at %s", lockPath)
		_ = os.Remove(lockPath)
		if retried {
			return nil, fmt.Errorf("acquire task state lock after recovery: %w", parseErr)
		}
		return acquireStateLock(d, noticeOut, true)
	}

	if processAlive(d, existingMeta.PID) {
		return nil, fmt.Errorf("%w (PID %d since %s)",
			ErrStateLockBusy,
			existingMeta.PID,
			existingMeta.StartedAt.Format(time.RFC3339),
		)
	}

	out.line(ansiYellow, "Removing stale task state lock (PID %d no longer running)", existingMeta.PID)
	if removeErr := os.Remove(lockPath); removeErr != nil && !os.IsNotExist(removeErr) {
		return nil, fmt.Errorf("remove stale task state lock: %w", removeErr)
	}
	if retried {
		return nil, fmt.Errorf("acquire task state lock after removing stale lock")
	}
	return acquireStateLock(d, noticeOut, true)
}

func createStateLockFile(d *Deps, lockPath string, payload []byte) (bool, error) {
	tmpPath := nextAtomicTempPath(filepath.Dir(lockPath))
	if err := d.FS.WriteFile(tmpPath, payload, 0o644); err != nil {
		return false, fmt.Errorf("write task state lock temp file: %w", err)
	}
	if err := os.Link(tmpPath, lockPath); err != nil {
		_ = d.FS.RemoveAll(tmpPath)
		return false, err
	}
	_ = d.FS.RemoveAll(tmpPath)
	return true, nil
}

func parseStateLockMetadata(data []byte) (*StateLockMetadata, error) {
	var meta StateLockMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	if meta.PID <= 0 || meta.StartedAt.IsZero() {
		return nil, fmt.Errorf("incomplete task state lock metadata")
	}
	return &meta, nil
}

// mergeNewRegistrations activates every discovered-but-unregistered set. It
// appends each new set's display id to added (for the "Registered new task
// set(s)" line) and its raw identifier to addedIDs (which the register command
// uses to eager-bind the current checkout per ADR-0115). Either out slice may be
// nil.
func mergeNewRegistrations(d *Deps, defPath string, disc *Discovery, state *GlobalState, added, addedIDs *[]string) {
	entry := state.Entry(defPath)
	registered := state.RegisteredIDs(defPath)
	ids := make([]string, 0, len(disc.Manifests))
	for id := range disc.Manifests {
		if _, ok := registered[id]; ok {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		reg := newRegisteredTaskSet(id)
		entry.TaskSets = append(entry.TaskSets, reg)
		registered[id] = len(entry.TaskSets) - 1
		if added != nil {
			*added = append(*added, registrationDisplayID(id, reg.AutoDrain))
		}
		if addedIDs != nil {
			*addedIDs = append(*addedIDs, id)
		}
	}
}

// newRegisteredTaskSet builds a new registration entry for a first-time
// registration. It seeds no worktree/auto-drain state from the manifest: those
// retired keys are no longer read (ADR-0115). Binding is materialized eagerly by
// the register command (it adopts the current checkout); auto-drain defaults off
// and is toggled via the CLI/dashboard.
func newRegisteredTaskSet(id string) RegisteredTaskSet {
	return RegisteredTaskSet{
		ID:       id,
		Priority: 0,
		Archived: false,
	}
}

func registrationDisplayID(id string, autoDrain bool) string {
	if autoDrain {
		return id + " (auto-drain)"
	}
	return id
}
