package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func keyF() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'f', Text: "f"} }
func keyX() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'x', Text: "x"} }
func keyN() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'n', Text: "n"} }

// unmonitorCalls records the unmonitor callback with a per-pane answer, the
// same shape killVerdicts uses, since the staleness rule (a pane gone between
// the prompt and the answer) is the only way this verb can partially fail.
type unmonitorCalls struct {
	unmonitored []string
}

func (u *unmonitorCalls) callbacks() AttentionCallbacks {
	return AttentionCallbacks{
		Unmonitor: func(paneID string) { u.unmonitored = append(u.unmonitored, paneID) },
	}
}

// followCalls records the follow toggle callback.
type followCalls struct {
	toggled []string
}

func (f *followCalls) callbacks() AttentionCallbacks {
	return AttentionCallbacks{
		ToggleFollow: func(paneID string) { f.toggled = append(f.toggled, paneID) },
	}
}

// followingPanes builds panes like selectionPanes, but with the named panes
// already following — the mixed state the toggle-all rule has to resolve.
func followingPanes(n int, followed ...string) []AttentionPane {
	panes := selectionPanes(n)
	set := make(map[string]bool, len(followed))
	for _, id := range followed {
		set[id] = true
	}
	for i := range panes {
		panes[i].Following = set[panes[i].PaneID]
	}
	return panes
}

// TestMonitorBulkUnmonitorConfirmed drives the whole plural unmonitor through
// the keyboard: mark three of five, ask once, answer once, and every process
// keeps running — only the monitor entry goes.
func TestMonitorBulkUnmonitorConfirmed(t *testing.T) {
	calls := &unmonitorCalls{}
	d := newMonitorDashboard(selectionPanes(5), calls.callbacks())
	d = markPanes(d, "%1", "%2", "%3")

	d = pressMonitor(d, keyX())
	if d.writePrompt == nil {
		t.Fatal("expected a write prompt over the selection")
	}
	if hints := StripANSI(d.buildHints()); !containsSubstring(hints, "unmonitor 3? y/N") {
		t.Errorf("bottom line = %q, want the plural y/N question", hints)
	}
	if len(calls.unmonitored) != 0 {
		t.Fatalf("unmonitored %v before the confirmation", calls.unmonitored)
	}

	d = pressMonitor(d, keyY())
	if strings.Join(calls.unmonitored, ",") != "%1,%2,%3" {
		t.Fatalf("unmonitored = %v, want every marked pane", calls.unmonitored)
	}
	if got := paneIDs(d.allPanes); strings.Join(got, ",") != "%4,%5" {
		t.Errorf("monitored set = %v, want each unmonitored pane's entry gone", got)
	}
	if got := paneIDs(d.panes); strings.Join(got, ",") != "%4,%5" {
		t.Errorf("view = %v, want only the survivors", got)
	}
	if d.selection.Active() {
		t.Errorf("selection still holds %d panes after a clean run", d.selection.Len())
	}
	if got := d.flash.Text(); got != "unmonitored 3 panes" {
		t.Errorf("flash = %q, want %q", got, "unmonitored 3 panes")
	}
	if !d.dirty {
		t.Error("expected dirty = true")
	}
}

// TestMonitorBulkUnmonitorDeclined pins that a refused prompt costs nothing:
// no unmonitor, and the whole Selection still stands.
func TestMonitorBulkUnmonitorDeclined(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  tea.KeyPressMsg
	}{
		{"n", keyN()},
		{"esc", tea.KeyPressMsg{Code: tea.KeyEscape}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := &unmonitorCalls{}
			d := newMonitorDashboard(selectionPanes(4), calls.callbacks())
			d = markPanes(d, "%1", "%2")
			d = pressMonitor(d, keyX())
			d = pressMonitor(d, tc.key)

			if d.writePrompt != nil {
				t.Error("prompt still open after the refusal")
			}
			if len(calls.unmonitored) != 0 {
				t.Errorf("unmonitored %v after the refusal", calls.unmonitored)
			}
			if d.selection.Len() != 2 || !d.selection.Has("%1") || !d.selection.Has("%2") {
				t.Errorf("selection holds %d panes, want both marks kept", d.selection.Len())
			}
			if len(d.panes) != 4 {
				t.Errorf("view = %v, want every pane alive", paneIDs(d.panes))
			}
		})
	}
}

// TestMonitorBulkUnmonitorAnswersTheCapturedSet pins that the prompt is
// answered against the panes it opened over: a pane the poll dropped in
// between is reported rather than quietly leaving the set the human agreed to.
func TestMonitorBulkUnmonitorAnswersTheCapturedSet(t *testing.T) {
	calls := &unmonitorCalls{}
	d := newMonitorDashboard(selectionPanes(4), calls.callbacks())
	d = markPanes(d, "%1", "%2", "%3")
	d = pressMonitor(d, keyX())

	d.reloadFunc = func() []AttentionPane {
		return []AttentionPane{
			{PaneID: "%1", Session: "s", Name: "pane%1"},
			{PaneID: "%3", Session: "s", Name: "pane%3"},
			{PaneID: "%4", Session: "s", Name: "pane%4"},
		}
	}
	m, _ := d.Update(reloadTickMsg{})
	d = m.(*MonitorDashboard)
	if d.writePrompt == nil {
		t.Fatal("the poll closed the prompt")
	}

	d = pressMonitor(d, keyY())
	if strings.Join(calls.unmonitored, ",") != "%1,%3" {
		t.Fatalf("unmonitored = %v, want the two panes that were still there", calls.unmonitored)
	}
	got := d.flash.Text()
	if !strings.Contains(got, "1 failed") || !strings.Contains(got, "%2") {
		t.Errorf("flash = %q, want the gone pane reported as a failure", got)
	}
	if !strings.Contains(got, "unmonitored 2 panes") {
		t.Errorf("flash = %q, want the successes counted too", got)
	}
	// %2 is not merely a failure — it is gone from the monitored set entirely,
	// so parkSelected's Retain drops its mark: there is nothing left to retry.
	if d.selection.Active() {
		t.Errorf("selection still holds %d panes, want none — the failed pane no longer exists", d.selection.Len())
	}
}

// TestMonitorBulkUnmonitorEmptiesTheList pins that a plural unmonitor quits
// the dashboard when nothing is left, exactly as a bulk kill does.
func TestMonitorBulkUnmonitorEmptiesTheList(t *testing.T) {
	calls := &unmonitorCalls{}
	d := newMonitorDashboard(selectionPanes(2), calls.callbacks())
	d = markPanes(d, "%1", "%2")
	d = pressMonitor(d, keyX())

	m, cmd := d.Update(keyY())
	d = m.(*MonitorDashboard)
	if cmd == nil {
		t.Error("expected a quit cmd once the bulk unmonitor emptied the list")
	}
	if d.result.Action != MonitorDashboardActionCancel {
		t.Errorf("action = %d, want MonitorDashboardActionCancel", d.result.Action)
	}
}

// TestMonitorBulkFollowFollowsAllWhenAnyUnfollowed pins the toggle-all rule's
// first branch: one followed pane in the marked set does not stop the rest
// from being driven to followed too.
func TestMonitorBulkFollowFollowsAllWhenAnyUnfollowed(t *testing.T) {
	calls := &followCalls{}
	d := newMonitorDashboard(followingPanes(4, "%1"), calls.callbacks())
	d = markPanes(d, "%1", "%2", "%3")

	d = pressMonitor(d, keyF())
	if d.writePrompt == nil {
		t.Fatal("expected a write prompt over the selection")
	}
	if hints := StripANSI(d.buildHints()); !containsSubstring(hints, "follow 3? y/N") {
		t.Errorf("bottom line = %q, want the computed direction and count", hints)
	}

	d = pressMonitor(d, keyY())
	// %1 was already following, so only the two unfollowed panes are toggled.
	if strings.Join(calls.toggled, ",") != "%2,%3" {
		t.Fatalf("toggled = %v, want only the panes that changed", calls.toggled)
	}
	for _, id := range []string{"%1", "%2", "%3"} {
		pane, ok := d.paneByID(id)
		if !ok || !pane.Following {
			t.Errorf("pane %s following = %v, want true", id, pane.Following)
		}
	}
	if got := d.flash.Text(); got != "followed 2 panes" {
		t.Errorf("flash = %q, want %q", got, "followed 2 panes")
	}
	if d.selection.Active() {
		t.Errorf("selection still holds %d panes after a clean run", d.selection.Len())
	}
}

// TestMonitorBulkFollowUnfollowsAllWhenAllFollowed pins the toggle-all rule's
// other branch: a marked set that is already entirely followed drives to
// unfollowed rather than flipping each pane independently.
func TestMonitorBulkFollowUnfollowsAllWhenAllFollowed(t *testing.T) {
	calls := &followCalls{}
	d := newMonitorDashboard(followingPanes(4, "%1", "%2"), calls.callbacks())
	d = markPanes(d, "%1", "%2")

	d = pressMonitor(d, keyF())
	if hints := StripANSI(d.buildHints()); !containsSubstring(hints, "unfollow 2? y/N") {
		t.Errorf("bottom line = %q, want the unfollow direction and count", hints)
	}

	d = pressMonitor(d, keyY())
	if strings.Join(calls.toggled, ",") != "%1,%2" {
		t.Fatalf("toggled = %v, want both marked panes", calls.toggled)
	}
	for _, id := range []string{"%1", "%2"} {
		pane, ok := d.paneByID(id)
		if !ok || pane.Following {
			t.Errorf("pane %s following = %v, want false", id, pane.Following)
		}
	}
	if got := d.flash.Text(); got != "unfollowed 2 panes" {
		t.Errorf("flash = %q, want %q", got, "unfollowed 2 panes")
	}
}

// TestMonitorBulkFollowDeclined pins that a refused prompt flips nothing and
// keeps every mark.
func TestMonitorBulkFollowDeclined(t *testing.T) {
	calls := &followCalls{}
	d := newMonitorDashboard(followingPanes(4), calls.callbacks())
	d = markPanes(d, "%1", "%2")
	d = pressMonitor(d, keyF())
	d = pressMonitor(d, keyN())

	if d.writePrompt != nil {
		t.Error("prompt still open after the refusal")
	}
	if len(calls.toggled) != 0 {
		t.Errorf("toggled %v after the refusal", calls.toggled)
	}
	if d.selection.Len() != 2 {
		t.Errorf("selection holds %d panes, want both marks kept", d.selection.Len())
	}
}

// TestMonitorBulkFollowAnswersTheCapturedSet pins the same staleness rule the
// bulk kill and bulk unmonitor honour: a pane gone by the time the answer
// arrives fails and stays marked, and the rest still go.
func TestMonitorBulkFollowAnswersTheCapturedSet(t *testing.T) {
	calls := &followCalls{}
	d := newMonitorDashboard(followingPanes(4), calls.callbacks())
	d = markPanes(d, "%1", "%2", "%3")
	d = pressMonitor(d, keyF())

	d.reloadFunc = func() []AttentionPane {
		return []AttentionPane{
			{PaneID: "%1", Session: "s", Name: "pane%1"},
			{PaneID: "%3", Session: "s", Name: "pane%3"},
			{PaneID: "%4", Session: "s", Name: "pane%4"},
		}
	}
	m, _ := d.Update(reloadTickMsg{})
	d = m.(*MonitorDashboard)
	if d.writePrompt == nil {
		t.Fatal("the poll closed the prompt")
	}

	d = pressMonitor(d, keyY())
	if strings.Join(calls.toggled, ",") != "%1,%3" {
		t.Fatalf("toggled = %v, want the two panes that were still there", calls.toggled)
	}
	got := d.flash.Text()
	if !strings.Contains(got, "1 failed") || !strings.Contains(got, "%2") {
		t.Errorf("flash = %q, want the gone pane reported as a failure", got)
	}
	if !strings.Contains(got, "followed 2 panes") {
		t.Errorf("flash = %q, want the successes counted too", got)
	}
	// %2 is gone from the monitored set entirely, not merely a failure, so
	// parkSelected's Retain drops its mark: there is nothing left to retry.
	if d.selection.Active() {
		t.Errorf("selection still holds %d panes, want none — the failed pane no longer exists", d.selection.Len())
	}
}

// TestMonitorSingleFollowUnmonitorUnchanged pins that without a Selection
// open, f and x still act on the cursored pane at once — no prompt, no
// capability-table detour.
func TestMonitorSingleFollowUnmonitorUnchanged(t *testing.T) {
	followCB := &followCalls{}
	unmonitorCB := &unmonitorCalls{}
	cb := AttentionCallbacks{
		ToggleFollow: followCB.callbacks().ToggleFollow,
		Unmonitor:    unmonitorCB.callbacks().Unmonitor,
	}
	d := newMonitorDashboard(selectionPanes(3), cb)
	d.list.SetCursorToKey("%1")
	d.syncFromList()

	d = pressMonitor(d, keyF())
	if d.writePrompt != nil {
		t.Fatal("a single-pane follow opened a prompt")
	}
	if strings.Join(followCB.toggled, ",") != "%1" {
		t.Fatalf("toggled = %v, want the cursored pane at once", followCB.toggled)
	}

	d = pressMonitor(d, keyX())
	if d.writePrompt != nil {
		t.Fatal("a single-pane unmonitor opened a prompt")
	}
	if strings.Join(unmonitorCB.unmonitored, ",") != "%1" {
		t.Fatalf("unmonitored = %v, want the cursored pane at once", unmonitorCB.unmonitored)
	}
	if got := paneIDs(d.panes); strings.Join(got, ",") != "%2,%3" {
		t.Errorf("view = %v, want the cursored pane gone and nothing else touched", got)
	}
}
