package dashboard

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/queuetest"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/internal/tmux/tmuxtest"
	"github.com/glebglazov/pop/tasks/drain"
	"github.com/glebglazov/pop/ui"
	"github.com/glebglazov/pop/work"
)

// Pane work attribution end to end (ADR-0209): the pane facts a launch reads, the
// kind-side ladder over them during the snapshot build, and the rows the dashboard
// lifts to the top of its list. Every test here drives the whole path — a fake tmux
// pane through BuildPageSnapshot into a real model — because the seam is only
// worth anything if all three halves agree about one container's identity.

// openFromPane is the launch: read the pane the fake says the caller is in, build
// page A through it, and construct the model that would take the first paint.
func openFromPane(t *testing.T, d *drain.Deps, cfg *config.Config) QueueDashboard {
	t.Helper()
	snap, err := BuildPageSnapshot(d, cfg, PageWork, LaunchPaneFacts(d.Tmux))
	if err != nil {
		t.Fatalf("BuildPageSnapshot: %v", err)
	}
	return NewDashboardOn(d, cfg, snap, PageWork)
}

// rebuild is one poll: the model's own reload command and the message it produces,
// which is the only path a running dashboard ever takes to new rows.
func rebuild(t *testing.T, m QueueDashboard) QueueDashboard {
	t.Helper()
	msg, ok := m.reload()().(dashboardRowsMsg)
	if !ok {
		t.Fatal("reload did not produce a rows message")
	}
	if msg.err != nil {
		t.Fatalf("reload: %v", msg.err)
	}
	updated, _ := m.Update(msg)
	return updated.(QueueDashboard)
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

// pinnedBlock is the ids of the marked rows at the head of the list, and it
// insists the block really is a block: a marked row further down would mean the
// pin reordered nothing, and a row marked twice would mean it was copied rather
// than moved.
func pinnedBlock(t *testing.T, m QueueDashboard) []string {
	t.Helper()
	var ids []string
	for i, row := range m.snap.Containers {
		if !row.Pinned {
			continue
		}
		if i != len(ids) {
			t.Fatalf("row %d (%s) is marked but sits below %d unmarked rows — the pinned rows are not a block at the top", i, row.ID, i-len(ids))
		}
		ids = append(ids, row.ID)
	}
	return ids
}

// rowIDs is the list in render order, which is what "where the rows sit" means.
func rowIDs(m QueueDashboard) []string {
	var ids []string
	for _, row := range m.snap.Containers {
		ids = append(ids, row.ID)
	}
	return ids
}

// unpinnedOrder is the order this page has with no pane behind it: the baseline
// every pinning launch must leave untouched beneath its block.
func unpinnedOrder(t *testing.T, d *drain.Deps, cfg *config.Config) []string {
	t.Helper()
	snap, err := BuildPageSnapshot(d, cfg, PageWork, work.PaneFacts{})
	if err != nil {
		t.Fatalf("BuildPageSnapshot: %v", err)
	}
	var ids []string
	for _, row := range snap.Containers {
		if row.Pinned {
			t.Fatalf("a build with no pane behind it pinned %s", row.ID)
		}
		ids = append(ids, row.ID)
	}
	return ids
}

// wantPinnedFirst is the whole list a launch should render: the pinned ids in
// attribution order, then the baseline with those ids removed — pinned rows are
// moved, not copied, and nothing else shifts.
func wantPinnedFirst(baseline []string, pinned ...string) []string {
	want := append([]string{}, pinned...)
	for _, id := range baseline {
		if !slices.Contains(pinned, id) {
			want = append(want, id)
		}
	}
	return want
}

// taskSetPaneFixture is a machine with three registered sets, all visible, and a
// fake tmux the caller places itself in. Three rows is the point: with one row a
// pinned row is indistinguishable from no pinning at all.
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
// pane is working on, so all four pin the same row — drain, verify, fold and assist
// are activities on one container, not four kinds of pane.
func TestPaneTaggedForATaskSetPinsItsRowFirst(t *testing.T) {
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
			baseline := unpinnedOrder(t, d, cfg)
			if baseline[0] == want {
				t.Fatalf("the tagged set already sorts first in %v — the fixture is not exercising a move", baseline)
			}
			inPane(rt.Fake, "work", "%9")
			tagPane(rt.Fake, "%9", tag.tag, want)

			m := openFromPane(t, d, cfg)

			if got := pinnedBlock(t, m); !slices.Equal(got, []string{want}) {
				t.Fatalf("pinned %v, want only the tagged set %q", got, want)
			}
			if got := rowIDs(m); !slices.Equal(got, wantPinnedFirst(baseline, want)) {
				t.Fatalf("rows = %v, want %v — the tagged set first, the rest in their own order", got, wantPinnedFirst(baseline, want))
			}
			if m.flash.Text() != "" {
				t.Fatalf("status = %q, want silence: the pin says it (ADR-0209 decision 8)", m.flash.Text())
			}
		})
	}
}

// A pinned row is marked wherever the cursor is: `▸` on its own, `█▸` when the
// cursor happens to be on it. The prefix column stays two cells wide either way,
// so a launch that pins and one that does not line their columns up identically.
func TestAPinnedRowIsMarkedInTheTwoCellPrefixColumn(t *testing.T) {
	d, cfg, rt, stems := taskSetPaneFixture(t)
	pinned := stems[len(stems)-1]
	inPane(rt.Fake, "work", "%9")
	tagPane(rt.Fake, "%9", tmuxmod.TagSet, pinned)

	m := openFromPane(t, d, cfg)
	m.list.Resize(len(m.snap.Containers))
	rows := m.list.VisibleRows()

	if got := ui.StripANSI(rows[0]); !strings.HasPrefix(got, "█▸") {
		t.Fatalf("cursored pinned row = %q, want the cursor block and the pin mark", got)
	}
	m.list.SetCursor(1)
	rows = m.list.VisibleRows()
	if got := ui.StripANSI(rows[0]); !strings.HasPrefix(got, " ▸") {
		t.Fatalf("uncursored pinned row = %q, want the pin mark alone", got)
	}
	if got := ui.StripANSI(rows[1]); !strings.HasPrefix(got, "█ ") {
		t.Fatalf("cursored unpinned row = %q, want the cursor block and a blank pin cell", got)
	}
}

// The ordinary case: an unrelated shell, and a pane tagged for a kind this page
// does not list. Both leave the dashboard exactly as it always looks — no pin, no
// line, no moved cursor. A "nothing found" line on every launch trains the human to
// ignore the status line, and a Routine resolves to a row on the other page, which
// page A does not follow.
func TestUnattributedPaneChangesNothingAndSaysNothing(t *testing.T) {
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
			baseline := unpinnedOrder(t, d, cfg)
			inPane(rt.Fake, "editor", "%4")
			if tc.value != "" {
				tagPane(rt.Fake, "%4", tc.tag, tc.value)
			}

			m := openFromPane(t, d, cfg)

			if m.snap.Attribution != nil {
				t.Fatalf("attribution = %+v, want none", *m.snap.Attribution)
			}
			if got := pinnedBlock(t, m); len(got) != 0 {
				t.Fatalf("pinned %v, want nothing", got)
			}
			if got := rowIDs(m); !slices.Equal(got, baseline) {
				t.Fatalf("rows = %v, want the untouched order %v", got, baseline)
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

// Decision 7: the preset is absolute. The set is attributed, but the active preset
// hides its row, so there is nothing to pin — and nothing to say either, because a
// dashboard that looks exactly as it always does is not an anomaly needing an
// explanation (decision 8).
func TestHiddenAttributedSetIsNotPinnedAndTheViewIsNotWidened(t *testing.T) {
	d, cfg, rt, stems := taskSetPaneFixture(t)
	hidden := stems[len(stems)-1]
	inPane(rt.Fake, "work", "%9")
	tagPane(rt.Fake, "%9", tmuxmod.TagSet, hidden)
	d.ViewPreset = config.WorkViewPreset{
		Name:  "_hide-done",
		Label: "in flight",
		Hide:  &config.WorkViewPresetFilter{Status: []string{"done"}},
	}

	m := openFromPane(t, d, cfg)

	if len(m.snap.Containers) != 0 {
		t.Fatalf("rows = %v, want none — the launch must not widen the preset to reveal the row", rowIDs(m))
	}
	if m.snap.Attribution == nil {
		t.Fatal("attribution = none: a hidden row is still attributed, it simply has no row to pin")
	}
	if m.flash.Text() != "" {
		t.Fatalf("status = %q, want silence about a row nobody can see", m.flash.Text())
	}
	if d.ViewPreset.Name != "_hide-done" {
		t.Fatalf("preset = %q, want the human's own choice untouched", d.ViewPreset.Name)
	}
}

// The live filter is the preset's twin: a query that excludes the pinned row drops
// it like any other row, silently, and the rows it does match keep their order.
func TestAFilterQueryThatExcludesThePinnedRowDropsItSilently(t *testing.T) {
	d, cfg, rt, stems := taskSetPaneFixture(t)
	pinned := stems[len(stems)-1]
	inPane(rt.Fake, "work", "%9")
	tagPane(rt.Fake, "%9", tmuxmod.TagSet, pinned)

	m := openFromPane(t, d, cfg)
	if got := pinnedBlock(t, m); !slices.Equal(got, []string{pinned}) {
		t.Fatalf("pinned %v before filtering, want %v", got, []string{pinned})
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = updated.(QueueDashboard)
	for _, ch := range stems[0] {
		updated, _ = m.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)})
		m = updated.(QueueDashboard)
	}

	if got := rowIDs(m); !slices.Equal(got, []string{stems[0]}) {
		t.Fatalf("rows under the /%s filter = %v, want only %q", stems[0], got, stems[0])
	}
	if m.flash.Text() != "" {
		t.Fatalf("status = %q, want silence when the filter hid the pinned row", m.flash.Text())
	}
}

// Decision 1: the pin is position, never navigation. The cursor opens on the first
// row the way it always does, and wherever the human moves it, a rebuild leaves
// them there — rows may rearrange under someone who is reading, but their selection
// does not.
func TestAttributionNeverMovesTheCursor(t *testing.T) {
	d, cfg, rt, stems := taskSetPaneFixture(t)
	pinned := stems[len(stems)-1]
	inPane(rt.Fake, "work", "%9")
	tagPane(rt.Fake, "%9", tmuxmod.TagSet, pinned)

	m := openFromPane(t, d, cfg)
	if m.ListCursor() != 0 {
		t.Fatalf("cursor = %d at launch, want the first row: attribution moves rows, not the cursor", m.ListCursor())
	}

	// Wherever the human went, it is not the pinned row.
	moved := ""
	for _, row := range m.snap.Containers {
		if row.ID != pinned {
			moved = row.ID
			break
		}
	}
	if !m.list.SetCursorToKey(rowKeyFor(t, m, moved)) {
		t.Fatalf("could not move the cursor to %q", moved)
	}

	after := rebuild(t, m)
	if got := cursorRow(t, after); got != moved {
		t.Fatalf("cursor after a rebuild = %q, want the row the human moved to (%q)", got, moved)
	}
}

// cursorRow is the id under the cursor, which is what "the cursor is still there"
// means when row order is not stable across rebuilds.
func cursorRow(t *testing.T, m QueueDashboard) string {
	t.Helper()
	row, ok := m.list.Selected()
	if !ok {
		t.Fatalf("no row under the cursor of %d rows", len(m.snap.Containers))
	}
	return row.ID
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

// Outside tmux there is no pane, and a dashboard that cannot tell where it is pins
// nothing rather than guessing.
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

// The pin is not a sort key: it lifts rows off the top of whatever order the active
// preset produced and changes nothing underneath. Every sort a preset can declare is
// checked against its own unpinned build, so a pin can never be mistaken for a
// re-sort.
func TestPinningLeavesEveryPresetSortIntactBeneathTheBlock(t *testing.T) {
	for _, sort := range []string{"", config.PresetSortCreatedDesc, config.PresetSortCreatedAsc} {
		t.Run("sort="+sort, func(t *testing.T) {
			d, cfg, rt, stems := taskSetPaneFixture(t)
			d.ViewPreset.Sort = sort
			pinned := stems[len(stems)-1]
			baseline := unpinnedOrder(t, d, cfg)
			inPane(rt.Fake, "work", "%9")
			tagPane(rt.Fake, "%9", tmuxmod.TagSet, pinned)

			m := openFromPane(t, d, cfg)

			if got, want := rowIDs(m), wantPinnedFirst(baseline, pinned); !slices.Equal(got, want) {
				t.Fatalf("rows = %v, want %v — the rows beneath the block keep the sort's own order", got, want)
			}
		})
	}
}
