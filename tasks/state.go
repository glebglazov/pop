package tasks

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/glebglazov/pop/store"
)

const StateVersion = 1

// stateFileName is the retired per-repository Task registration file. ADR-0055
// moves registration into the global store; surviving files are folded in on
// first read (foldLegacyStateFile), then removed. No command writes it.
const stateFileName = "state.json"

// StatePathFor returns the retired per-repository Task registration file path
// for a definition (tasks) directory: <storage>/state.json beside the tasks/
// directory.
//
// Deprecated: registration now lives in the global store (see ADR-0055). This
// path is retained only as the per-repository migration source folded in by
// foldLegacyStateFile; it is the value still threaded as the statePath
// argument, which the store-backed loaders ignore.
func StatePathFor(defPath string) string {
	return filepath.Join(filepath.Dir(defPath), stateFileName)
}

// RegisteredTaskSet is registration metadata for a Task set.
type RegisteredTaskSet struct {
	ID        string `json:"id"`
	Priority  int    `json:"priority"`
	Archived  bool   `json:"archived"`
	AutoDrain bool   `json:"auto_drain"`
	// WorktreeIntent is the set-level worktree directive seeded once at first
	// registration (ADR-0059), exactly like AutoDrain. Nil means no directive:
	// the set drains in the current checkout. It records intent only — no
	// provisioning happens here — and is never re-read from the manifest on later
	// refreshes.
	WorktreeIntent *WorktreeDirective `json:"worktree_intent,omitempty"`
}

// worktreeIntentToStore flattens a seeded worktree directive into the store's
// two columns (ADR-0059): nil → no directive, Managed → managed bit, else the
// adopted worktree name.
func worktreeIntentToStore(w *WorktreeDirective) (managed bool, name string) {
	if w == nil {
		return false, ""
	}
	return w.Managed, w.Name
}

// worktreeIntentFromStore reconstructs a seeded worktree directive from the
// store's two columns. The managed bit wins; else a non-empty name adopts; else
// there is no directive (nil).
func worktreeIntentFromStore(managed bool, name string) *WorktreeDirective {
	switch {
	case managed:
		return &WorktreeDirective{Managed: true}
	case name != "":
		return &WorktreeDirective{Name: name}
	default:
		return nil
	}
}

// TaskEntry holds registered Task sets for one definition path.
type TaskEntry struct {
	TaskSets []RegisteredTaskSet `json:"issue_sets"`
}

// GlobalState is the in-memory view of Task set registration. It mirrors the
// store's sets table, keyed by definition path; load reads the whole (tiny)
// table and save rewrites it. The store is the durable home (ADR-0055); the JSON
// tags survive only to parse the retired state.json files during migration.
type GlobalState struct {
	Version int                   `json:"version"`
	Tasks   map[string]*TaskEntry `json:"workloads"`
	path    string
}

// DefaultStatePath returns the legacy global task state file path.
//
// Deprecated: Task registration lives in the global store (ADR-0055). This path
// is retained only as the pre-per-repo migration source read by Migrate and
// MigrateStorageLayout; no normal command reads or writes it.
func DefaultStatePath() string {
	return DefaultStatePathWith(defaultDeps)
}

// DefaultStatePathWith returns the legacy global task state file path using
// provided dependencies. See DefaultStatePath for why it survives.
func DefaultStatePathWith(d *Deps) string {
	if xdgData := d.FS.Getenv("XDG_DATA_HOME"); xdgData != "" {
		return filepath.Join(xdgData, "pop", "workloads-state.json")
	}
	home, err := d.FS.UserHomeDir()
	if err != nil {
		return filepath.Join("/tmp", "pop", "workloads-state.json")
	}
	return filepath.Join(home, ".local", "share", "pop", "workloads-state.json")
}

// LoadGlobalState reads registration from the store.
func LoadGlobalState(path string) (*GlobalState, error) {
	return LoadGlobalStateWith(defaultDeps, path)
}

// LoadGlobalStateWith reads registration from the global store. It first folds
// any surviving per-repository state.json files into the store and retires them,
// then reads the whole sets table into the keyed-by-def-path view. The path
// argument is the retired state.json path; it is ignored for reads (the store is
// global) and kept only for call-site compatibility. A reader with no legacy
// files and no store yields an empty state without materialising the database.
func LoadGlobalStateWith(d *Deps, path string) (*GlobalState, error) {
	if err := foldLegacyStateFile(d, path); err != nil {
		return nil, err
	}
	gs := &GlobalState{
		Version: StateVersion,
		Tasks:   make(map[string]*TaskEntry),
		path:    path,
	}
	s, ok, err := openDrainStoreIfExists(d)
	if err != nil || !ok {
		return gs, err
	}
	all, err := s.AllSets()
	if err != nil {
		return nil, err
	}
	for def, regs := range all {
		entry := &TaskEntry{}
		for _, r := range regs {
			entry.TaskSets = append(entry.TaskSets, RegisteredTaskSet{
				ID:             r.SetID,
				Priority:       r.Priority,
				Archived:       r.Archived,
				AutoDrain:      r.AutoDrain,
				WorktreeIntent: worktreeIntentFromStore(r.WorktreeManaged, r.WorktreeName),
			})
		}
		gs.Tasks[def] = entry
	}
	return gs, nil
}

// Save persists registration to the store.
func (s *GlobalState) Save() error {
	return s.SaveWith(defaultDeps)
}

// SaveWith rewrites the store's sets table from this view, creating the store on
// first write. The whole-table rewrite is atomic (single writer) and serialised
// by the global state lock held across UpdateGlobalStateWith, so a concurrent
// update to another repository is never lost.
func (s *GlobalState) SaveWith(d *Deps) error {
	st, err := openDrainStore(d)
	if err != nil {
		return err
	}
	rows := make(map[string][]store.SetReg, len(s.Tasks))
	for def, entry := range s.Tasks {
		if entry == nil {
			continue
		}
		for _, r := range entry.TaskSets {
			managed, name := worktreeIntentToStore(r.WorktreeIntent)
			rows[def] = append(rows[def], store.SetReg{
				DefPath:         def,
				SetID:           r.ID,
				Priority:        r.Priority,
				Archived:        r.Archived,
				AutoDrain:       r.AutoDrain,
				WorktreeManaged: managed,
				WorktreeName:    name,
			})
		}
	}
	return st.ReplaceAllSets(rows)
}

// Entry returns the task entry for a definition path, creating it if needed.
func (s *GlobalState) Entry(defPath string) *TaskEntry {
	if s.Tasks[defPath] == nil {
		s.Tasks[defPath] = &TaskEntry{TaskSets: nil}
	}
	return s.Tasks[defPath]
}

// RegisteredWorktreeIntent returns the seeded worktree directive for the set
// (defPath, setID), or nil when the set is unregistered or carries no directive.
// It reads the persisted registration intent (ADR-0059) — the one-time seed —
// and never re-reads the manifest. Drain routing consults it to decide whether
// an unbound drain provisions/adopts a worktree.
func RegisteredWorktreeIntent(d *Deps, defPath, setID string) (*WorktreeDirective, error) {
	state, err := LoadGlobalStateWith(d, StatePathFor(defPath))
	if err != nil {
		return nil, err
	}
	entry := state.Tasks[defPath]
	if entry == nil {
		return nil, nil
	}
	for _, set := range entry.TaskSets {
		if set.ID == setID {
			return set.WorktreeIntent, nil
		}
	}
	return nil, nil
}

// RegisteredIDs returns a set of registered Task-set IDs for a definition path.
func (s *GlobalState) RegisteredIDs(defPath string) map[string]int {
	entry := s.Tasks[defPath]
	result := make(map[string]int)
	if entry == nil {
		return result
	}
	for i, set := range entry.TaskSets {
		result[set.ID] = i
	}
	return result
}

// foldLegacyStateFile migrates one retired per-repository state.json into the
// store and removes it, completing the move at the tasks boundary on first read
// (ADR-0055). statePath is the file beside this repository's Task storage. A
// missing file is the steady state after the one-time migration and opens no
// store. Priority, archived, and auto-drain bits are preserved exactly, and
// registration order is kept by inserting in file order; a (def_path, set_id)
// already in the store wins (the file is stale).
func foldLegacyStateFile(d *Deps, statePath string) error {
	legacy, err := loadLegacyGlobalState(d, statePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	hasRows := false
	for _, entry := range legacy.Tasks {
		if entry != nil && len(entry.TaskSets) > 0 {
			hasRows = true
			break
		}
	}
	if hasRows {
		s, err := openDrainStore(d)
		if err != nil {
			return err
		}
		existing, err := s.AllSets()
		if err != nil {
			return err
		}
		for def, entry := range legacy.Tasks {
			if entry == nil {
				continue
			}
			seen := make(map[string]bool, len(existing[def]))
			for _, r := range existing[def] {
				seen[r.SetID] = true
			}
			for _, reg := range entry.TaskSets {
				if seen[reg.ID] {
					continue
				}
				managed, name := worktreeIntentToStore(reg.WorktreeIntent)
				if err := s.PutSet(store.SetReg{
					DefPath:         def,
					SetID:           reg.ID,
					Priority:        reg.Priority,
					Archived:        reg.Archived,
					AutoDrain:       reg.AutoDrain,
					WorktreeManaged: managed,
					WorktreeName:    name,
				}); err != nil {
					return err
				}
				seen[reg.ID] = true
			}
		}
	}
	return d.FS.RemoveAll(statePath)
}

// loadLegacyGlobalState parses a retired JSON registration file (a per-repo
// state.json or the pre-per-repo global workloads-state.json). It is the only
// remaining JSON reader; the store-backed loaders never touch these files except
// to migrate them. A missing file yields an empty state and os.ErrNotExist so
// callers can distinguish absence.
func loadLegacyGlobalState(d *Deps, path string) (*GlobalState, error) {
	data, err := d.FS.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &GlobalState{
				Version: StateVersion,
				Tasks:   make(map[string]*TaskEntry),
				path:    path,
			}, err
		}
		return nil, err
	}
	var s GlobalState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("task state: parse %s: %w", path, err)
	}
	if s.Version != StateVersion {
		return nil, fmt.Errorf("task state: unsupported version %d in %s", s.Version, path)
	}
	if s.Tasks == nil {
		s.Tasks = make(map[string]*TaskEntry)
	}
	s.path = path
	return &s, nil
}

// saveLegacyGlobalState writes a retired JSON registration file atomically. It
// is used only to prune entries from the pre-per-repo global workloads-state.json
// during the storage-layout migration.
func saveLegacyGlobalState(d *Deps, s *GlobalState) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return WriteAtomicWith(d, s.path, data, 0o644)
}

// updateLegacyGlobalState load-merge-saves the pre-per-repo global
// workloads-state.json under the global state lock. The store-backed
// UpdateGlobalStateWith handles live registration; this survives only for the
// one-time storage-layout migration that drains the legacy global file.
func updateLegacyGlobalState(d *Deps, path string, merge func(*GlobalState) error) error {
	return withStateLock(d, func() error {
		state, err := loadLegacyGlobalState(d, path)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := merge(state); err != nil {
			return err
		}
		return saveLegacyGlobalState(d, state)
	})
}
