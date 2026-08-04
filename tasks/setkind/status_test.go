package setkind

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/repogroup"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/work"
)

// archiveFixture registers one DONE set in real Task state and returns the kind
// over it, so the archive pair is exercised through the writer that actually
// hides a row rather than through a stub.
func archiveFixture(t *testing.T) (*Deps, repogroup.Group) {
	t.Helper()
	td := workDataDeps(t)
	tasksDir := filepath.Join(t.TempDir(), "tasks")
	statePath := tasks.StatePathFor(tasksDir)
	canon, err := tasks.CanonicalDefinitionPathWith(td, tasksDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := tasks.UpdateGlobalStateWith(td, statePath, func(state *tasks.GlobalState) error {
		if state.Tasks == nil {
			state.Tasks = map[string]*tasks.TaskEntry{}
		}
		state.Tasks[canon] = &tasks.TaskEntry{TaskSets: []tasks.RegisteredTaskSet{{ID: "2026-07-01-demo"}}}
		return nil
	}); err != nil {
		t.Fatalf("seed state: %v", err)
	}
	d := &Deps{Tasks: td, Config: &config.Config{}}
	group := repogroup.Group{
		DefPath:     canon,
		StatePath:   statePath,
		StorageDir:  filepath.Dir(canon),
		RepoKey:     "repo-key",
		ProjectName: "pop",
		Rep:         &repogroup.Checkout{Name: "pop", ProjectPath: "/repo/main", RuntimePath: "/repo/main"},
	}
	return d, group
}

func setIDs(t *testing.T, d *Deps, g repogroup.Group) []string {
	t.Helper()
	containers, err := containersForGroup(d, d.config(), g)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, c := range containers {
		ids = append(ids, c.ID)
	}
	return ids
}

// The archive pair the status submenu offers, performed through the kind: each
// writes the reversible flag in Task state and the next load reflects it. Both
// halves are the same flag, which is why one verb id per direction is all there is.
func TestSetKindArchivePairWritesTheFlag(t *testing.T) {
	d, g := archiveFixture(t)
	k := New(d)
	row := work.Container{ID: "2026-07-01-demo", DefPath: g.DefPath, StatePath: g.StatePath}

	if ids := setIDs(t, d, g); !slices.Contains(ids, "2026-07-01-demo") {
		t.Fatalf("set missing before archiving: %v", ids)
	}
	out, err := k.Perform(row, nil, VerbArchive)
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	if out.Kind != work.OutcomeRefresh || !strings.Contains(out.Message, "archived") {
		t.Fatalf("archive outcome = %+v, want a refresh naming the write", out)
	}
	if ids := setIDs(t, d, g); slices.Contains(ids, "2026-07-01-demo") {
		t.Fatalf("archived set still listed by default: %v", ids)
	}

	// With the show-archived view on the row comes back, says it is archived, and
	// can be unarchived — which is the whole reason the view exists. It comes back
	// even though the set is DONE and Done-inclusion is off: an archived row is on
	// screen because the operator asked for archived rows.
	d.IncludeArchived = true
	containers, err := containersForGroup(d, d.config(), g)
	if err != nil {
		t.Fatal(err)
	}
	if len(containers) != 1 || !containers[0].Archived {
		t.Fatalf("show-archived containers = %+v, want one archived row", containers)
	}
	if cell := work.StatusCellText(k.StatusCell(containers[0])); !strings.Contains(cell, "archived") {
		t.Fatalf("status cell = %q, want it to name the archived state", cell)
	}
	if _, err := k.Perform(containers[0], nil, VerbUnarchive); err != nil {
		t.Fatalf("unarchive: %v", err)
	}
	d.IncludeArchived = false
	if ids := setIDs(t, d, g); !slices.Contains(ids, "2026-07-01-demo") {
		t.Fatalf("unarchived set missing from the default view: %v", ids)
	}
}

// A DONE set that was never archived stays hidden with only show-archived on: the
// two view flags are independent, and show-archived is not a second show-done.
func TestShowArchivedDoesNotRevealPlainDoneSets(t *testing.T) {
	d, g := archiveFixture(t)
	d.IncludeArchived = true
	d.Refresh = func(string) (*tasks.RefreshResult, error) {
		return &tasks.RefreshResult{Rows: []tasks.Row{
			{ID: "done-set", Status: tasks.StatusDone},
			{ID: "archived-set", Status: tasks.StatusDone, Archived: true},
		}}, nil
	}
	ids := setIDs(t, d, g)
	if slices.Contains(ids, "done-set") {
		t.Fatalf("show-archived revealed a plain DONE set: %v", ids)
	}
	if !slices.Contains(ids, "archived-set") {
		t.Fatalf("archived DONE set missing: %v", ids)
	}
}

// The status submenu's complete/open/skip are the whole-set writes: the same verb
// ids the item menu uses, told apart by whether Perform was handed a task.
func TestSetKindStatusVerbsAreWholeSetWrites(t *testing.T) {
	k := New(&Deps{Tasks: workDataDeps(t)})
	var status []work.Verb
	for _, a := range k.StatusActions(work.Container{ID: "2026-07-01-demo"}) {
		status = append(status, a.Verb)
	}
	for _, verb := range []work.Verb{VerbComplete, VerbOpen, VerbSkip} {
		if !slices.Contains(status, verb) {
			t.Fatalf("status submenu missing %q: %v", verb, status)
		}
		if _, err := k.Perform(work.Container{ID: "2026-07-01-demo"}, nil, verb); err == nil {
			t.Fatalf("%q with no item resolved no task set and still succeeded", verb)
		} else if strings.Contains(err.Error(), "is an item verb") {
			t.Fatalf("%q refused the whole-set form: %v", verb, err)
		}
	}
}
