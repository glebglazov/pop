package dashboard

import (
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/queuetest"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/internal/tmux/tmuxtest"
	"github.com/glebglazov/pop/tasks/drain"
	"github.com/glebglazov/pop/work"
)

// Pane work attribution end to end (ADR-0201): the pane facts a launch reads, the
// kind-side ladder over them during the snapshot build, and the cursor the
// dashboard opens on. Every test here drives the whole path — a fake tmux pane
// through BuildSeededPageSnapshot into a real model — because the seam is only
// worth anything if all three halves agree about one container's identity.

// openSeeded is the launch: read the pane the fake says the caller is in, build
// page A through it, and construct the model that would take the first paint.
func openSeeded(t *testing.T, d *drain.Deps, cfg *config.Config) QueueDashboard {
	t.Helper()
	snap, err := BuildSeededPageSnapshot(d, cfg, PageWork, LaunchPaneFacts(d.Tmux))
	if err != nil {
		t.Fatalf("BuildSeededPageSnapshot: %v", err)
	}
	return NewDashboardOn(d, cfg, snap, PageWork)
}

// inPane arranges the fake so the caller is sitting in one pane of one session.
func inPane(f *tmuxtest.Fake, session, paneID string) {
	f.CurrentPaneID = paneID
	f.CurrentSessionName = session
}

// tagPane writes the tag pop would have written when it opened that pane for a
// container.
func tagPane(f *tmuxtest.Fake, paneID string, tag tmuxmod.PaneTag, value string) {
	if f.PaneTagValues == nil {
		f.PaneTagValues = map[string]map[tmuxmod.PaneTag]string{}
	}
	if f.PaneTagValues[paneID] == nil {
		f.PaneTagValues[paneID] = map[tmuxmod.PaneTag]string{}
	}
	f.PaneTagValues[paneID][tag] = value
}

// cursorRow is the id under the cursor, which is what "landed on that row" means
// when row order is not stable across rebuilds.
func cursorRow(t *testing.T, m QueueDashboard) string {
	t.Helper()
	row, ok := m.list.Selected()
	if !ok {
		t.Fatalf("no row under the cursor of %d rows", len(m.snap.Containers))
	}
	return row.ID
}

// taskSetPaneFixture is a machine with three registered sets, all visible, and a
// fake tmux the caller places itself in. Three rows is the point: with one row a
// seeded cursor is indistinguishable from no seeding at all.
func taskSetPaneFixture(t *testing.T) (*drain.Deps, *config.Config, *queuetest.RecordingTmux, []string) {
	t.Helper()
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "2026-01-01-done-1", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	d, cfg, _, rt := dashboardLaunchFixture(t, repo, setID)
	stems := registerDoneSets(t, repo, 3)
	d.ViewPreset, _ = config.ShippedWorkViewPreset("all")
	return d, cfg, rt, stems
}

// The first rung: a pane pop opened for a Task set. All four tags name the set the
// pane is working on, so all four attribute the same row — drain, verify, fold and
// assist are activities on one container, not four kinds of pane.
func TestPaneTaggedForATaskSetSeedsItsRow(t *testing.T) {
	for _, tag := range []struct {
		name string
		tag  tmuxmod.PaneTag
	}{
		{"set", tmuxmod.TagSet},
		{"verify", tmuxmod.TagVerify},
		{"fold", tmuxmod.TagFold},
		{"assist", tmuxmod.TagAssist},
	} {
		t.Run(tag.name, func(t *testing.T) {
			d, cfg, rt, stems := taskSetPaneFixture(t)
			want := stems[len(stems)-1]
			inPane(rt.Fake, "work", "%9")
			tagPane(rt.Fake, "%9", tag.tag, want)

			m := openSeeded(t, d, cfg)

			if len(m.snap.Containers) != len(stems) {
				t.Fatalf("rows = %d, want %d — the fixture is not exercising a choice of rows", len(m.snap.Containers), len(stems))
			}
			if got := cursorRow(t, m); got != want {
				t.Fatalf("cursor on %q, want the tagged set %q", got, want)
			}
			if m.flash.Text() != "" {
				t.Fatalf("status = %q, want silence when the cursor landed", m.flash.Text())
			}
		})
	}
}

// The ordinary case: an unrelated shell, and a pane tagged for a kind this page
// does not list. Both are silent — a "nothing found" line on every launch trains
// the human to ignore the status line (ADR-0201 decision 6), and a Routine
// resolves to a row on the other page, which the launch does not follow
// (decision 5).
func TestUnattributedPaneSeedsNothingAndSaysNothing(t *testing.T) {
	for _, tc := range []struct {
		name  string
		tag   tmuxmod.PaneTag
		value string
	}{
		{name: "an unrelated shell"},
		{name: "a routine pane", tag: tmuxmod.TagRoutine, value: "nightly-audit"},
		{name: "a tag naming a set that no longer exists", tag: tmuxmod.TagSet, value: "2019-01-01-deleted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, cfg, rt, _ := taskSetPaneFixture(t)
			inPane(rt.Fake, "editor", "%4")
			if tc.value != "" {
				tagPane(rt.Fake, "%4", tc.tag, tc.value)
			}

			m := openSeeded(t, d, cfg)

			if m.snap.Attribution != nil {
				t.Fatalf("attribution = %+v, want none", *m.snap.Attribution)
			}
			if m.ListCursor() != 0 {
				t.Fatalf("cursor = %d, want the untouched first row", m.ListCursor())
			}
			if m.flash.Text() != "" {
				t.Fatalf("status = %q, want silence", m.flash.Text())
			}
		})
	}
}

// The loud half of decision 6. The set is attributed, but the active preset hides
// its row: a cursor resting at row one with no explanation is indistinguishable
// from a broken feature, so the container is named along with what is hiding it —
// and the preset is left exactly as the human set it.
func TestHiddenAttributedSetIsNamedAndThePresetIsNotWidened(t *testing.T) {
	d, cfg, rt, stems := taskSetPaneFixture(t)
	hidden := stems[len(stems)-1]
	inPane(rt.Fake, "work", "%9")
	tagPane(rt.Fake, "%9", tmuxmod.TagSet, hidden)
	d.ViewPreset = config.WorkViewPreset{
		Name:  "_hide-done",
		Label: "in flight",
		Hide:  &config.WorkViewPresetFilter{Status: []string{"done"}},
	}

	m := openSeeded(t, d, cfg)

	if len(m.snap.Containers) != 0 {
		t.Fatalf("rows = %d, want none — the launch must not widen the preset to reveal the row", len(m.snap.Containers))
	}
	if m.snap.Attribution == nil {
		t.Fatal("attribution = none: a hidden row must still be attributed, or nothing can be said about it")
	}
	if !strings.Contains(m.flash.Text(), hidden) || !strings.Contains(m.flash.Text(), "in flight") {
		t.Fatalf("status = %q, want it to name %q and the view hiding it", m.flash.Text(), hidden)
	}
	if d.ViewPreset.Name != "_hide-done" {
		t.Fatalf("preset = %q, want the human's own choice untouched", d.ViewPreset.Name)
	}
}

// Decision 4: one shot. The human opens on the seeded row, deliberately moves
// somewhere else, and the next rebuild — the poll, or a preset change — must leave
// them there. A target that outlives the human's own navigation is a cursor that
// fights them.
func TestSeedingIsNotRetriedOnALaterRebuild(t *testing.T) {
	d, cfg, rt, stems := taskSetPaneFixture(t)
	seeded := stems[len(stems)-1]
	inPane(rt.Fake, "work", "%9")
	tagPane(rt.Fake, "%9", tmuxmod.TagSet, seeded)

	m := openSeeded(t, d, cfg)
	if got := cursorRow(t, m); got != seeded {
		t.Fatalf("cursor opened on %q, want %q", got, seeded)
	}

	// Wherever the human went, it is not the seeded row.
	moved := ""
	for _, row := range m.snap.Containers {
		if row.ID != seeded {
			moved = row.ID
			break
		}
	}
	if !m.list.SetCursorToKey(rowKeyFor(t, m, moved)) {
		t.Fatalf("could not move the cursor to %q", moved)
	}

	rebuilt, err := BuildPageSnapshot(d, cfg, PageWork)
	if err != nil {
		t.Fatalf("BuildPageSnapshot: %v", err)
	}
	if rebuilt.Attribution != nil {
		t.Fatalf("rebuild attributed %+v — only a launch may attribute a pane", *rebuilt.Attribution)
	}
	updated, _ := m.Update(dashboardRowsMsg{page: PageWork, snap: rebuilt})
	after := updated.(QueueDashboard)
	if got := cursorRow(t, after); got != moved {
		t.Fatalf("cursor after a rebuild = %q, want the row the human moved to (%q)", got, moved)
	}
}

// rowKeyFor is the cursor key of the row with this id, which is the only valid
// handle on a row: order is not stable across rebuilds.
func rowKeyFor(t *testing.T, m QueueDashboard, id string) string {
	t.Helper()
	for _, row := range m.snap.Containers {
		if row.ID == id {
			return row.CursorKey
		}
	}
	t.Fatalf("no row %q among %d", id, len(m.snap.Containers))
	return ""
}

// Outside tmux there is no pane, and a dashboard that cannot tell where it is
// seeds nothing rather than guessing.
func TestLaunchPaneFactsWithoutAPaneIsEmpty(t *testing.T) {
	if got := LaunchPaneFacts(nil); !got.Empty() {
		t.Fatalf("LaunchPaneFacts(nil) = %+v, want zero facts", got)
	}
	if got := LaunchPaneFacts(&tmuxtest.Fake{}); !got.Empty() {
		t.Fatalf("LaunchPaneFacts(no current pane) = %+v, want zero facts", got)
	}
	if att := work.AttributePane(nil, work.PaneFacts{}); att != nil {
		t.Fatalf("AttributePane with no facts = %+v, want none", *att)
	}
}
