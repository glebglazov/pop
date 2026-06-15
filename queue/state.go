package queue

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebglazov/pop/tasks"
)

// DaemonState is persisted supervisor-owned state. Later queue slices add
// cooldowns, parked sets, and retry timers here.
type DaemonState struct {
	Version           int                           `json:"version"`
	UpdatedAt         time.Time                     `json:"updated_at,omitempty"`
	AgentCooldowns    map[string]time.Time          `json:"agent_cooldowns,omitempty"`
	SetBackoffs       map[string]time.Time          `json:"set_backoffs,omitempty"`
	SetCrashBackoffs  map[string]time.Time          `json:"set_crash_backoffs,omitempty"`
	SetCrashCounts    map[string]int                `json:"set_crash_counts,omitempty"`
	ParkedSets        map[string]ParkedSet          `json:"parked_sets,omitempty"`
	Mergeability      map[string]MergeabilityRecord `json:"mergeability,omitempty"`
	WorktreeBindings  map[string]WorktreeBinding    `json:"worktree_bindings,omitempty"`
}

// WorktreeBinding records the durable checkout associated with one Task set.
type WorktreeBinding struct {
	RuntimePath string `json:"runtime_path"`
	Branch      string `json:"branch"`
	Project     string `json:"project"`
	// Provisioned is true when pop ran `git worktree add` to create this
	// checkout. False (or absent) means the binding is adopted — a human
	// pointed an existing checkout at the set; pop must never delete it.
	Provisioned bool `json:"provisioned,omitempty"`
}

// bindingProvisioned reports whether the binding stored under key was
// provisioned by pop (safe to teardown) or adopted (must not delete).
func bindingProvisioned(state *DaemonState, key string) bool {
	if state == nil || state.WorktreeBindings == nil {
		return false
	}
	b, ok := state.WorktreeBindings[key]
	return ok && b.Provisioned
}

// bindingShouldTeardown reports whether the worktree at key should be torn
// down. It returns true when there is no binding (legacy/unknown — pop probably
// created it) or when the binding is explicitly provisioned. It returns false
// only for explicitly adopted bindings (Provisioned=false), which must never
// have their directories deleted.
func bindingShouldTeardown(state *DaemonState, key string) bool {
	if state == nil || state.WorktreeBindings == nil {
		return true // no state at all: legacy path, tear down
	}
	b, ok := state.WorktreeBindings[key]
	if !ok {
		return true // no binding recorded: legacy path, tear down
	}
	return b.Provisioned // adopted=false → retain; provisioned=true → tear down
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

// ReadDaemonState reads the persisted queue daemon state file.
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
	return &state, nil
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
	return id.Basename + "-" + id.ShortHash
}

// setScopedKey keys set-scoped daemon state by repository identity plus set id.
func setScopedKey(repoKey, setID string) string {
	return repoKey + "\x00" + setID
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
