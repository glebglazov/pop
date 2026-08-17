package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func keyShiftTab() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift} }

func pressMonitor(d *MonitorDashboard, msg tea.KeyPressMsg) *MonitorDashboard {
	m, _ := d.Update(msg)
	return m.(*MonitorDashboard)
}

func selectionPanes(n int) []AttentionPane {
	panes := make([]AttentionPane, 0, n)
	for i := 1; i <= n; i++ {
		id := "%" + string(rune('0'+i))
		panes = append(panes, AttentionPane{PaneID: id, Session: "s", Name: "pane" + id})
	}
	return panes
}

// markPanes presses tab on each named pane in turn, the way a human marking a
// batch does: land the cursor on the row, then hit the key.
func markPanes(d *MonitorDashboard, ids ...string) *MonitorDashboard {
	for _, id := range ids {
		if !d.list.SetCursorToKey(id) {
			panic("no pane " + id + " to mark")
		}
		d.syncFromList()
		d = pressMonitor(d, keyTab())
	}
	return d
}

func paneIDs(panes []AttentionPane) []string {
	ids := make([]string, len(panes))
	for i, pane := range panes {
		ids[i] = pane.PaneID
	}
	return ids
}

func plainRows(d *MonitorDashboard) []string {
	rows := d.list.VisibleRows()
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = StripANSI(row)
	}
	return out
}

// TestMonitorSelectionTabMarks pins tab's whole outcome: the pane is marked, its
// row lifts into the reserved region at the top, exactly once, and the cursor
// lands on the row that followed it (ADR-0215 decisions 3 and 8).
func TestMonitorSelectionTabMarks(t *testing.T) {
	t.Run("the cursored pane lifts to the top and the cursor moves on", func(t *testing.T) {
		d := newMonitorDashboard(selectionPanes(4), AttentionCallbacks{})
		d.cursor = 1 // %2
		d = pressMonitor(d, keyTab())

		if !d.selection.Has("%2") {
			t.Fatalf("tab did not mark %%2; selection holds %d", d.selection.Len())
		}
		if got := paneIDs(d.panes); strings.Join(got, ",") != "%2,%1,%3,%4" {
			t.Errorf("view order = %v, want the marked pane first", got)
		}
		if d.list.RegionCount() != 1 {
			t.Errorf("region count = %d, want 1", d.list.RegionCount())
		}
		if pane, _ := d.list.Selected(); pane.PaneID != "%3" {
			t.Errorf("cursor sits on %s, want %%3 — the row that followed the marked one", pane.PaneID)
		}
	})

	t.Run("a marked row appears exactly once", func(t *testing.T) {
		d := newMonitorDashboard(selectionPanes(4), AttentionCallbacks{})
		d = markPanes(d, "%2")
		seen := 0
		for _, pane := range d.panes {
			if pane.PaneID == "%2" {
				seen++
			}
		}
		if seen != 1 || len(d.panes) != 4 {
			t.Fatalf("%%2 appears %d times over %d rows, want once over 4 — a mark moves a row, it does not copy it", seen, len(d.panes))
		}
	})

	t.Run("tab again unmarks and the row returns to its sorted position", func(t *testing.T) {
		d := newMonitorDashboard(selectionPanes(4), AttentionCallbacks{})
		d = markPanes(d, "%2")
		d = markPanes(d, "%2")

		if d.selection.Active() {
			t.Fatalf("selection still holds %d panes after unmarking the only one", d.selection.Len())
		}
		if got := paneIDs(d.panes); strings.Join(got, ",") != "%1,%2,%3,%4" {
			t.Errorf("view order = %v, want the unmarked pane back in its own place", got)
		}
		if d.list.RegionCount() != 0 {
			t.Errorf("region count = %d, want 0", d.list.RegionCount())
		}
	})

	t.Run("the region follows the list's own order, not the marking order", func(t *testing.T) {
		d := newMonitorDashboard(selectionPanes(4), AttentionCallbacks{})
		d = markPanes(d, "%3", "%1")
		if got := paneIDs(d.panes); strings.Join(got, ",") != "%1,%3,%2,%4" {
			t.Errorf("view order = %v, want %%1 before %%3 — marking order is not state", got)
		}
	})
}

// TestMonitorSelectionShiftTabClears pins that the mode is left by dropping the
// marks, and that the rows go back where they came from.
func TestMonitorSelectionShiftTabClears(t *testing.T) {
	d := newMonitorDashboard(selectionPanes(4), AttentionCallbacks{})
	d = markPanes(d, "%3", "%1")
	d = pressMonitor(d, keyShiftTab())

	if d.selection.Active() {
		t.Fatalf("selection still holds %d panes", d.selection.Len())
	}
	if got := paneIDs(d.panes); strings.Join(got, ",") != "%1,%2,%3,%4" {
		t.Errorf("view order = %v, want the original order back", got)
	}
	if d.modeWord() != "" {
		t.Errorf("mode word = %q, want none — the mode is derived from the marks", d.modeWord())
	}
}

// TestMonitorSelectionChrome pins what the surface says while rows are marked:
// the dim count line under the region, the mode word at the left of the bottom
// line, and the overflow note when the viewport cap bites.
func TestMonitorSelectionChrome(t *testing.T) {
	t.Run("a dim count line divides the region from the rest", func(t *testing.T) {
		d := newMonitorDashboard(selectionPanes(5), AttentionCallbacks{})
		d = markPanes(d, "%1", "%2")

		rows := plainRows(d)
		sep := -1
		for i, row := range rows {
			if strings.Contains(row, "2 selected") && strings.Contains(row, "─") {
				sep = i
			}
		}
		if sep < 0 {
			t.Fatalf("no `2 selected` rule in the list:\n%s", strings.Join(rows, "\n"))
		}
		if !strings.Contains(rows[sep-1], "pane%2") {
			t.Errorf("line above the separator = %q, want the last marked pane", rows[sep-1])
		}
		if !strings.Contains(rows[sep+1], "pane%3") {
			t.Errorf("line below the separator = %q, want the first unmarked pane", rows[sep+1])
		}
		want := SelectionSeparator(2, d.leftWidth())
		if got := d.list.VisibleRows()[sep]; got != want {
			t.Errorf("separator line = %q, want the dim rule %q", got, want)
		}
	})

	t.Run("a narrow list truncates the rule without wrapping", func(t *testing.T) {
		d := newMonitorDashboard(selectionPanes(4), AttentionCallbacks{})
		d.list.SetWidth(10)
		d = markPanes(d, "%1", "%2")

		rows := plainRows(d)
		sep := -1
		for i, row := range rows {
			if strings.Contains(row, "─") {
				sep = i
				break
			}
		}
		if sep < 0 {
			t.Fatalf("no rule in the list:\n%s", strings.Join(rows, "\n"))
		}
		if got := len([]rune(rows[sep])); got != 10 {
			t.Errorf("rule width = %d, want 10: %q", got, rows[sep])
		}
		if strings.Contains(rows[sep], "\n") {
			t.Errorf("rule wrapped: %q", rows[sep])
		}
		if got := d.list.VisibleRows()[sep]; got != SelectionSeparator(2, 10) {
			t.Errorf("separator = %q, want SelectionSeparator(2, 10)", got)
		}
	})

	t.Run("the mode word holds the left of the bottom line", func(t *testing.T) {
		d := newMonitorDashboard(selectionPanes(4), AttentionCallbacks{})
		if view := StripANSI(d.View().Content); strings.Contains(view, SelectionMode) {
			t.Fatalf("mode word shown with nothing marked:\n%s", view)
		}
		d = markPanes(d, "%2")
		view := StripANSI(d.View().Content)
		if !strings.Contains(view, SelectionMode) {
			t.Fatalf("no %q in the view:\n%s", SelectionMode, view)
		}
		bottom := lastLine(view)
		if !strings.HasPrefix(strings.TrimSpace(bottom), SelectionMode) {
			t.Errorf("bottom line = %q, want the mode word at the left", bottom)
		}
		// The word is padded on both sides, so the hints never butt against it.
		if !strings.Contains(bottom, "  "+SelectionMode+"  j/k move") {
			t.Errorf("bottom line = %q, want the mode word evenly spaced from the hints", bottom)
		}
		d = pressMonitor(d, keyShiftTab())
		if view := StripANSI(d.View().Content); strings.Contains(view, SelectionMode) {
			t.Errorf("mode word survived shift+tab:\n%s", view)
		}
	})

	t.Run("the region is capped at a third of the viewport and says what it hid", func(t *testing.T) {
		d := newMonitorDashboard(selectionPanes(8), AttentionCallbacks{})
		d.list.Resize(9) // a third is three rows
		d = markPanes(d, "%1", "%2", "%3", "%4", "%5")

		rows := plainRows(d)
		if len(rows) != 9 {
			t.Fatalf("list drew %d lines, want 9", len(rows))
		}
		body := strings.Join(rows, "\n")
		if !strings.Contains(body, "5 selected") {
			t.Errorf("no count line over five marks:\n%s", body)
		}
		if !strings.Contains(body, "… +2 more selected") {
			t.Errorf("no overflow note for the two members the cap hid:\n%s", body)
		}
		drawn := 0
		for _, id := range []string{"%1", "%2", "%3", "%4", "%5"} {
			if strings.Contains(body, "pane"+id) {
				drawn++
			}
		}
		if drawn != 3 {
			t.Errorf("%d marked rows drawn, want 3 — a third of a nine-line viewport", drawn)
		}
	})
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return lines[len(lines)-1]
}

// TestMonitorSelectionSurvivesRebuild pins decision 1: the Selection is keyed by
// pane id, so the wholesale rebuild every second carries it, and a pane that
// leaves the monitored set takes its mark with it and says nothing.
func TestMonitorSelectionSurvivesRebuild(t *testing.T) {
	t.Run("a mark rides the rebuild", func(t *testing.T) {
		d := newMonitorDashboard(selectionPanes(4), AttentionCallbacks{})
		d = markPanes(d, "%2")
		// The reload hands back the same panes in a different order, as a poll
		// that re-sorted the monitored set would.
		d.reloadFunc = func() []AttentionPane {
			return []AttentionPane{
				{PaneID: "%4", Session: "s", Name: "pane%4"},
				{PaneID: "%3", Session: "s", Name: "pane%3"},
				{PaneID: "%2", Session: "s", Name: "pane%2"},
				{PaneID: "%1", Session: "s", Name: "pane%1"},
			}
		}
		m, _ := d.Update(reloadTickMsg{})
		d = m.(*MonitorDashboard)

		if !d.selection.Has("%2") {
			t.Fatal("the mark did not survive the rebuild")
		}
		if got := paneIDs(d.panes); got[0] != "%2" || d.list.RegionCount() != 1 {
			t.Errorf("view order = %v with region %d, want %%2 alone at the top", got, d.list.RegionCount())
		}
	})

	t.Run("a pane that leaves the monitored set is dropped silently", func(t *testing.T) {
		d := newMonitorDashboard(selectionPanes(4), AttentionCallbacks{})
		d = markPanes(d, "%2", "%3")
		d.reloadFunc = func() []AttentionPane {
			return []AttentionPane{
				{PaneID: "%1", Session: "s", Name: "pane%1"},
				{PaneID: "%3", Session: "s", Name: "pane%3"},
			}
		}
		m, _ := d.Update(reloadTickMsg{})
		d = m.(*MonitorDashboard)

		if d.selection.Len() != 1 || !d.selection.Has("%3") {
			t.Fatalf("selection holds %d panes, want only %%3", d.selection.Len())
		}
		if d.flash.Text() != "" {
			t.Errorf("flash = %q, want nothing — a gone pane is not an event", d.flash.Text())
		}
	})
}

// TestMonitorSelectionCursorAndRegion pins where the cursor may and may not be:
// never in the region by default or after a rebuild, but j/k walk in, which is
// how a mark is removed.
func TestMonitorSelectionCursorAndRegion(t *testing.T) {
	t.Run("a rebuild never moves the cursor into the region", func(t *testing.T) {
		d := newMonitorDashboard(selectionPanes(4), AttentionCallbacks{})
		d = markPanes(d, "%1", "%2")
		before, _ := d.list.Selected()
		d.reloadFunc = func() []AttentionPane { return selectionPanes(4) }
		m, _ := d.Update(reloadTickMsg{})
		d = m.(*MonitorDashboard)

		if d.cursor < d.list.RegionCount() {
			t.Fatalf("cursor = %d, inside a region of %d rows", d.cursor, d.list.RegionCount())
		}
		if pane, _ := d.list.Selected(); pane.PaneID != before.PaneID {
			t.Errorf("cursor moved from %s to %s over a rebuild", before.PaneID, pane.PaneID)
		}
	})

	t.Run("k walks into the region and tab there unmarks", func(t *testing.T) {
		d := newMonitorDashboard(selectionPanes(4), AttentionCallbacks{})
		d = markPanes(d, "%1", "%2")
		d.list.SetCursor(2) // the first row below the region
		d.syncFromList()

		d = pressMonitor(d, tea.KeyPressMsg{Code: 'k', Text: "k"})
		if d.cursor != 1 {
			t.Fatalf("cursor = %d, want 1 — k must enter the region", d.cursor)
		}
		d = pressMonitor(d, keyTab())
		if d.selection.Has("%2") {
			t.Error("tab inside the region did not unmark the row")
		}
	})
}

// TestMonitorSelectionRegionAwareJumps pins decision 4's jump rule: gg and G stop
// at the edge of the region the cursor is in before the edge of the whole list.
func TestMonitorSelectionRegionAwareJumps(t *testing.T) {
	newMarked := func(t *testing.T) *MonitorDashboard {
		t.Helper()
		d := newMonitorDashboard(selectionPanes(6), AttentionCallbacks{})
		d = markPanes(d, "%1", "%2")
		if d.list.RegionCount() != 2 {
			t.Fatalf("region count = %d, want 2", d.list.RegionCount())
		}
		return d
	}

	t.Run("G reaches the region's bottom, then the list's", func(t *testing.T) {
		d := newMarked(t)
		d.cursor = 0
		d = pressMonitor(d, tea.KeyPressMsg{Code: 'G', Text: "G"})
		if d.cursor != 1 {
			t.Fatalf("cursor = %d, want 1 — the bottom of the region", d.cursor)
		}
		d = pressMonitor(d, tea.KeyPressMsg{Code: 'G', Text: "G"})
		if d.cursor != 5 {
			t.Fatalf("cursor = %d, want 5 — the bottom of the whole list", d.cursor)
		}
	})

	t.Run("gg reaches the rest's top, then the list's", func(t *testing.T) {
		d := newMarked(t)
		d.cursor = 5
		d = pressG(pressG(d))
		if d.cursor != 2 {
			t.Fatalf("cursor = %d, want 2 — the top of the rows below the region", d.cursor)
		}
		d = pressG(pressG(d))
		if d.cursor != 0 {
			t.Fatalf("cursor = %d, want 0 — the top of the whole list", d.cursor)
		}
	})

	t.Run("G from below the region reaches the list's bottom at once", func(t *testing.T) {
		d := newMarked(t)
		d.cursor = 3
		d = pressMonitor(d, tea.KeyPressMsg{Code: 'G', Text: "G"})
		if d.cursor != 5 {
			t.Fatalf("cursor = %d, want 5", d.cursor)
		}
	})
}

// TestMonitorSelectionRefusesSingularVerbs pins decision 4: a verb that cannot be
// plural yet says so on the bottom line rather than acting on the cursored row or
// going quietly inert.
func TestMonitorSelectionRefusesSingularVerbs(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  tea.KeyPressMsg
		want string
	}{
		{"enter", tea.KeyPressMsg{Code: tea.KeyEnter}, "open and clear"},
		{"peek", tea.KeyPressMsg{Code: 'p', Text: "p"}, "peek"},
		{"toggle unread", tea.KeyPressMsg{Code: 'r', Text: "r"}, "toggle unread"},
		{"mark unread", tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl}, "mark unread"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var acted []string
			cb := AttentionCallbacks{
				MarkClear:    func(string) { acted = append(acted, "clear") },
				MarkUnread:   func(string) { acted = append(acted, "unread") },
				ToggleFollow: func(string) { acted = append(acted, "follow") },
				Unmonitor:    func(string) { acted = append(acted, "unmonitor") },
				KillPane:     func(string) error { acted = append(acted, "kill"); return nil },
			}
			d := newMonitorDashboard(selectionPanes(4), cb)
			d = markPanes(d, "%1", "%2")
			d = pressMonitor(d, tc.key)

			if len(acted) > 0 {
				t.Fatalf("%s acted (%v) while two panes were marked", tc.name, acted)
			}
			if d.result.Selected != nil || d.killPrompt != nil {
				t.Fatalf("%s handed a single pane on", tc.name)
			}
			if !strings.Contains(d.flash.Text(), tc.want) {
				t.Fatalf("flash = %q, want it to name %q", d.flash.Text(), tc.want)
			}
			if !strings.Contains(d.flash.Text(), "shift+tab") {
				t.Errorf("flash = %q, want the way out of the mode named", d.flash.Text())
			}
			// The mode outlives the message: a refusal is exactly when the human
			// needs to see which mode they are in.
			if view := StripANSI(d.View().Content); !strings.Contains(view, SelectionMode) || !strings.Contains(view, tc.want) {
				t.Errorf("bottom line lost the mode word or the reason:\n%s", lastLine(view))
			}
		})
	}
}

// TestMonitorSelectionNavigationStaysLive pins that nothing about getting around
// is gated by the mode — including the follow view, which a marked pane survives
// because a mark outranks a filter (decision 2).
func TestMonitorSelectionNavigationStaysLive(t *testing.T) {
	d := newMonitorDashboard(selectionPanes(5), AttentionCallbacks{})
	d = markPanes(d, "%1", "%2")

	d.cursor = 4
	d = pressMonitor(d, tea.KeyPressMsg{Code: 'k', Text: "k"})
	if d.cursor != 3 {
		t.Errorf("k did not move the cursor: %d", d.cursor)
	}
	d = pressMonitor(d, tea.KeyPressMsg{Code: 'j', Text: "j"})
	if d.cursor != 4 {
		t.Errorf("j did not move the cursor: %d", d.cursor)
	}

	d = pressMonitor(d, tea.KeyPressMsg{Code: 'h', Mod: tea.ModCtrl})
	if !d.showHelp {
		t.Error("C-h did not open help in selection mode")
	}
	d = pressMonitor(d, tea.KeyPressMsg{Code: 'h', Mod: tea.ModCtrl})

	d = pressMonitor(d, tea.KeyPressMsg{Code: 'F', Text: "F"})
	if !d.following {
		t.Fatal("F did not turn the follow view on")
	}
	if got := paneIDs(d.panes); strings.Join(got, ",") != "%1,%2" {
		t.Errorf("follow view = %v, want the marked panes to stay on screen", got)
	}
	if d.selection.Len() != 2 {
		t.Errorf("selection holds %d panes after the filter changed, want 2", d.selection.Len())
	}
}

// TestMonitorSelectionAbsentInPickerMode pins the promise picker mode makes: it
// mutates nothing, so no Selection is ever formed there.
func TestMonitorSelectionAbsentInPickerMode(t *testing.T) {
	d := newMonitorDashboard(selectionPanes(4), AttentionCallbacks{}, WithMonitorDashboardPickerMode("alt"))
	before := paneIDs(d.panes)
	d = pressMonitor(d, keyTab())
	d = pressMonitor(d, keyShiftTab())

	if d.selection.Active() {
		t.Fatalf("picker mode formed a selection of %d panes", d.selection.Len())
	}
	if d.list.RegionCount() != 0 {
		t.Errorf("region count = %d, want 0", d.list.RegionCount())
	}
	if got := paneIDs(d.panes); strings.Join(got, ",") != strings.Join(before, ",") {
		t.Errorf("view order = %v, want %v", got, before)
	}
}

// TestMonitorSelectionHelpDocumentsTheKeys keeps the C-h overlay the complete
// listing (ADR-0204).
func TestMonitorSelectionHelpDocumentsTheKeys(t *testing.T) {
	d := newMonitorDashboard(selectionPanes(4), AttentionCallbacks{})
	d.showHelp = true
	view := StripANSI(d.View().Content)
	for _, want := range []string{"Tab", "Shift+Tab"} {
		if !strings.Contains(view, want) {
			t.Errorf("help does not document %q:\n%s", want, view)
		}
	}
}
