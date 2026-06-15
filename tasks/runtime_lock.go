package tasks

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// RuntimeLockMetadata is persisted in a runtime execution lock file.
type RuntimeLockMetadata struct {
	PID         int       `json:"pid"`
	RuntimePath string    `json:"runtime_path"`
	StartedAt   time.Time `json:"started_at"`
	// SetID identifies the task set this checkout is draining. It is optional:
	// older lock files (and locks written by older binaries) omit it, and the
	// parser must not reject a lock for its absence. A reader uses it to tell
	// which set a busy checkout is draining, not merely that it is busy.
	SetID string `json:"set_id,omitempty"`
}

// RuntimeLockStatus is best-effort lock information for status rendering.
type RuntimeLockStatus struct {
	RuntimePath string
	Locked      bool
	Metadata    *RuntimeLockMetadata
	Malformed   bool
}

// RuntimeLock is a held runtime execution lock.
type RuntimeLock struct {
	path string
}

// Release removes the runtime execution lock file.
func (l *RuntimeLock) Release() error {
	if l == nil || l.path == "" {
		return nil
	}
	err := os.Remove(l.path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// RuntimeLockDirWith returns the directory for runtime execution lock files.
func RuntimeLockDirWith(d *Deps) string {
	if xdgData := d.FS.Getenv("XDG_DATA_HOME"); xdgData != "" {
		return filepath.Join(xdgData, "pop", "runtime-locks")
	}
	home, err := d.FS.UserHomeDir()
	if err != nil {
		return filepath.Join("/tmp", "pop", "runtime-locks")
	}
	return filepath.Join(home, ".local", "share", "pop", "runtime-locks")
}

// RuntimeLockPathFor returns the lock file path for a canonical runtime root.
func RuntimeLockPathFor(d *Deps, runtimeRoot string) string {
	sum := sha256.Sum256([]byte(runtimeRoot))
	name := fmt.Sprintf("%x.lock", sum)
	return filepath.Join(RuntimeLockDirWith(d), name)
}

// ReadRuntimeLockStatus reads lock metadata for a runtime root without acquiring.
func ReadRuntimeLockStatus(d *Deps, runtimeRoot string) *RuntimeLockStatus {
	status := &RuntimeLockStatus{RuntimePath: runtimeRoot}
	lockPath := RuntimeLockPathFor(d, runtimeRoot)
	data, err := d.FS.ReadFile(lockPath)
	if os.IsNotExist(err) {
		return status
	}
	if err != nil {
		return status
	}
	meta, err := parseRuntimeLockMetadata(data)
	if err != nil {
		status.Malformed = true
		return status
	}
	status.Metadata = meta
	status.Locked = processAlive(d, meta.PID)
	return status
}

// AcquireRuntimeLock acquires an exclusive runtime execution lock.
func AcquireRuntimeLock(d *Deps, runtimeRoot string, noticeOut io.Writer) (*RuntimeLock, error) {
	return acquireRuntimeLock(d, runtimeRoot, "", noticeOut, false)
}

// AcquireRuntimeLockForSet acquires an exclusive runtime execution lock and
// records which task set is draining the checkout in the lock metadata.
func AcquireRuntimeLockForSet(d *Deps, runtimeRoot, setID string, noticeOut io.Writer) (*RuntimeLock, error) {
	return acquireRuntimeLock(d, runtimeRoot, setID, noticeOut, false)
}

func acquireRuntimeLock(d *Deps, runtimeRoot, setID string, noticeOut io.Writer, retried bool) (*RuntimeLock, error) {
	if noticeOut == nil {
		noticeOut = io.Discard
	}
	out := outputFor(noticeOut)
	lockDir := RuntimeLockDirWith(d)
	if err := d.FS.MkdirAll(lockDir, 0o755); err != nil {
		return nil, exitErr(ExitOperational, "create runtime lock directory: %v", err)
	}

	lockPath := RuntimeLockPathFor(d, runtimeRoot)
	meta := RuntimeLockMetadata{
		PID:         os.Getpid(),
		RuntimePath: runtimeRoot,
		StartedAt:   time.Now().UTC(),
		SetID:       setID,
	}
	payload, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return nil, exitErr(ExitOperational, "encode runtime lock: %v", err)
	}

	f, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err == nil {
		if _, err := f.Write(payload); err != nil {
			_ = f.Close()
			_ = os.Remove(lockPath)
			return nil, exitErr(ExitOperational, "write runtime lock: %v", err)
		}
		if err := f.Close(); err != nil {
			_ = os.Remove(lockPath)
			return nil, exitErr(ExitOperational, "close runtime lock: %v", err)
		}
		return &RuntimeLock{path: lockPath}, nil
	}
	if !os.IsExist(err) {
		return nil, exitErr(ExitOperational, "acquire runtime lock: %v", err)
	}

	existing, readErr := d.FS.ReadFile(lockPath)
	if readErr != nil {
		out.line(ansiYellow, "Removing unreadable runtime execution lock at %s", lockPath)
		_ = os.Remove(lockPath)
		if retried {
			return nil, exitErr(ExitOperational, "acquire runtime lock after recovery: %v", readErr)
		}
		return acquireRuntimeLock(d, runtimeRoot, setID, noticeOut, true)
	}

	existingMeta, parseErr := parseRuntimeLockMetadata(existing)
	if parseErr != nil {
		out.line(ansiYellow, "Removing malformed runtime execution lock at %s", lockPath)
		_ = os.Remove(lockPath)
		if retried {
			return nil, exitErr(ExitOperational, "acquire runtime lock after recovery: %v", parseErr)
		}
		return acquireRuntimeLock(d, runtimeRoot, setID, noticeOut, true)
	}

	if processAlive(d, existingMeta.PID) {
		return nil, exitErr(ExitOperational,
			"runtime execution already in progress (PID %d since %s at %s)",
			existingMeta.PID,
			existingMeta.StartedAt.Format(time.RFC3339),
			existingMeta.RuntimePath,
		)
	}

	out.line(ansiYellow, "Removing stale runtime execution lock (PID %d no longer running)", existingMeta.PID)
	if removeErr := os.Remove(lockPath); removeErr != nil && !os.IsNotExist(removeErr) {
		return nil, exitErr(ExitOperational, "remove stale runtime lock: %v", removeErr)
	}
	if retried {
		return nil, exitErr(ExitOperational, "acquire runtime lock after removing stale lock")
	}
	return acquireRuntimeLock(d, runtimeRoot, setID, noticeOut, true)
}

func parseRuntimeLockMetadata(data []byte) (*RuntimeLockMetadata, error) {
	var meta RuntimeLockMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	if meta.PID <= 0 || meta.RuntimePath == "" || meta.StartedAt.IsZero() {
		return nil, fmt.Errorf("incomplete runtime lock metadata")
	}
	return &meta, nil
}

func processAlive(d *Deps, pid int) bool {
	if d != nil && d.ProcessAlive != nil {
		return d.ProcessAlive(pid)
	}
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// CheckCrossCheckoutConflict checks whether the same (repo, setID) pair is
// already live in any other checkout of the same repository. It enumerates all
// runtime lock files, and for each live, PID-alive lock that matches setID in a
// different checkout, verifies membership in the same repository by comparing
// git common directories. Stale (dead-PID) locks are silently ignored,
// consistent with the per-checkout lock's self-healing behavior.
// currentRuntimePath is excluded from the scan so the existing per-checkout lock
// handles same-checkout conflicts without this backstop interfering.
//
// Repository identity comparison is lazy: it only runs git commands when a
// live candidate conflict is found, keeping the common (no-conflict) path free
// of subprocess overhead.
func CheckCrossCheckoutConflict(d *Deps, projectPath, currentRuntimePath, setID string) error {
	if setID == "" {
		return nil
	}
	lockDir := RuntimeLockDirWith(d)
	entries, err := d.FS.ReadDir(lockDir)
	if err != nil {
		return nil // no lock directory = no conflicts
	}
	var ourCommonDir string
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".lock") {
			continue
		}
		data, err := d.FS.ReadFile(filepath.Join(lockDir, entry.Name()))
		if err != nil {
			continue
		}
		meta, err := parseRuntimeLockMetadata(data)
		if err != nil {
			continue
		}
		if !processAlive(d, meta.PID) || meta.SetID != setID || meta.RuntimePath == currentRuntimePath {
			continue
		}
		// Candidate conflict: resolve our commonDir lazily on the first one found.
		if ourCommonDir == "" {
			id, err := ResolveRepositoryIdentity(d, projectPath)
			if err != nil {
				return nil // can't determine our repo — skip the backstop
			}
			ourCommonDir = id.CommonDir
		}
		theirCommonDir, err := resolveCheckoutCommonDir(d, meta.RuntimePath)
		if err != nil {
			continue
		}
		if theirCommonDir == ourCommonDir {
			return exitErr(ExitOperational,
				"runtime execution already in progress (PID %d since %s at %s)",
				meta.PID,
				meta.StartedAt.Format(time.RFC3339),
				meta.RuntimePath,
			)
		}
	}
	return nil
}

// resolveCheckoutCommonDir returns the canonical git common directory for the
// repository containing checkoutPath.
func resolveCheckoutCommonDir(d *Deps, checkoutPath string) (string, error) {
	out, err := d.Git.CommandInDir(checkoutPath, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	out = strings.TrimSpace(out)
	if !filepath.IsAbs(out) {
		out = filepath.Join(checkoutPath, out)
	}
	return canonicalAbsPath(d, out)
}
