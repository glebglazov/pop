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

// TestStatusAndDashboardReadOneOrderingUnderThePreset drives ADR-0210's edge:
// the active preset's sort is translated into the builder's ordering here, in
// the layer that knows about config, and both Work read surfaces read the same
// translation. The fixture's Map is dated between the two task sets, which is
// the smallest page where "interleaved" and "kind-partitioned" differ — so the
// default preset must render it between them, and `sort = "status"` must put it
// back in the trailing Map block.
func TestStatusAndDashboardReadOneOrderingUnderThePreset(t *testing.T) {
	day := func(month time.Month) time.Time { return time.Date(2026, month, 1, 0, 0, 0, 0, time.UTC) }
	sets := orderingStubKind{id: ref.KindTaskSet, containers: []work.Container{
		{ID: "2026-06-01-older", CreatedAt: day(time.June)},
		{ID: "2026-08-01-newer", CreatedAt: day(time.August)},
	}}
	maps := orderingStubKind{id: ref.KindMap, containers: []work.Container{
		{ID: "2026-07-01-chart", CreatedAt: day(time.July)},
	}}
	interleaved := []string{
		"task-set:2026-08-01-newer",
		"map:2026-07-01-chart",
		"task-set:2026-06-01-older",
	}

	cases := []struct {
		name string
		sort string
		want []string
	}{
		{
			// The whole behavioural change: a preset that says nothing about sorting
			// now says creation order, newest first.
			name: "absent sort interleaves by creation date, newest first",
			sort: "",
			want: interleaved,
		},
		{
			name: "created_desc means the same thing, declared",
			sort: config.PresetSortCreatedDesc,
			want: interleaved,
		},
		{
			name: "created_asc reverses the whole page",
			sort: config.PresetSortCreatedAsc,
			want: []string{
				"task-set:2026-06-01-older",
				"map:2026-07-01-chart",
				"task-set:2026-08-01-newer",
			},
		},
		{
			// The one line that buys the pre-change page back: kind-precedence
			// blocks with the Maps trailing, each block in its own kind's order.
			name: "status restores the kind-precedence blocks",
			sort: config.PresetSortStatus,
			want: []string{
				"task-set:2026-06-01-older",
				"task-set:2026-08-01-newer",
				"map:2026-07-01-chart",
			},
		},
	}

	cfg := &config.Config{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := &drain.Deps{
				ViewPreset: config.WorkViewPreset{
					Name:                 "fixture",
					WorkViewPresetFilter: config.WorkViewPresetFilter{Sort: tc.sort},
				},
				Kinds:        func(*drain.Deps, *config.Config) []work.Kind { return []work.Kind{maps, sets} },
				RoutineKinds: func(*drain.Deps, *config.Config) []work.Kind { return nil },
			}

			page, err := dashboard.BuildPageSnapshot(d, cfg, dashboard.PageWork, work.PaneFacts{})
			if err != nil {
				t.Fatalf("BuildPageSnapshot: %v", err)
			}
			if got := orderingRowKeys(page.Containers); !slices.Equal(got, tc.want) {
				t.Fatalf("dashboard order = %v, want %v", got, tc.want)
			}

			tables, err := workBuildStatusTables(d, cfg)
			if err != nil {
				t.Fatalf("workBuildStatusTables: %v", err)
			}
			// Row for row, not merely similar: `pop work status` is a second render
			// of the build the dashboard shows, so any divergence here is the two
			// surfaces having drifted apart on the same preset.
			if got := orderingRowKeys(tables.TaskSets.Rows); !slices.Equal(got, tc.want) {
				t.Fatalf("status table order = %v, want the dashboard's %v", got, tc.want)
			}
		})
	}
}

// orderingRowKeys renders a page as `kind:id`, the only thing this test reads
// off a container.
func orderingRowKeys(containers []work.Container) []string {
	keys := make([]string, 0, len(containers))
	for _, c := range containers {
		keys = append(keys, string(c.Kind)+":"+c.ID)
	}
	return keys
}

// orderingStubKind loads the containers the test handed it and orders its own by
// id ascending — a comparator picked so that a page falling through to it reads
// differently from a page ordered by date.
type orderingStubKind struct {
	id         work.KindID
	containers []work.Container
}

func (k orderingStubKind) ID() work.KindID                                { return k.id }
func (k orderingStubKind) Load() ([]work.Container, error)                { return k.containers, nil }
func (k orderingStubKind) Less(a, b work.Container) bool                  { return a.ID < b.ID }
func (k orderingStubKind) StatusCell(work.Container) []work.StatusSegment { return nil }
func (k orderingStubKind) Actions(work.Container) []work.Action           { return nil }
func (k orderingStubKind) StatusActions(work.Container) []work.Action     { return nil }
func (k orderingStubKind) ItemActions(work.Container, work.Item) []work.Action {
	return nil
}
func (k orderingStubKind) Perform(_ work.Container, _ *work.Item, verb work.Verb) (work.Outcome, error) {
	return work.Outcome{}, work.UnknownVerb(k.id, verb)
}
func (k orderingStubKind) Summary([]work.Container) []string { return nil }
func (k orderingStubKind) Columns() []string                 { return nil }
