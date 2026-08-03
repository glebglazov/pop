package wayfinder

import (
	"path/filepath"
	"strings"

	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/work"
)

// The read side of the Map's lineage field: turning the bare set ids
// `spawned_sets` holds into what those sets are doing right now. The ids are the
// record; every status here is derived on the render that shows it and stored
// nowhere, so a Map never carries a stale copy of another container's state.

// SpawnedSet is one set a Map spawned, resolved against the sets on disk.
type SpawnedSet struct {
	ID string
	// Status is the Task-set display label the dashboard shows for the set, empty
	// when nothing on this machine answers to the id.
	Status string
	// Progress is the set's task tally ("3/5 done, 2 open"), empty when unresolved.
	Progress string
	// Archived reports a set that has been archived. It is a normal end state of a
	// spawned set, not a defect: the set still renders, with its status.
	Archived bool
	// Missing reports an id that resolved to no set — deleted, moved, or never
	// registered. The line still renders: the Map is the record of what the effort
	// spawned, and dropping the row would rewrite that history.
	Missing bool
}

// Line renders one spawned set for a read surface, in the STATUS-cell house
// style: the label first, then plain suffixes.
func (s SpawnedSet) Line() string {
	if s.Missing {
		return s.ID + " — (missing)"
	}
	parts := []string{s.Status}
	if s.Progress != "" {
		parts = append(parts, s.Progress)
	}
	if s.Archived {
		parts = append(parts, "archived")
	}
	return s.ID + " — " + strings.Join(parts, " · ")
}

// ResolveSpawnedSets reads the live status of every set a Map spawned, in the
// order the Map recorded them. It is the one derivation both read surfaces use —
// the dashboard's detail pane and `pop map show` — so the two can never disagree.
func ResolveSpawnedSets(d *Deps, m Map) []SpawnedSet {
	return newSetStatusTable(d, defPathForMap(m)).resolve(m.SpawnedSets)
}

// setStatusTable resolves set ids against one Task-storage definition path,
// reading the registered sets at most once. The Work dashboard walks every Map of
// a repository group in a single pass, and those Maps all spawn into the same
// storage, so a refresh per Map would read the same rows over and over.
type setStatusTable struct {
	d       *Deps
	defPath string
	rows    map[string]SpawnedSet
	loaded  bool
}

func newSetStatusTable(d *Deps, defPath string) *setStatusTable {
	return &setStatusTable{d: d, defPath: defPath}
}

// resolve answers one Map's ids. A set that the table has no row for renders as
// missing rather than being dropped, and an unreadable storage makes every id
// missing rather than failing the render: a Map that cannot be shown because one
// spawned set went away is worse than a Map that says so.
func (t *setStatusTable) resolve(ids []string) []SpawnedSet {
	if len(ids) == 0 {
		return nil
	}
	if !t.loaded {
		t.rows = t.load()
		t.loaded = true
	}
	out := make([]SpawnedSet, 0, len(ids))
	for _, id := range ids {
		if row, ok := t.rows[id]; ok {
			out = append(out, row)
			continue
		}
		out = append(out, SpawnedSet{ID: id, Missing: true})
	}
	return out
}

func (t *setStatusTable) load() map[string]SpawnedSet {
	if t.d != nil && t.d.SetStatuses != nil {
		rows, err := t.d.SetStatuses(t.defPath)
		if err != nil {
			return nil
		}
		return rows
	}
	return refreshSetStatuses(t.d, t.defPath)
}

// refreshSetStatuses reads the sets registered under defPath through the Task-set
// refresh — the same read the dashboard's rows come from — and projects each row
// onto its display label. Archived sets are a second read because the refresh
// answers one side at a time; they belong in the table, since a spawned set being
// archived is how a finished handoff normally ends.
func refreshSetStatuses(d *Deps, defPath string) map[string]SpawnedSet {
	if strings.TrimSpace(defPath) == "" {
		return nil
	}
	td := d.taskDeps()
	statePath := tasks.StatePathFor(defPath)
	rows := map[string]SpawnedSet{}
	if refresh, err := tasks.RefreshWith(td, defPath, statePath); err == nil {
		collectSetStatuses(rows, refresh, false)
	}
	if refresh, err := tasks.RefreshArchivedWith(td, defPath, statePath); err == nil {
		collectSetStatuses(rows, refresh, true)
	}
	return rows
}

func collectSetStatuses(rows map[string]SpawnedSet, refresh *tasks.RefreshResult, archived bool) {
	if refresh == nil {
		return
	}
	for _, row := range refresh.Rows {
		rows[row.ID] = SpawnedSet{
			ID: row.ID,
			// The dashboard's own label composer, over the fields a refresh answers:
			// the derived status with the READY→IN PROGRESS refinement. A spawned set
			// is read from outside its repository group, so the overlays that need a
			// group's store snapshot (live drain, park, orphan) are not in hand here.
			Status:   tasks.WorkRowStatusLabel(work.Container{RawStatus: row.Status, Started: row.Started}),
			Progress: row.Progress,
			Archived: archived,
		}
	}
}

// defPathForMap derives the Task-set definition path a Map's spawned sets live
// under from the Map's own directory: both are containers of one repository's
// Task storage, `<storage>/maps/<id>` beside `<storage>/tasks`.
func defPathForMap(m Map) string {
	if strings.TrimSpace(m.Dir) == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(filepath.Dir(m.Dir)), tasksDirName)
}

const tasksDirName = "tasks"
