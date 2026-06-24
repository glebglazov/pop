// Package integration owns Mergeability persistence, the Integrate verb, and
// Integration backlog helpers. It is trigger-agnostic: both `pop queue run` and
// `pop tasks implement` read and write the same mergeability store beside the
// binding store, not in queue daemon state (ADR-0036, ADR-0046).
package integration

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebglazov/pop/tasks"
)

const (
	StatusClean     = "clean"
	StatusConflicts = "conflicts"
)

// Record records whether a DONE set branch can be textually merged into the
// working branch. It is advisory; pop never integrates automatically except
// when the repository opts in via auto_merge_clean.
//
// Status is the verdict, Target/Source the working and runtime HEADs it was
// computed from, and WorkingPath the working checkout — carried so the reconcile
// SHA gate can read both HEADs without resolving the trunk afresh (ADR-0055).
type Record struct {
	Project     string    `json:"project,omitempty"`
	RuntimePath string    `json:"runtime_path"`
	WorkingPath string    `json:"working_path,omitempty"`
	SetID       string    `json:"set_id"`
	Status      string    `json:"status"`
	CheckedAt   time.Time `json:"checked_at"`
	Target      string    `json:"target,omitempty"`
	Source      string    `json:"source,omitempty"`
}

// Store is the shared per-repository mergeability cache keyed by repository
// identity plus Task set identifier.
type Store struct {
	Records map[string]Record `json:"records,omitempty"`
}

// Get returns the record stored under key.
func (s *Store) Get(key string) (Record, bool) {
	if s == nil || s.Records == nil {
		return Record{}, false
	}
	rec, ok := s.Records[key]
	return rec, ok
}

// Put records rec under key, allocating the map on demand.
func (s *Store) Put(key string, rec Record) {
	if s.Records == nil {
		s.Records = map[string]Record{}
	}
	s.Records[key] = rec
}

// Delete forgets the record under key.
func (s *Store) Delete(key string) {
	delete(s.Records, key)
}

// Load reads the mergeability store from the global execution-state database
// (ADR-0055). A store that does not yet exist yields an empty Store; nothing is
// ever read from a standalone mergeability file.
func Load(d *tasks.Deps) (*Store, error) {
	entries, err := tasks.LoadMergeabilityEntries(d)
	if err != nil {
		return nil, err
	}
	s := &Store{}
	for key, e := range entries {
		s.Put(key, recordFromEntry(e))
	}
	return s, nil
}

// Save writes the mergeability store to the global execution-state database,
// replacing every row in one transaction (ADR-0055).
func Save(d *tasks.Deps, store *Store) error {
	if store == nil {
		store = &Store{}
	}
	entries := make(map[string]tasks.MergeabilityEntry, len(store.Records))
	for key, rec := range store.Records {
		entries[key] = entryFromRecord(rec)
	}
	return tasks.SaveMergeabilityEntries(d, entries)
}

func recordFromEntry(e tasks.MergeabilityEntry) Record {
	return Record{
		Project:     e.Project,
		RuntimePath: e.RuntimePath,
		WorkingPath: e.WorkingPath,
		SetID:       e.SetID,
		Status:      e.Verdict,
		CheckedAt:   e.ComputedAt,
		Target:      e.BaseSHA,
		Source:      e.BranchSHA,
	}
}

func entryFromRecord(rec Record) tasks.MergeabilityEntry {
	return tasks.MergeabilityEntry{
		Project:     rec.Project,
		RuntimePath: rec.RuntimePath,
		WorkingPath: rec.WorkingPath,
		SetID:       rec.SetID,
		Verdict:     rec.Status,
		BaseSHA:     rec.Target,
		BranchSHA:   rec.Source,
		ComputedAt:  rec.CheckedAt,
	}
}

// AllRecords returns every mergeability record in the shared store.
func AllRecords(d *tasks.Deps) (map[string]Record, error) {
	store, err := Load(d)
	if err != nil {
		return nil, err
	}
	if store == nil || len(store.Records) == 0 {
		return map[string]Record{}, nil
	}
	out := make(map[string]Record, len(store.Records))
	for k, v := range store.Records {
		out[k] = v
	}
	return out, nil
}

// MigrateLegacyFromDaemonState folds mergeability entries still embedded in a
// pre-refactor daemon state file into the shared store. Entries already present
// in the store are left unchanged.
func MigrateLegacyFromDaemonState(d *tasks.Deps, legacy map[string]Record) error {
	if len(legacy) == 0 {
		return nil
	}
	store, err := Load(d)
	if err != nil {
		return err
	}
	changed := false
	for key, rec := range legacy {
		if _, ok := store.Get(key); ok {
			continue
		}
		store.Put(key, rec)
		changed = true
	}
	if changed {
		return Save(d, store)
	}
	return nil
}

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
