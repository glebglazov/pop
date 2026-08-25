package dashboard

import (
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/glebglazov/pop/ui"
)

// Where the actions are is a fact about the surface, not about which row is
// cursored or how many are marked (ADR-0224 decision 4): every menu on the table
// view is a Frame block immediately above the hint line, so these tests are
// written as line arithmetic over the rendered pane rather than as "near the
// cursor".

var menuTestRowID = regexp.MustCompile(`set-\d\d`)

// blockLines is the menu's reserved height — the rule plus one line per item.
func blockLines(t *testing.T, m QueueDashboard) int {
	t.Helper()
	n := len(dashboardMenuLines(m.menu, m.width, m.liveCache()))
	if n == 0 {
		t.Fatal("no menu block to measure")
	}
	return n
}

// menuRuleLine is the screen row the menu's top rule lands on.
func menuRuleLine(t *testing.T, view string) int {
	t.Helper()
	lines := strings.Split(ui.StripANSI(view), "\n")
	idx := dashboardTestLineIndex(lines, "actions · ")
	if idx < 0 {
		t.Fatalf("no menu rule on screen:\n%s", ui.StripANSI(view))
	}
	return idx
}

// visibleRowIDs is the set ids the table shows, in screen order. The menu's own
// rule names its target row, so the rule is skipped: a caption is not a row.
func visibleRowIDs(view string) []string {
	var ids []string
	for _, line := range strings.Split(ui.StripANSI(view), "\n") {
		if strings.HasPrefix(strings.TrimLeft(line, " "), "─") {
			continue
		}
		if id := menuTestRowID.FindString(line); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

// cursoredRowID is the id of the table row carrying the █ block, empty when no
// row does.
func cursoredRowID(view string) string {
	for _, line := range strings.Split(ui.StripANSI(view), "\n") {
		if strings.HasPrefix(line, "█") {
			return menuTestRowID.FindString(line)
		}
	}
	return ""
}

// manyRows is a page long enough that the cursor can sit above and below the
// scroll window, which is where the old placement rule used to flip the menu.
func manyRows(t *testing.T, height int) QueueDashboard {
	t.Helper()
	ids := make([]string, 20)
	for i := range ids {
		ids[i] = fmtSetID(i)
	}
	m, _ := setDashboard(t, ids...)
	m.height = height
	m.resizeMainList()
	return m
}

func fmtSetID(i int) string {
	return "set-" + string(rune('0'+i/10)) + string(rune('0'+i%10))
}

// The menu's rows are the same rows whatever the cursor is on, and they are the
// last rows of the pane before the hint line — the ones the preset menu occupies.
func TestActionMenuIsBottomChromeAtOneFixedPosition(t *testing.T) {
	ruleAt := func(cursor int) (int, int, int) {
		m := manyRows(t, 24)
		m.list.SetCursor(cursor)
		m = bulkPress(t, m, selKeyRune('a'))
		if m.menu == nil {
			t.Fatal("`a` did not open the action menu")
		}
		view := m.View().Content
		lines := strings.Split(view, "\n")
		if len(lines) != m.height {
			t.Fatalf("view = %d lines, want the pane's %d:\n%s", len(lines), m.height, ui.StripANSI(view))
		}
		return menuRuleLine(t, view), blockLines(t, m), len(lines)
	}

	topRule, topBlock, paneLines := ruleAt(0)
	botRule, botBlock, _ := ruleAt(19)
	if topRule != botRule {
		t.Fatalf("menu rule at line %d with the cursor at the top and %d at the bottom — the position must not move", topRule, botRule)
	}
	// Rule, then one line per item, then the hint line: the block ends on the row
	// above the bottom line whatever it holds.
	if want := paneLines - 1 - topBlock; topRule != want {
		t.Fatalf("menu rule at line %d, want %d — the block must end just above the hint line", topRule, want)
	}
	if topBlock != botBlock {
		t.Fatalf("block height %d vs %d for the same menu", topBlock, botBlock)
	}

	// The preset menu is the mechanism this moved onto, so it ends on the same row.
	m := manyRows(t, 24)
	m = bulkPress(t, m, selKeyRune('f'))
	if m.filter == nil {
		t.Fatal("`f` did not open the preset menu")
	}
	filterView := m.View().Content
	filterLines := strings.Split(filterView, "\n")
	filterAt := dashboardTestLineIndex(filterLines, "filters")
	if filterAt < 0 {
		t.Fatalf("no preset menu on screen:\n%s", ui.StripANSI(filterView))
	}
	if want := len(filterLines) - 1 - len(m.dashboardFilterMenuLines()); filterAt != want {
		t.Fatalf("preset menu opens at line %d, want %d — both blocks end above the hint line", filterAt, want)
	}
}

// The plural menu is the same object at the same rows: the count on its rule is
// the only difference the human sees.
func TestPluralActionMenuRendersAtTheSamePosition(t *testing.T) {
	// The two menus hold different verb counts — the plural one lists the
	// intersection — so what is identical is the row the block ends on, one above
	// the hint line, not the row it starts on.
	lastBlockRow := func(m QueueDashboard) (int, int) {
		view := m.View().Content
		lines := strings.Split(view, "\n")
		return menuRuleLine(t, view) + blockLines(t, m), len(lines) - 1
	}

	singular := manyRows(t, 24)
	singular.list.SetCursor(0)
	singular = bulkPress(t, singular, selKeyRune('a'))
	singularEnd, singularBottom := lastBlockRow(singular)

	plural := manyRows(t, 24)
	plural = bulkPress(t, plural, selKeyTab())
	plural = bulkPress(t, plural, selKeyTab())
	plural = bulkPress(t, plural, selKeyRune('a'))
	if plural.menu == nil || !plural.menu.plural {
		t.Fatal("`a` did not open the plural action menu")
	}
	pluralView := plural.View().Content
	pluralEnd, pluralBottom := lastBlockRow(plural)
	if singularEnd != singularBottom || pluralEnd != pluralBottom {
		t.Fatalf("blocks end at %d and %d, want the row above the hint line (%d, %d)", singularEnd, pluralEnd, singularBottom, pluralBottom)
	}
	if !strings.Contains(ui.StripANSI(pluralView), "actions · 2 selected") {
		t.Fatalf("plural rule does not count its targets:\n%s", ui.StripANSI(pluralView))
	}
}

// Opening the menu takes exactly its own height out of the list body, and the
// list re-clamps to keep the cursor visible, so the rows that go are the ones at
// the top. Closing it brings them back.
func TestActionMenuShrinksTheListBodyByItsHeight(t *testing.T) {
	m := manyRows(t, 24)
	m.list.SetCursor(19)
	before := visibleRowIDs(m.View().Content)

	m = bulkPress(t, m, selKeyRune('a'))
	block := blockLines(t, m)
	during := visibleRowIDs(m.View().Content)

	if got, want := len(before)-len(during), block; got != want {
		t.Fatalf("the body lost %d rows, want exactly the menu's %d lines:\n%v\n%v", got, want, before, during)
	}
	if during[len(during)-1] != "set-19" {
		t.Fatalf("last visible row = %q, want the cursored set-19 to stay on screen: %v", during[len(during)-1], during)
	}
	if during[0] <= before[0] {
		t.Fatalf("first visible row went from %q to %q, want the rows to leave from the top", before[0], during[0])
	}

	m = bulkPress(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.menu != nil {
		t.Fatal("esc did not close the menu")
	}
	if got := visibleRowIDs(m.View().Content); len(got) != len(before) || got[0] != before[0] {
		t.Fatalf("after close the body shows %v, want %v back", got, before)
	}
}

// The list under an open menu is the live list, not a picture of it: uppercase
// movement reaches the row list while lowercase movement stays with the menu.
func TestListStaysLiveUnderAnOpenMenu(t *testing.T) {
	m := manyRows(t, 24)
	m.list.SetCursor(0)
	m = bulkPress(t, m, selKeyRune('a'))
	if got := cursoredRowID(m.View().Content); got != "set-00" {
		t.Fatalf("cursored row = %q, want set-00 painted under the open menu", got)
	}

	m = bulkPress(t, m, tea.KeyPressMsg{Code: 'J', Text: "J"})
	if m.menu == nil {
		t.Fatal("moving the list closed the menu")
	}
	if got := cursoredRowID(m.View().Content); got != "set-01" {
		t.Fatalf("cursored row = %q after moving down, want set-01 — the body is not live", got)
	}
}

// A menu with no room left is the block mechanism's own problem, and it answers
// with its indicator rather than by drawing past the pane.
func TestActionMenuTallerThanThePaneIsClipped(t *testing.T) {
	m := manyRows(t, 10)
	m = bulkPress(t, m, selKeyRune('a'))
	if m.menu == nil {
		t.Fatal("`a` did not open the action menu")
	}
	view := m.View().Content
	lines := strings.Split(view, "\n")
	if len(lines) > m.height {
		t.Fatalf("view = %d lines on a pane of %d:\n%s", len(lines), m.height, ui.StripANSI(view))
	}
	if !strings.Contains(ui.StripANSI(view), "clipped to fit the pane") {
		t.Fatalf("an over-tall menu was cut without saying so:\n%s", ui.StripANSI(view))
	}
	// The hint line still owns the last row: the clip lands above it.
	if bottom := ui.StripANSI(lines[len(lines)-1]); !strings.Contains(bottom, "esc close") {
		t.Fatalf("bottom line = %q, want the menu's hints", bottom)
	}
}

// The singular menu names the container it will act on, and the menus that open
// beside or under it name the same target under their own noun.
func TestMenuRuleNamesItsTarget(t *testing.T) {
	m, _ := setDashboard(t, "set-a", "set-b")
	m = bulkPress(t, m, selKeyRune('a'))
	view := ui.StripANSI(m.View().Content)
	if !strings.Contains(view, "actions · set-a") {
		t.Fatalf("singular rule does not name the cursored container:\n%s", view)
	}

	// The Status and the Mute menus both open from the row list, and both render
	// through the one block path, which is what this pins.
	for _, tc := range []struct {
		name, key, want string
	}{
		{name: "status", key: "s", want: "status · set-a"},
		{name: "mute", key: "m", want: "mute · set-a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opened, _ := setDashboard(t, "set-a", "set-b")
			opened = bulkPress(t, opened, selKeyRune('a'))
			actionsAt := menuRuleLine(t, opened.View().Content)
			actionsBlock := blockLines(t, opened)
			opened = bulkPress(t, opened, selKeyEsc())

			opened = bulkPress(t, opened, selKeyRune(rune(tc.key[0])))
			if opened.menu == nil || (opened.menu.status == nil && opened.menu.mute == nil) {
				t.Fatalf("`%s` did not open the %s menu", tc.key, tc.name)
			}
			subView := opened.View().Content
			subLines := strings.Split(subView, "\n")
			if !strings.Contains(ui.StripANSI(subView), tc.want) {
				t.Fatalf("%s rule does not name its target:\n%s", tc.name, ui.StripANSI(subView))
			}
			// Same path, same anchor: both blocks end on the row above the hint line,
			// so the submenu's first row differs from the parent's only by height.
			subAt := dashboardTestLineIndex(subLines, tc.want)
			if want := len(subLines) - 1 - blockLines(t, opened); subAt != want {
				t.Fatalf("%s submenu opens at line %d, want %d", tc.name, subAt, want)
			}
			if actionsAt+actionsBlock != subAt+blockLines(t, opened) {
				t.Fatalf("%s submenu ends at a different row than the menu it opened from", tc.name)
			}
		})
	}
}
