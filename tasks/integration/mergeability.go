// Package integration owns Mergeability persistence, the Integrate verb, and
// Integration backlog helpers. It is trigger-agnostic: both `pop queue run` and
// `pop tasks implement` read and write the same mergeability store beside the
// binding store, not in queue daemon state (ADR-0036, ADR-0046).
package integration

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebglazov/pop/tasks/binding"
	"github.com/glebglazov/pop/tasks"
)

const (
	StatusClean     = "clean"
	StatusConflicts = "conflicts"
)

// Record records whether a DONE set branch can be textually merged into the
// working branch. It is advisory; pop never integrates automatically except
// when the repository opts in via auto_merge_clean.
type Record struct {
	Project     string    `json:"project,omitempty"`
	RuntimePath string    `json:"runtime_path"`
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

// StorePath returns the shared mergeability store file path beside bindings.json.
func StorePath(d *tasks.Deps) string {
	return filepath.Join(filepath.Dir(binding.StorePath(d)), "mergeability.json")
}

// Load reads the shared mergeability store. A missing file yields an empty
// store. Legacy entries embedded in queue daemon state are migrated on first
// read via MigrateLegacyFromDaemonState.
func Load(d *tasks.Deps) (*Store, error) {
	data, err := d.FS.ReadFile(StorePath(d))
	if errors.Is(err, os.ErrNotExist) {
		return &Store{}, nil
	}
	if err != nil {
		return nil, err
	}
	var store Store
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, fmt.Errorf("parse mergeability store: %w", err)
	}
	migrateStoreKeys(&store)
	return &store, nil
}

// Save writes the shared mergeability store atomically.
func Save(d *tasks.Deps, store *Store) error {
	if store == nil {
		store = &Store{}
	}
	payload, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	return tasks.WriteAtomicWith(d, StorePath(d), append(payload, '\n'), 0o644)
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

func migrateStoreKeys(store *Store) {
	if store == nil || len(store.Records) == 0 {
		return
	}
	targets := map[string][]string{}
	for oldKey, rec := range store.Records {
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
		if _, exists := store.Records[newKey]; exists {
			continue
		}
		store.Records[newKey] = store.Records[oldKeys[0]]
		delete(store.Records, oldKeys[0])
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
	return binding.ScopedKey(repoKey, setID), true
}

func isLegacyScopedKey(prefix string) bool {
	return strings.HasPrefix(prefix, "/") || strings.Contains(prefix, "\\")
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

func setIDFromScopedKey(key string) string {
	parts := strings.Split(key, "\x00")
	if len(parts) != 2 {
		return ""
	}
	return parts[1]
}
