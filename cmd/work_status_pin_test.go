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
// a pinning build actually pin.
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
// with empty pane facts, so no preset's `pin` can reorder or mark it. The same
// rows under a pinning preset are the dashboard's pinned page and status' plain
// one, which is the pair this test holds together.
func TestWorkStatusNeverPinsUnderAnyPreset(t *testing.T) {
	day := func(month time.Month) time.Time { return time.Date(2026, month, 1, 0, 0, 0, 0, time.UTC) }
	sets := attributingStubKind{
		orderingStubKind: orderingStubKind{id: ref.KindTaskSet, containers: []work.Container{
			{ID: "2026-06-01-older", CursorKey: "task-set:2026-06-01-older", CreatedAt: day(time.June)},
			{ID: "2026-08-01-newer", CursorKey: "task-set:2026-08-01-newer", CreatedAt: day(time.August)},
		}},
		paneID:      "%5",
		containerID: "2026-06-01-older",
	}
	unpinned := []string{"task-set:2026-08-01-newer", "task-set:2026-06-01-older"}

	cfg := &config.Config{}
	for _, pin := range []bool{false, true} {
		d := &drain.Deps{
			ViewPreset:   config.WorkViewPreset{Name: "fixture", Pin: pin},
			Kinds:        func(*drain.Deps, *config.Config) []work.Kind { return []work.Kind{sets} },
			RoutineKinds: func(*drain.Deps, *config.Config) []work.Kind { return nil },
		}

		tables, err := workBuildStatusTables(d, cfg)
		if err != nil {
			t.Fatalf("workBuildStatusTables(pin=%v): %v", pin, err)
		}
		if got := orderingRowKeys(tables.TaskSets.Rows); !slices.Equal(got, unpinned) {
			t.Fatalf("status order under pin=%v = %v, want the sorted order %v", pin, got, unpinned)
		}
		for _, row := range tables.TaskSets.Rows {
			if row.Pinned {
				t.Fatalf("status marked %s under pin=%v: it never has a pane to attribute", row.ID, pin)
			}
		}

		// The same preset on a page launched from the attributed pane: that is where
		// the grant lands, and it is what makes the identical status output a fact
		// about the surface rather than about a fixture that could not pin.
		page, err := dashboard.BuildPageSnapshot(d, cfg, dashboard.PageWork, work.PaneFacts{PaneID: "%5"})
		if err != nil {
			t.Fatalf("BuildPageSnapshot(pin=%v): %v", pin, err)
		}
		wantPage := unpinned
		if pin {
			wantPage = []string{"task-set:2026-06-01-older", "task-set:2026-08-01-newer"}
		}
		if got := orderingRowKeys(page.Containers); !slices.Equal(got, wantPage) {
			t.Fatalf("dashboard order under pin=%v = %v, want %v", pin, got, wantPage)
		}
		if page.Attribution == nil {
			t.Fatalf("dashboard attribution under pin=%v = none, want the pane's container either way", pin)
		}
	}
}
