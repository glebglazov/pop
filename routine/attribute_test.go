package routine

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// paneRoutines builds three Routines bound to a checkout of one project and
// returns the kind a page of Routines is read through, plus that checkout.
func paneRoutines(t *testing.T) (*Kind, string) {
	t.Helper()
	root := t.TempDir()
	here := filepath.Join(root, "main")
	mkdirs(t, here)
	d := routineDeps(t, filepath.Join(root, "data"))
	for _, id := range []string{"aaa-audit", "mmm-mirror", "zzz-sweep"} {
		if _, err := AddWith(d, id, "daily at 10:00", here); err != nil {
			t.Fatal(err)
		}
	}
	checkout := canonical(t, here)
	k := kindOver(d, checkout, "pop", []project.ExpandedProject{
		{Name: "pop", ProjectLabel: "pop", Path: checkout},
	})
	return k, checkout
}

func routineRowIDs(snap work.Snapshot) []string {
	ids := make([]string, 0, len(snap.Containers))
	for _, c := range snap.Containers {
		ids = append(ids, c.ID)
	}
	return ids
}

func buildForPane(t *testing.T, k *Kind, facts work.PaneFacts) work.Snapshot {
	t.Helper()
	snap, err := work.BuildSnapshotForPane([]work.Kind{k}, facts, work.BuildOptions{Ordering: work.OrderByKindPrecedence, LiftPane: true})
	if err != nil {
		t.Fatalf("BuildSnapshotForPane: %v", err)
	}
	return snap
}

// The tag rung: the pane pop opened to fire a Routine belongs to that Routine,
// and its row is lifted to the top of the page. `zzz-sweep` sorts last of the
// three under the kind's own comparator, so seeing it first is the whole of the
// lift.
func TestFirePaneLiftsItsRoutine(t *testing.T) {
	k, _ := paneRoutines(t)

	snap := buildForPane(t, k, work.PaneFacts{PaneID: "%7", Session: RoutinesSessionName, Routine: "zzz-sweep"})

	if snap.Attribution == nil {
		t.Fatal("attribution = none, want the tagged routine")
	}
	want := work.AttributedContainer{
		Ref:       ref.WorkRef{Kind: ref.KindRoutine, ContainerID: "zzz-sweep"},
		CursorKey: "routine\x00zzz-sweep",
		Label:     "routine zzz-sweep",
	}
	if got := snap.Attribution.Containers; !slices.Equal(got, []work.AttributedContainer{want}) {
		t.Fatalf("attributed %+v, want just %+v", got, want)
	}
	if got, wantOrder := routineRowIDs(snap), []string{"zzz-sweep", "aaa-audit", "mmm-mirror"}; !slices.Equal(got, wantOrder) {
		t.Fatalf("rows = %v, want the fire pane's routine lifted above the rest: %v", got, wantOrder)
	}
	if !snap.Containers[0].Lifted {
		t.Fatal("the lifted row does not say it is lifted")
	}
	for _, c := range snap.Containers[1:] {
		if c.Lifted {
			t.Fatalf("%s is lifted too, want only the tagged routine", c.ID)
		}
	}
}

// There is no neighbourhood rung. A Routine is project-scoped, so a shell merely
// standing in a project's checkout — the directory every one of these Routines is
// bound to — attributes to no Routine at all, rather than lifting the project's
// whole routine list.
func TestShellInAProjectDirectoryLiftsNoRoutine(t *testing.T) {
	k, checkout := paneRoutines(t)
	baseline := routineRowIDs(buildForPane(t, k, work.PaneFacts{}))

	snap := buildForPane(t, k, work.PaneFacts{PaneID: "%3", Session: "pop", Directory: checkout})

	if snap.Attribution != nil {
		t.Fatalf("attribution = %+v, want none for a shell that is merely standing there", *snap.Attribution)
	}
	if got := routineRowIDs(snap); !slices.Equal(got, baseline) {
		t.Fatalf("rows = %v, want the untouched order %v", got, baseline)
	}
	for _, c := range snap.Containers {
		if c.Lifted {
			t.Fatalf("%s is lifted, want nothing lifted", c.ID)
		}
	}
}

// A tag naming a Routine this load did not find gets the same silence: the pane
// outlived the Routine it was opened for.
func TestFirePaneOfADeletedRoutineLiftsNothing(t *testing.T) {
	k, _ := paneRoutines(t)

	snap := buildForPane(t, k, work.PaneFacts{PaneID: "%7", Routine: "gone-yesterday"})

	if snap.Attribution != nil {
		t.Fatalf("attribution = %+v, want none", *snap.Attribution)
	}
}
