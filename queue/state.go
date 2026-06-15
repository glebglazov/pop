package queue

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebglazov/pop/binding"
	"github.com/glebglazov/pop/tasks"
)

// DaemonState is persisted supervisor-owned state. Later queue slices add
// cooldowns, parked sets, and retry timers here.
//
// WorktreeBindings is no longer persisted here: ADR-0036 moved Worktree
// bindings into the shared per-repository store owned by the binding module.
// The field is retained as an in-memory view that ReadDaemonState populates
// from the binding store and WriteDaemonState writes back to it, so the daemon
// state file itself never carries binding state.
type DaemonState struct {
	Version          int                           `json:"version"`
	UpdatedAt        time.Time                     `json:"updated_at,omitempty"`
	AgentCooldowns   map[string]time.Time          `json:"agent_cooldowns,omitempty"`
	SetBackoffs      map[string]time.Time          `json:"set_backoffs,omitempty"`
	SetCrashBackoffs map[string]time.Time          `json:"set_crash_backoffs,omitempty"`
	SetCrashCounts   map[string]int                `json:"set_crash_counts,omitempty"`
	ParkedSets       map[string]ParkedSet          `json:"parked_sets,omitempty"`
	Mergeability     map[string]MergeabilityRecord `json:"mergeability,omitempty"`
	WorktreeBindings map[string]WorktreeBinding    `json:"worktree_bindings,omitempty"`
}

// WorktreeBinding is the binding module's durable checkout record. The alias
// keeps queue call sites and tests referring to it unchanged while the type is
// owned by the binding module.
type WorktreeBinding = binding.Binding

// bindingProvisioned reports whether the binding stored under key was
// provisioned by pop (safe to teardown) or adopted (must not delete).
func bindingProvisioned(state *DaemonState, key string) bool {
	if state == nil {
		return false
	}
	return (&binding.Store{Bindings: state.WorktreeBindings}).Provisioned(key)
}

// bindingShouldTeardown reports whether the worktree at key should be torn
// down. It returns true when there is no binding (legacy/unknown — pop probably
// created it) or when the binding is explicitly provisioned. It returns false
// only for explicitly adopted bindings (Provisioned=false), which must never
// have their directories deleted.
func bindingShouldTeardown(state *DaemonState, key string) bool {
	if state == nil {
		return true // no state at all: legacy path, tear down
	}
	return (&binding.Store{Bindings: state.WorktreeBindings}).ShouldTeardown(key)
}

// ParkedSet records a task set whose consecutive abnormal exits exhausted the
// crash retry schedule. It is durable so a supervisor restart does not re-arm
// the loop.
type ParkedSet struct {
	RuntimePath              string    `json:"runtime_path"`
	SetID                    string    `json:"set_id"`
	ParkedAt                 time.Time `json:"parked_at"`
	Reason                   string    `json:"reason,omitempty"`
	ConsecutiveAbnormalExits int       `json:"consecutive_abnormal_exits"`
}

const (
	MergeabilityClean     = "clean"
	MergeabilityConflicts = "conflicts"
)

// MergeabilityRecord records whether a DONE set branch can be textually merged
// into the working branch. It is advisory; queue never integrates automatically.
type MergeabilityRecord struct {
	Project     string    `json:"project,omitempty"`
	RuntimePath string    `json:"runtime_path"`
	SetID       string    `json:"set_id"`
	Status      string    `json:"status"`
	CheckedAt   time.Time `json:"checked_at"`
	Target      string    `json:"target,omitempty"`
	Source      string    `json:"source,omitempty"`
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

// ReadDaemonState reads the persisted queue daemon state file and hydrates the
// in-memory WorktreeBindings view from the shared binding store. Any binding
// state still embedded in a pre-ADR-0036 daemon state file is folded into the
// view (store entries win) and migrated out on the next write.
func ReadDaemonState(d *tasks.Deps) (*DaemonState, error) {
	data, err := d.FS.ReadFile(DaemonStatePath(d))
	if err != nil {
		return nil, err
	}
	var state DaemonState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse queue daemon state: %w", err)
	}
	migrateDaemonState(&state)
	if err := loadBindings(d, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

// loadBindings replaces state.WorktreeBindings with the shared binding store's
// contents, folding in any legacy bindings the daemon state file still carries.
func loadBindings(d *tasks.Deps, state *DaemonState) error {
	store, err := binding.Load(d)
	if err != nil {
		return err
	}
	merged := store.Bindings
	for key, legacy := range state.WorktreeBindings {
		if _, ok := merged[key]; ok {
			continue
		}
		if merged == nil {
			merged = map[string]WorktreeBinding{}
		}
		merged[key] = legacy
	}
	state.WorktreeBindings = merged
	return nil
}

// WriteDaemonState writes the queue daemon state file atomically. Worktree
// bindings are persisted to the shared binding store (not the daemon state
// file) so they remain readable and writable without a daemon process. The
// store is only rewritten when the caller manages bindings (non-nil view), so a
// plain state update never clobbers the store.
func WriteDaemonState(d *tasks.Deps, state *DaemonState) error {
	if state == nil {
		state = &DaemonState{Version: 1}
	}
	if state.Version == 0 {
		state.Version = 1
	}
	state.UpdatedAt = time.Now().UTC()
	if state.WorktreeBindings != nil {
		if err := binding.Save(d, &binding.Store{Bindings: state.WorktreeBindings}); err != nil {
			return err
		}
	}
	bindings := state.WorktreeBindings
	state.WorktreeBindings = nil
	payload, err := json.MarshalIndent(state, "", "  ")
	state.WorktreeBindings = bindings
	if err != nil {
		return err
	}
	return tasks.WriteAtomicWith(d, DaemonStatePath(d), append(payload, '\n'), 0o644)
}

// repoIdentityKey returns the repository identity prefix used in set-scoped keys.
// It delegates to the binding module so daemon-state keys and shared-store
// binding keys stay byte-identical for the same (repo, set).
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
	migrateStringTimeMap(state.SetCrashBackoffs)
	migrateIntMap(state.SetCrashCounts)
	migrateParkedSets(state)
	migrateMergeability(state)
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

func migrateIntMap(m map[string]int) {
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

func migrateParkedSets(state *DaemonState) {
	if len(state.ParkedSets) == 0 {
		return
	}
	targets := map[string][]string{}
	for oldKey, rec := range state.ParkedSets {
		newKey, ok := migratedScopedKey(oldKey, rec.RuntimePath)
		if !ok || newKey == oldKey {
			continue
		}
		targets[newKey] = append(targets[newKey], oldKey)
	}
	for newKey, oldKeys := range targets {
		if len(oldKeys) != 1 {
			continue
		}
		if _, exists := state.ParkedSets[newKey]; exists {
			continue
		}
		state.ParkedSets[newKey] = state.ParkedSets[oldKeys[0]]
		delete(state.ParkedSets, oldKeys[0])
	}
}

func migrateMergeability(state *DaemonState) {
	if len(state.Mergeability) == 0 {
		return
	}
	targets := map[string][]string{}
	for oldKey, rec := range state.Mergeability {
		newKey, ok := migratedScopedKey(oldKey, rec.RuntimePath)
		if !ok || newKey == oldKey {
			continue
		}
		targets[newKey] = append(targets[newKey], oldKey)
	}
	for newKey, oldKeys := range targets {
		if len(oldKeys) != 1 {
			continue
		}
		if _, exists := state.Mergeability[newKey]; exists {
			continue
		}
		state.Mergeability[newKey] = state.Mergeability[oldKeys[0]]
		delete(state.Mergeability, oldKeys[0])
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
