package tasks

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/glebglazov/pop/store"
)

// BindingEntry is the layer-2 Worktree binding for one Task set at the tasks
// boundary, keyed (by the caller) per repository identity plus set id. It mirrors
// store.Binding; the binding package wraps it in its own Binding façade so its
// callers are unchanged by the move off bindings.json (ADR-0055).
type BindingEntry struct {
	RuntimePath string
	Branch      string
	Project     string
	Provisioned bool
}

// legacyBindingsFile is the standalone JSON binding store ADR-0055 retires. Its
// contents are folded into the global store on first read, then the file is
// removed. It lived beside the per-repo storage tree, in pop's data dir.
const legacyBindingsFile = "bindings.json"

// LegacyBindingsPath returns the retired standalone binding store file path.
func LegacyBindingsPath(d *Deps) string {
	return filepath.Join(popDataDirWith(d), legacyBindingsFile)
}

// LoadBindingEntries returns every stored Worktree binding keyed by its scoped
// key. It first migrates any surviving bindings.json into the store (preserving
// each binding's provisioned bit) and retires the file, then reads from the
// store. It opens the store only when it already exists, so a pure reader with
// no legacy file never materialises an empty database.
func LoadBindingEntries(d *Deps) (map[string]BindingEntry, error) {
	if err := migrateLegacyBindingsFile(d); err != nil {
		return nil, err
	}
	s, ok, err := openDrainStoreIfExists(d)
	if err != nil || !ok {
		return map[string]BindingEntry{}, err
	}
	defer func() { _ = s.Close() }()
	rows, err := s.AllBindings()
	if err != nil {
		return nil, err
	}
	out := make(map[string]BindingEntry, len(rows))
	for key, b := range rows {
		out[key] = bindingEntryFromStore(b)
	}
	return out, nil
}

// SaveBindingEntries replaces the entire stored binding set with all, creating
// the store on first write. It mirrors the whole-store rewrite the file-backed
// store used.
func SaveBindingEntries(d *Deps, all map[string]BindingEntry) error {
	s, err := openDrainStore(d)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	rows := make(map[string]store.Binding, len(all))
	for key, e := range all {
		rows[key] = storeBindingFromEntry(key, e)
	}
	return s.ReplaceAllBindings(rows)
}

// PutBindingEntry upserts one binding entry under scopedKey.
func PutBindingEntry(d *Deps, scopedKey string, e BindingEntry) error {
	s, err := openDrainStore(d)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	return s.PutBinding(storeBindingFromEntry(scopedKey, e))
}

// DeleteBindingEntry forgets the binding under scopedKey. It opens the store
// only when it already exists.
func DeleteBindingEntry(d *Deps, scopedKey string) error {
	s, ok, err := openDrainStoreIfExists(d)
	if err != nil || !ok {
		return err
	}
	defer func() { _ = s.Close() }()
	return s.DeleteBinding(scopedKey)
}

// migrateLegacyBindingsFile folds a surviving bindings.json into the store and
// removes the file. A missing file is the steady state after the one-time
// migration and costs only the read miss — no store is opened. Every active
// binding and its provisioned bit is preserved; an entry already present in the
// store is left untouched (the store wins).
func migrateLegacyBindingsFile(d *Deps) error {
	path := LegacyBindingsPath(d)
	data, err := d.FS.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var legacy struct {
		Bindings map[string]struct {
			RuntimePath string `json:"runtime_path"`
			Branch      string `json:"branch"`
			Project     string `json:"project"`
			Provisioned bool   `json:"provisioned"`
		} `json:"bindings"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		return fmt.Errorf("parse legacy bindings file: %w", err)
	}
	if len(legacy.Bindings) > 0 {
		s, err := openDrainStore(d)
		if err != nil {
			return err
		}
		existing, err := s.AllBindings()
		if err != nil {
			_ = s.Close()
			return err
		}
		for key, b := range legacy.Bindings {
			if _, ok := existing[key]; ok {
				continue
			}
			if err := s.PutBinding(store.Binding{
				ScopedKey:   key,
				RuntimePath: b.RuntimePath,
				Branch:      b.Branch,
				Project:     b.Project,
				Provisioned: b.Provisioned,
			}); err != nil {
				_ = s.Close()
				return err
			}
		}
		if err := s.Close(); err != nil {
			return err
		}
	}
	// Retire the file once its contents are safely in the store.
	return d.FS.RemoveAll(path)
}

func bindingEntryFromStore(b store.Binding) BindingEntry {
	return BindingEntry{
		RuntimePath: b.RuntimePath,
		Branch:      b.Branch,
		Project:     b.Project,
		Provisioned: b.Provisioned,
	}
}

func storeBindingFromEntry(key string, e BindingEntry) store.Binding {
	return store.Binding{
		ScopedKey:   key,
		RuntimePath: e.RuntimePath,
		Branch:      e.Branch,
		Project:     e.Project,
		Provisioned: e.Provisioned,
	}
}
