package work_test

import (
	"slices"
	"testing"
	"time"

	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/setkind"
	"github.com/glebglazov/pop/wayfinder"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// TestCreationOrderInterleavesTaskSetsAndMaps is the whole point of ADR-0210 over
// the two real adapters: the fixture's Map is dated between the two task sets, and
// under creation order it renders between them instead of hanging off the bottom
// of the page in the trailing Map block. Both directions are reachable, and
// reversing the value reverses the page.
func TestCreationOrderInterleavesTaskSetsAndMaps(t *testing.T) {
	f := newFixture(t)

	desc, err := work.BuildSnapshotForPane(straddlingPage(f), work.PaneFacts{}, work.BuildOptions{Ordering: work.OrderByCreatedDesc})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"task-set:2026-08-01-newer",
		"map:2026-07-01-chart",
		"task-set:2026-06-01-older",
	}
	if got := rowKeys(desc); !slices.Equal(got, want) {
		t.Fatalf("created_desc order = %v, want %v", got, want)
	}

	asc, err := work.BuildSnapshotForPane(straddlingPage(f), work.PaneFacts{}, work.BuildOptions{Ordering: work.OrderByCreatedAsc})
	if err != nil {
		t.Fatal(err)
	}
	slices.Reverse(want)
	if got := rowKeys(asc); !slices.Equal(got, want) {
		t.Fatalf("created_asc order = %v, want %v", got, want)
	}

	// The header still counts per kind: the blocks are gone, the tallies are not.
	if want := "2 task sets · 2 ready · 1 map"; desc.SummaryLine() != want {
		t.Fatalf("summary line = %q, want %q", desc.SummaryLine(), want)
	}
}

// straddlingPage is twoKindPage with its task sets dated either side of the
// fixture's 2026-07-01 Map — the smallest page on which "interleaved" and
// "kind-partitioned" produce different sequences.
func straddlingPage(f fixture) []work.Kind {
	sets := setkind.New(&setkind.Deps{
		Tasks:      f.tasks,
		Project:    f.project,
		Config:     f.cfg,
		Groups:     f.groups,
		LiveDrains: func() ([]tasks.RunningDrain, error) { return nil, nil },
		Refresh: func(string) (*tasks.RefreshResult, error) {
			return &tasks.RefreshResult{Rows: []tasks.Row{
				{ID: "2026-06-01-older", Status: tasks.StatusReady},
				{ID: "2026-08-01-newer", Status: tasks.StatusReady},
			}}, nil
		},
	})
	maps := wayfinder.NewMapKind(&wayfinder.MapKindDeps{
		Wayfinder: &wayfinder.Deps{FS: f.fs, Tasks: f.tasks},
		Project:   f.project,
		Config:    f.cfg,
		Groups:    f.groups,
	})
	return []work.Kind{maps, sets}
}

// TestCreationOrderKeysUnderTheDate pins the keys below the date — kind
// precedence, then the owning kind's own comparator — and that nothing sits
// above it: a live drain is ordered by its date like every other row (ADR-0210
// as amended). Stub kinds carry the containers because every one of those keys
// is the builder's — what the rules need from a kind is a date and a comparator,
// not a filesystem.
func TestCreationOrderKeysUnderTheDate(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2026, time.July, d, 0, 0, 0, 0, time.UTC) }
	sets := stubKind{id: ref.KindTaskSet, containers: []work.Container{
		// The oldest row on the page, held by a live drain: liveness is a STATUS-cell
		// fact and no longer a position, so it sits at its date like anything else.
		{ID: "2026-07-01-draining", CreatedAt: day(1), LiveDrain: true},
		{ID: "2026-07-09-newest", CreatedAt: day(9)},
		{ID: "2026-07-05-tie", CreatedAt: day(5)},
		// Two undated sets, loaded in the order the kind's comparator does not want.
		{ID: "b-undated"},
		{ID: "a-undated"},
	}}
	maps := stubKind{id: ref.KindMap, containers: []work.Container{
		{ID: "2026-07-05-tie-map", CreatedAt: day(5)},
		{ID: "z-undated-map"},
	}}

	snap, err := work.BuildSnapshotForPane([]work.Kind{maps, sets}, work.PaneFacts{}, work.BuildOptions{Ordering: work.OrderByCreatedDesc})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"task-set:2026-07-09-newest",
		// An exact date tie between two kinds is a real case, and kind precedence is
		// what is left to break it.
		"task-set:2026-07-05-tie",
		"map:2026-07-05-tie-map",
		"task-set:2026-07-01-draining",
		// A zero date is no opinion: every undated row sinks below every dated one,
		// and among themselves they fall through to kind precedence and then to the
		// owning kind's comparator.
		"task-set:a-undated",
		"task-set:b-undated",
		"map:z-undated-map",
	}
	if got := rowKeys(snap); !slices.Equal(got, want) {
		t.Fatalf("created_desc order = %v, want %v", got, want)
	}
}

// rowKeys renders a snapshot's order as `kind:id`, which is the only thing these
// tests read off a container.
func rowKeys(snap work.Snapshot) []string {
	keys := make([]string, 0, len(snap.Containers))
	for _, c := range snap.Containers {
		keys = append(keys, string(c.Kind)+":"+c.ID)
	}
	return keys
}

// stubKind is a Work kind that loads the containers a test handed it and orders
// its own by id ascending — a comparator chosen so that a fall-through to it is
// visible in the result rather than indistinguishable from load order.
type stubKind struct {
	id         work.KindID
	containers []work.Container
}

func (k stubKind) ID() work.KindID                                { return k.id }
func (k stubKind) Load() ([]work.Container, error)                { return k.containers, nil }
func (k stubKind) Less(a, b work.Container) bool                  { return a.ID < b.ID }
func (k stubKind) StatusCell(work.Container) []work.StatusSegment { return nil }
func (k stubKind) Actions(work.Container) []work.Action           { return nil }
func (k stubKind) StatusActions(work.Container) []work.Action     { return nil }
func (k stubKind) CopyActions(work.Container) []work.Action       { return nil }
func (k stubKind) ItemActions(work.Container, work.Item) []work.Action {
	return nil
}
func (k stubKind) Perform(_ work.Container, _ *work.Item, verb work.Verb) (work.Outcome, error) {
	return work.Outcome{}, work.UnknownVerb(k.id, verb)
}
func (k stubKind) Summary([]work.Container) []string { return nil }
func (k stubKind) Columns() []string                 { return nil }
