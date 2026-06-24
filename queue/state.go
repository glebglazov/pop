package queue

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebglazov/pop/tasks/binding"
	"github.com/glebglazov/pop/tasks/integration"
	"github.com/glebglazov/pop/tasks"
)

// DaemonState is persisted supervisor-owned state. It tracks pinned-agent quota
// cooldown timers and drain panes. Abnormal-driven backoff and parking are no
// longer persisted here — they are derived from Drain history plus a durable
// park-clear event (ADR-0055). Worktree bindings and mergeability live in the
// shared per-repository stores owned by tasks/binding and tasks/integration.
type DaemonState struct {
	Version     int                  `json:"version"`
	UpdatedAt   time.Time            `json:"updated_at,omitempty"`
	SetBackoffs map[string]time.Time `json:"set_backoffs,omitempty"`
	DrainPanes  map[string]DrainPane `json:"drain_panes,omitempty"`
}

// WorktreeBinding is the binding module's durable checkout record. The alias
// keeps queue call sites and tests referring to it unchanged while the type is
// owned by the binding module.
type WorktreeBinding = binding.Binding

// DrainPane records the tmux pane currently associated with a Task set drain.
type DrainPane struct {
	Project     string    `json:"project,omitempty"`
	RuntimePath string    `json:"runtime_path,omitempty"`
	SetID       string    `json:"set_id"`
	PaneID      string    `json:"pane_id"`
	RecordedAt  time.Time `json:"recorded_at"`
	Source      string    `json:"source,omitempty"`
}

// bindingProvisioned reports whether the binding stored under key was
// provisioned by pop (safe to teardown) or adopted (must not delete).
func bindingProvisioned(d *tasks.Deps, key string) bool {
	return binding.Provisioned(d, key)
}

// bindingShouldTeardown reports whether the worktree at key should be torn
// down. It returns true when there is no binding (legacy/unknown — pop probably
// created it) or when the binding is explicitly provisioned. It returns false
// only for explicitly adopted bindings (Provisioned=false), which must never
// have their directories deleted.
func bindingShouldTeardown(d *tasks.Deps, key string) bool {
	return binding.ShouldTeardown(d, key)
}

const (
	MergeabilityClean     = integration.StatusClean
	MergeabilityConflicts = integration.StatusConflicts
)

// MergeabilityRecord records whether a DONE set branch can be textually merged
// into the working branch.
type MergeabilityRecord = integration.Record

// bindingForSet returns the shared-store binding for (repoKey, setID).
func bindingForSet(d *tasks.Deps, repoKey, setID string) (WorktreeBinding, bool) {
	if d == nil {
		return WorktreeBinding{}, false
	}
	store, err := binding.Load(d)
	if err != nil {
		return WorktreeBinding{}, false
	}
	b, ok := store.Get(setScopedKey(repoKey, setID))
	return b, ok
}

// DaemonStatePath returns the queue daemon state path.
func DaemonStatePath(d *tasks.Deps) string {
	return filepath.Join(QueueDataDir(d), "state.json")
}

// EnsureDaemonState creates the daemon state file when missing and returns it.
func EnsureDaemonState(d *tasks.Deps) (*DaemonState, error) {
	state, err := ReadDaemonState(d)
	if err == nil {
		return state, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	state = &DaemonState{Version: 1, UpdatedAt: time.Now().UTC()}
	if err := WriteDaemonState(d, state); err != nil {
		return nil, err
	}
	return state, nil
}

// ReadDaemonState reads the persisted queue daemon state file. Legacy
// worktree_bindings embedded in pre-ADR-0036 state files are migrated into the
// shared binding store on read.
func ReadDaemonState(d *tasks.Deps) (*DaemonState, error) {
	data, err := d.FS.ReadFile(DaemonStatePath(d))
	if err != nil {
		return nil, err
	}
	var envelope struct {
		DaemonState
		LegacyBindings     map[string]WorktreeBinding    `json:"worktree_bindings,omitempty"`
		LegacyMergeability map[string]MergeabilityRecord `json:"mergeability,omitempty"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("parse queue daemon state: %w", err)
	}
	state := envelope.DaemonState
	migrateDaemonState(&state)
	if len(envelope.LegacyBindings) > 0 {
		if err := migrateLegacyBindings(d, envelope.LegacyBindings); err != nil {
			return nil, err
		}
	}
	if len(envelope.LegacyMergeability) > 0 {
		legacy := make(map[string]integration.Record, len(envelope.LegacyMergeability))
		for k, v := range envelope.LegacyMergeability {
			legacy[k] = integration.Record(v)
		}
		if err := integration.MigrateLegacyFromDaemonState(d, legacy); err != nil {
			return nil, err
		}
	}
	return &state, nil
}

func migrateLegacyBindings(d *tasks.Deps, legacy map[string]WorktreeBinding) error {
	store, err := binding.Load(d)
	if err != nil {
		return err
	}
	changed := false
	for key, b := range legacy {
		if _, ok := store.Get(key); ok {
			continue
		}
		store.Put(key, b)
		changed = true
	}
	if changed {
		return binding.Save(d, store)
	}
	return nil
}

// WriteDaemonState writes the queue daemon state file atomically.
func WriteDaemonState(d *tasks.Deps, state *DaemonState) error {
	if state == nil {
		state = &DaemonState{Version: 1}
	}
	if state.Version == 0 {
		state.Version = 1
	}
	state.UpdatedAt = time.Now().UTC()
	payload, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return tasks.WriteAtomicWith(d, DaemonStatePath(d), append(payload, '\n'), 0o644)
}

// repoIdentityKey returns the repository identity prefix used in set-scoped keys.
func repoIdentityKey(id *tasks.RepositoryIdentity) string {
	return binding.RepoKey(id)
}

// setScopedKey keys set-scoped daemon state by repository identity plus set id.
func setScopedKey(repoKey, setID string) string {
	return binding.ScopedKey(repoKey, setID)
}

// repoIdentityFromWorktreePath extracts basename-shortHash from a queue worktree path.
func repoIdentityFromWorktreePath(path string) string {
	clean := filepath.Clean(path)
	parts := strings.Split(clean, string(os.PathSeparator))
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] == "worktrees" {
			return parts[i+1]
		}
	}
	return ""
}

func migrateDaemonState(state *DaemonState) {
	if state == nil {
		return
	}
	migrateStringTimeMap(state.SetBackoffs)
}

func migrateStringTimeMap(m map[string]time.Time) {
	if len(m) == 0 {
		return
	}
	targets := map[string][]string{}
	for oldKey := range m {
		runtimePath := runtimePathFromScopedKey(oldKey)
		newKey, ok := migratedScopedKey(oldKey, runtimePath)
		if !ok || newKey == oldKey {
			continue
		}
		targets[newKey] = append(targets[newKey], oldKey)
	}
	for newKey, oldKeys := range targets {
		if len(oldKeys) != 1 {
			continue
		}
		if _, exists := m[newKey]; exists {
			continue
		}
		m[newKey] = m[oldKeys[0]]
		delete(m, oldKeys[0])
	}
}

func migratedScopedKey(oldKey, runtimePath string) (string, bool) {
	parts := strings.Split(oldKey, "\x00")
	if len(parts) != 2 {
		return oldKey, false
	}
	setID := parts[1]
	if !isLegacyScopedKey(parts[0]) {
		return oldKey, false
	}
	repoKey := repoIdentityFromWorktreePath(parts[0])
	if repoKey == "" {
		repoKey = repoIdentityFromWorktreePath(runtimePath)
	}
	if repoKey == "" {
		return oldKey, false
	}
	return setScopedKey(repoKey, setID), true
}

func isLegacyScopedKey(prefix string) bool {
	return strings.HasPrefix(prefix, "/") || strings.Contains(prefix, "\\")
}

func runtimePathFromScopedKey(key string) string {
	parts := strings.Split(key, "\x00")
	if len(parts) != 2 {
		return ""
	}
	return parts[0]
}
