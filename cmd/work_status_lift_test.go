package cmd

import (
	"slices"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/dashboard"
	"github.com/glebglazov/pop/tasks/drain"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// attributingStubKind is an orderingStubKind that recognises one pane as one of
// its containers' own, which is the strongest rung of the ladder — enough to make
// a lifting build actually lift.
type attributingStubKind struct {
	orderingStubKind
	paneID, containerID string
}

func (k attributingStubKind) AttributePane(facts work.PaneFacts) (work.Attribution, bool) {
	if facts.PaneID != k.paneID {
		return work.Attribution{}, false
	}
	return work.AttributeOne(work.AttributedContainer{
		Ref:       ref.WorkRef{Kind: ref.Kind(k.id), ContainerID: k.containerID},
		CursorKey: string(k.id) + ":" + k.containerID,
		Label:     "task set " + k.containerID,
	}), true
}

// `pop work status` is a printed list, not a page anyone is standing in: it builds
// with empty pane facts, so no preset's `lift` can reorder or mark it. The same
// rows under a lifting preset are the dashboard's lifted page and status' plain
// one, which is the pair this test holds together.
func TestWorkStatusNeverLiftsUnderAnyPreset(t *testing.T) {
	day := func(month time.Month) time.Time { return time.Date(2026, month, 1, 0, 0, 0, 0, time.UTC) }
	sets := attributingStubKind{
		orderingStubKind: orderingStubKind{id: ref.KindTaskSet, containers: []work.Container{
			{ID: "2026-06-01-older", CursorKey: "task-set:2026-06-01-older", CreatedAt: day(time.June)},
			{ID: "2026-08-01-newer", CursorKey: "task-set:2026-08-01-newer", CreatedAt: day(time.August)},
		}},
		paneID:      "%5",
		containerID: "2026-06-01-older",
	}
	unlifted := []string{"task-set:2026-08-01-newer", "task-set:2026-06-01-older"}

	cfg := &config.Config{}
	for _, lift := range []bool{false, true} {
		d := &drain.Deps{
			ViewPreset:   config.WorkViewPreset{Name: "fixture", Lift: lift},
			Kinds:        func(*drain.Deps, *config.Config) []work.Kind { return []work.Kind{sets} },
			RoutineKinds: func(*drain.Deps, *config.Config) []work.Kind { return nil },
		}

		tables, err := workBuildStatusTables(d, cfg)
		if err != nil {
			t.Fatalf("workBuildStatusTables(lift=%v): %v", lift, err)
		}
		if got := orderingRowKeys(tables.TaskSets.Rows); !slices.Equal(got, unlifted) {
			t.Fatalf("status order under lift=%v = %v, want the sorted order %v", lift, got, unlifted)
		}
		for _, row := range tables.TaskSets.Rows {
			if row.Lifted {
				t.Fatalf("status marked %s under lift=%v: it never has a pane to attribute", row.ID, lift)
			}
		}

		// The same preset on a page launched from the attributed pane: that is where
		// the grant lands, and it is what makes the identical status output a fact
		// about the surface rather than about a fixture that could not lift.
		page, err := dashboard.BuildPageSnapshot(d, cfg, dashboard.PageWork, work.PaneFacts{PaneID: "%5"})
		if err != nil {
			t.Fatalf("BuildPageSnapshot(lift=%v): %v", lift, err)
		}
		wantPage := unlifted
		if lift {
			wantPage = []string{"task-set:2026-06-01-older", "task-set:2026-08-01-newer"}
		}
		if got := orderingRowKeys(page.Containers); !slices.Equal(got, wantPage) {
			t.Fatalf("dashboard order under lift=%v = %v, want %v", lift, got, wantPage)
		}
		if page.Attribution == nil {
			t.Fatalf("dashboard attribution under lift=%v = none, want the pane's container either way", lift)
		}
	}
}
