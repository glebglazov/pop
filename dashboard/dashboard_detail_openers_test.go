package dashboard

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/routine"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/drain"
	"github.com/glebglazov/pop/ui"
	"github.com/glebglazov/pop/work"
)

// ADR-0261 decision 1: inside a detail view the Status, Copy and Mute openers
// act on the container the detail is open over, and decision 4 brings the flat
// `I` down with them. The claim is "the same menu one level down", so these
// tests drive the two levels against each other over one container rather than
// asserting a shape at either level alone.

// detailOpenerRow is a task set whose kind offers all three of the things the
// openers ask for.
func detailOpenerRow() DashboardRow {
	return DashboardRow{
		Project: "pop", CursorKey: "pop\x00my-set", RawStatus: tasks.StatusReady,
		ID: "my-set", DefPath: "/repo/tasks", StatePath: "/repo/state.json",
	}
}

func detailOpenerDashboard(row DashboardRow) QueueDashboard {
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{row}})
	m.width, m.height = 120, 24
	return m
}

// containerMenuEntries is the open container menu's entries as "<key> <label>",
// whichever of the three it is — the only thing a comparison between two levels
// needs to be about.
func containerMenuEntries(m QueueDashboard) []string {
	if m.menu == nil {
		return nil
	}
	var out []string
	switch {
	case m.menu.status != nil:
		for _, action := range m.menu.status.list.Items() {
			out = append(out, action.Key+" "+action.Label)
		}
	case m.menu.copy != nil:
		for _, action := range m.menu.copy.list.Items() {
			out = append(out, action.Key+" "+action.Label)
		}
	case m.menu.mute != nil:
		for _, entry := range m.menu.mute.list.Items() {
			out = append(out, entry.key+" "+entry.label)
		}
	}
	return out
}

func TestDetailOpenersOpenTheSameMenusTheRowDoes(t *testing.T) {
	row := detailOpenerRow()
	cases := []struct {
		key   string
		model func(*testing.T) QueueDashboard
	}{
		{"s", func(*testing.T) QueueDashboard { return detailOpenerDashboard(row) }},
		{"y", func(*testing.T) QueueDashboard { return detailOpenerDashboard(row) }},
		// The Mute menu's windows are derived from today, so its two levels are
		// compared over the pinned clock the mute tests already build.
		{"m", func(t *testing.T) QueueDashboard { m, _ := muteDashboard(t, row); return m }},
	}
	for _, c := range cases {
		t.Run(c.key, func(t *testing.T) {
			fromRow := pressKeys(t, c.model(t), c.key)
			want := containerMenuEntries(fromRow)
			if len(want) == 0 {
				t.Fatalf("%s over the row opened no menu to compare against", c.key)
			}

			fromDetail := pressKeys(t, c.model(t), "l", c.key)
			if fromDetail.detail == nil {
				t.Fatalf("%s closed the detail it was pressed in", c.key)
			}
			if got := containerMenuEntries(fromDetail); !reflect.DeepEqual(got, want) {
				t.Fatalf("%s in the detail offered %v, want the row's %v", c.key, got, want)
			}
			if fromDetail.menu.row.ID != row.ID {
				t.Fatalf("%s opened over %q, want the detail's own container", c.key, fromDetail.menu.row.ID)
			}

			// esc unwinds one level: the menu goes and the detail is still standing.
			closed, _ := fromDetail.update(tea.KeyPressMsg{Code: tea.KeyEscape})
			after := closed.(QueueDashboard)
			if after.menu != nil {
				t.Fatalf("esc did not close the %s menu", c.key)
			}
			if after.detail == nil {
				t.Fatalf("esc out of the %s menu dismissed the detail with it", c.key)
			}
		})
	}
}

// A container whose kind offers none of the three answers with the same refusal
// from either level — on the hint line the operator is actually looking at
// (ADR-0236 decision 7).
func TestDetailOpenerRefusalsReadTheSameFromBothLevels(t *testing.T) {
	refusals := map[string]string{
		"s": "a Task has no status to write",
		"y": "a Task has nothing to copy",
		"m": "a Task cannot be muted",
	}
	for key, want := range refusals {
		t.Run(key, func(t *testing.T) {
			fromRow := pressKeys(t, genericDetailDashboard(&itemVerbKind{}), key)
			if fromRow.flash.Text() != want {
				t.Fatalf("%s over the row flashed %q, want %q", key, fromRow.flash.Text(), want)
			}

			fromDetail := pressKeys(t, genericDetailDashboard(&itemVerbKind{}), "l", key)
			if fromDetail.menu != nil {
				t.Fatalf("%s opened a menu the kind has nothing to fill", key)
			}
			if fromDetail.detail.flash.Text() != want {
				t.Fatalf("%s in the detail flashed %q, want %q", key, fromDetail.detail.flash.Text(), want)
			}
			if fromDetail.flash.Text() != "" {
				t.Fatalf("%s reported on the row list behind the detail: %q", key, fromDetail.flash.Text())
			}
		})
	}
}

// `I` in a detail runs the container's own flat verb — the same verb, through
// the same dispatch, that the row one level up runs (ADR-0261 decision 4).
func TestDetailFlatVerbRunsTheContainersOwn(t *testing.T) {
	spied := func(t *testing.T) (QueueDashboard, *verbSpyKind) {
		spy := &verbSpyKind{Kind: routine.NewKind(nil)}
		kinds := func(*drain.Deps, *config.Config) []work.Kind { return []work.Kind{spy} }
		return openPage(t, &drain.Deps{Kinds: kinds, RoutineKinds: kinds}, PageRoutines), spy
	}

	fromRow, rowSpy := spied(t)
	pressKeys(t, fromRow, "I")
	if len(rowSpy.performed) == 0 {
		t.Fatal("I over the row performed nothing to compare against")
	}

	fromDetail, detailSpy := spied(t)
	got := pressKeys(t, fromDetail, "l", "I")
	if fmt.Sprint(detailSpy.performed) != fmt.Sprint(rowSpy.performed) {
		t.Fatalf("I in the detail performed %v, want the row's %v", detailSpy.performed, rowSpy.performed)
	}
	if got.detail == nil {
		t.Fatal("an in-place flat verb must leave the detail standing")
	}
	if got.detail.flash.Text() == "" {
		t.Fatalf("the verb's report landed off the detail: row list has %q", got.flash.Text())
	}
}

// A menu opened from a detail is the detail's own reserved Block, at the same
// screen position and taking the same height off the body as the item menu that
// renders there (ADR-0224 decision 4).
func TestDetailContainerMenuIsTheDetailsOwnBlock(t *testing.T) {
	row := detailOpenerRow()
	row.Items = make([]work.Item, 20)
	for i := range row.Items {
		row.Items[i] = work.Item{ID: fmt.Sprintf("item-%02d", i), Title: "Item", Type: "AFK", Status: "open"}
	}
	m := detailOpenerDashboard(row)
	m.height = 18
	m = pressKeys(t, m, "l")

	// The Frame sizes the list as it renders, so the body it is being compared
	// against has to have been drawn once.
	m.View()
	beforeRows := len(m.detail.list.VisibleRows())
	m = pressKeys(t, m, "y")
	if m.menu == nil || m.menu.copy == nil {
		t.Fatal("y in the detail did not open the container's copy menu")
	}

	view := m.View().Content
	block := dashboardMenuLines(m.menu, m.width, m.liveCache())
	lines := strings.Split(ui.StripANSI(view), "\n")
	ruleAt := dashboardTestLineIndex(lines, "copy · "+row.ID)
	if ruleAt < 0 {
		t.Fatalf("the menu block does not name its container:\n%s", ui.StripANSI(view))
	}
	if want := len(lines) - 1 - len(block); ruleAt != want {
		t.Fatalf("menu rule at line %d, want %d so its block ends above the hint line:\n%s", ruleAt, want, ui.StripANSI(view))
	}
	if got := beforeRows - len(m.detail.list.VisibleRows()); got != len(block) {
		t.Fatalf("the item list lost %d rows, want the block's %d lines", got, len(block))
	}
}

// A Selection is a row-list mark, and ADR-0254 decision 5 keeps item-level bulk
// out: a menu opened from a detail acts on that one container however many rows
// are marked behind it.
func TestDetailMenuStaysSingularUnderASelection(t *testing.T) {
	row := detailOpenerRow()
	m := pressKeys(t, detailOpenerDashboard(row), "tab")
	if !m.selection.Active() {
		t.Fatal("tab did not mark the row")
	}
	m.detail = m.openDetailView(row)

	m = pressKeys(t, m, "y")
	if m.menu == nil || m.menu.copy == nil {
		t.Fatal("y in the detail did not open the container's copy menu")
	}
	if m.menu.plural || len(m.menu.targets) != 0 {
		t.Fatalf("the detail's menu went plural: plural=%v targets=%v", m.menu.plural, m.menu.targets)
	}
	want := containerMenuEntries(pressKeys(t, detailOpenerDashboard(row), "l", "y"))
	if got := containerMenuEntries(m); !reflect.DeepEqual(got, want) {
		t.Fatalf("menu under a Selection offered %v, want the singular %v", got, want)
	}
}

// The detail advertises the four openers where the row list advertises them: on
// the hint line and in the help overlay.
func TestDetailKeyboardNamesTheFourOpeners(t *testing.T) {
	m := pressKeys(t, detailOpenerDashboard(detailOpenerRow()), "l")

	hint := detailHints(m.detail)
	for _, want := range []string{"r run ▸", "s status ▸", "y copy ▸", "m mute ▸"} {
		if !strings.Contains(hint, want) {
			t.Fatalf("detail hint = %q, want it to name %q", hint, want)
		}
	}

	described := map[string]string{}
	for _, entry := range m.helpEntries() {
		described[entry.Key] = entry.Desc
	}
	for key, want := range map[string]string{
		"r": "item run menu",
		"s": "status menu",
		"y": "copy menu",
		"m": "mute menu",
	} {
		if described[key] != want {
			t.Fatalf("help describes %q as %q, want %q", key, described[key], want)
		}
	}
	if described["I"] == "" {
		t.Fatalf("help does not name the container's flat verb: %v", described)
	}
}

// A verb chosen from a detail-opened menu reports on the detail's own hint line.
// The row list behind it is not on screen, so a confirmation left there is a
// verb that appears to have done nothing.
func TestDetailMenuVerbReportsOnTheDetail(t *testing.T) {
	row := detailOpenerRow()
	m := detailOpenerDashboard(row)
	var captured string
	m.copyFunc = func(s string) error { captured = s; return nil }

	got := pressKeys(t, m, "l", "y", "y")
	if got.menu != nil {
		t.Fatal("choosing an entry must close the menu")
	}
	if captured != "/repo/tasks/my-set" {
		t.Fatalf("y y from the detail copied %q, want the set's own definition folder", captured)
	}
	if got.detail.flash.Text() == "" || got.flash.Text() != "" {
		t.Fatalf("report landed on the wrong surface: detail=%q row list=%q", got.detail.flash.Text(), got.flash.Text())
	}
}
