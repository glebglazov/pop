package integration

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
)

// RecordMergeability persists a mergeability record for a completed set.
func RecordMergeability(d *Deps, projectPath string, rec Record) error {
	if d == nil || rec.RuntimePath == "" || rec.SetID == "" {
		return nil
	}
	if rec.CheckedAt.IsZero() {
		rec.CheckedAt = time.Now().UTC()
	}
	scopedKey, err := ScopedKeyForPaths(d.tasksDeps(), projectPath, rec.RuntimePath, rec.SetID)
	if err != nil {
		return err
	}
	store, err := Load(d.tasksDeps())
	if err != nil {
		return err
	}
	if !AwaitsIntegration(rec) {
		store.Delete(scopedKey)
		return Save(d.tasksDeps(), store)
	}
	store.Put(scopedKey, rec)
	return Save(d.tasksDeps(), store)
}

// DeleteRecord removes the mergeability record under key.
func DeleteRecord(td *tasks.Deps, key string) error {
	store, err := Load(td)
	if err != nil {
		return err
	}
	if _, ok := store.Get(key); !ok {
		return nil
	}
	store.Delete(key)
	return Save(td, store)
}

// Lookup returns the mergeability record for setID. It returns (rec, true, nil)
// when a record exists, (zero, false, nil) when absent, and (zero, false, err)
// when the store cannot be read.
func Lookup(d *Deps, setID string) (Record, bool, error) {
	if d == nil {
		d = DefaultDeps()
	}
	store, err := Load(d.tasksDeps())
	if err != nil {
		return Record{}, false, err
	}
	_, rec, ok, err := findRecord(store, setID)
	return rec, ok, err
}

// GetForSet returns the mergeability record keyed by repoKey and setID.
func GetForSet(td *tasks.Deps, repoKey, setID string) (Record, bool, error) {
	store, err := Load(td)
	if err != nil {
		return Record{}, false, err
	}
	rec, ok := store.Get(binding.ScopedKey(repoKey, setID))
	if ok && !AwaitsIntegration(rec) {
		return Record{}, false, nil
	}
	return rec, ok, nil
}

func findRecord(store *Store, setID string) (key string, rec Record, ok bool, err error) {
	if store == nil || len(store.Records) == 0 {
		return "", Record{}, false, nil
	}
	setID = strings.TrimSpace(setID)
	var keys []string
	for k, r := range store.Records {
		if r.SetID == setID && AwaitsIntegration(r) {
			keys = append(keys, k)
		}
	}
	switch len(keys) {
	case 0:
		return "", Record{}, false, nil
	case 1:
		return keys[0], store.Records[keys[0]], true, nil
	default:
		sort.Strings(keys)
		var b strings.Builder
		fmt.Fprintf(&b, "integration: set %q is ambiguous; awaiting integration in:", setID)
		for _, k := range keys {
			r := store.Records[k]
			fmt.Fprintf(&b, "\n  %s (%s)", r.Project, r.RuntimePath)
		}
		return "", Record{}, false, fmt.Errorf("%s", b.String())
	}
}

// RecordImplementMergeability computes and records Mergeability for a task set
// drained by `pop tasks implement` to Done in a linked worktree. It is a no-op
// when no worktree binding exists or the repository is bare with no main
// working tree to merge into.
func RecordImplementMergeability(d *Deps, projectPath, runtimePath, setID, project string) error {
	if d == nil {
		d = DefaultDeps()
	}
	td := d.tasksDeps()

	store, err := binding.Load(td)
	if err != nil {
		return err
	}
	id, err := tasks.ResolveRepositoryIdentity(td, runtimePath)
	if err != nil {
		return err
	}
	b, ok := store.Get(binding.Key(id, setID))
	if !ok {
		return nil
	}

	mainPath, bare, err := binding.ResolveTrunkPath(td, nil, runtimePath)
	if err != nil {
		return err
	}
	if bare || mainPath == "" {
		return nil
	}

	proj := project
	if proj == "" {
		proj = b.Project
	}

	merge, err := d.computeMergeability(mainPath, runtimePath)
	if err != nil {
		return err
	}
	merge.Project = proj
	merge.RuntimePath = runtimePath
	merge.SetID = setID
	return RecordMergeability(d, projectPath, merge)
}

// RecordCompletionMergeability records Mergeability when a manual task
// completion (CLI `pop tasks complete`, its batch form, or the dashboard `C`
// action) flips a worktree-bound set to Done. It is the manual-completion
// analogue of RecordImplementMergeability: the implement epilogue covers sets a
// drain finished, this covers sets a human concluded by hand, so the
// Integration backlog sees a merge verdict the moment the set becomes
// integrable regardless of how Done was reached (ADR-0051).
//
// It is a no-op unless the set is Done in refresh and has a non-trunk Worktree
// binding (a trunk drain records nothing). Best-effort: callers treat errors as
// advisory, since the completion itself already succeeded and Mergeability is
// recomputed at integrate time.
func RecordCompletionMergeability(d *Deps, projectPath, setID string, refresh *tasks.RefreshResult) error {
	if refresh == nil || setID == "" {
		return nil
	}
	m := refresh.Manifests[setID]
	if m == nil || tasks.DeriveStatus(m) != tasks.StatusDone {
		return nil
	}
	if d == nil {
		d = DefaultDeps()
	}
	td := d.tasksDeps()
	store, err := binding.Load(td)
	if err != nil {
		return err
	}
	id, err := tasks.ResolveRepositoryIdentity(td, projectPath)
	if err != nil {
		return err
	}
	b, ok := store.Get(binding.Key(id, setID))
	if !ok {
		return nil // trunk drain: nothing enters the backlog
	}
	return RecordImplementMergeability(d, projectPath, b.RuntimePath, setID, b.Project)
}

// AwaitingIntegration lists every set in the Integration backlog.
func AwaitingIntegration(td *tasks.Deps) ([]Record, error) {
	store, err := Load(td)
	if err != nil {
		return nil, err
	}
	if store == nil || len(store.Records) == 0 {
		return nil, nil
	}
	out := make([]Record, 0, len(store.Records))
	for _, rec := range store.Records {
		if AwaitsIntegration(rec) {
			out = append(out, rec)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Project != out[j].Project {
			return out[i].Project < out[j].Project
		}
		return out[i].SetID < out[j].SetID
	})
	return out, nil
}

// AwaitsIntegration reports whether a mergeability record represents a real
// integration backlog item. Records with identical target and source commits
// are no-ops left by older queue supervisor builds that computed mergeability
// for an in-place trunk drain.
func AwaitsIntegration(rec Record) bool {
	return rec.Target == "" || rec.Source == "" || rec.Target != rec.Source
}

// ProjectForScopedKey returns the project label for a scoped store key by
// consulting mergeability records and bindings.
func ProjectForScopedKey(td *tasks.Deps, key string) string {
	setID := binding.SetIDFromKey(key)
	store, err := Load(td)
	if err == nil {
		for _, rec := range store.Records {
			if rec.SetID == setID {
				return rec.Project
			}
		}
	}
	bindings, _ := binding.AllBindings(td)
	for _, b := range bindings {
		if b.Project != "" {
			for k := range bindings {
				if binding.SetIDFromKey(k) == setID {
					return b.Project
				}
			}
		}
	}
	for _, b := range bindings {
		if b.Project != "" {
			return b.Project
		}
	}
	return ""
}
