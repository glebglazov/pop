package ui

import (
	"errors"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// killVerdicts is the kill callback with a per-pane answer, which is what the
// partial-failure rules need: one marked pane refuses and the rest still go.
type killVerdicts struct {
	killed []string
	errs   map[string]error
}

func (k *killVerdicts) callbacks() AttentionCallbacks {
	return AttentionCallbacks{
		KillPane: func(paneID string) error {
			k.killed = append(k.killed, paneID)
			return k.errs[paneID]
		},
	}
}

func keyY() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'y', Text: "y"} }

// TestMonitorVerbCapabilities pins the capability table itself: kill, follow
// and unmonitor are the bindings granted the plural mode, and a binding that
// declares nothing acts on one pane and says so (ADR-0254 decision 5).
func TestMonitorVerbCapabilities(t *testing.T) {
	pluralBindings := map[*key.Binding]bool{
		&dashboardKeys.KillPane:   true,
		&dashboardKeys.FollowPane: true,
		&dashboardKeys.Unmonitor:  true,
	}
	for binding, verb := range dashboardVerbs {
		if verb.plural != pluralBindings[binding] {
			t.Errorf("%q plural = %v, want %v", verb.name, verb.plural, pluralBindings[binding])
		}
	}

	d := newMonitorDashboard(selectionPanes(3), AttentionCallbacks{})
	d = markPanes(d, "%1")
	// ToggleFollowView has no entry: silence is singular, and the refusal names
	// the key because the table gave it no verb name.
	if !d.refuseSingular(&dashboardKeys.ToggleFollowView) {
		t.Fatal("a binding with no entry was treated as plural")
	}
	if got := d.flash.Text(); !strings.Contains(got, "F") || !strings.Contains(got, "acts on one pane") {
		t.Errorf("flash = %q, want the unlisted key refused by name", got)
	}
}

// TestMonitorBulkKillConfirmed drives the whole plural kill through the keyboard:
// mark three panes of five, ask once, answer once.
func TestMonitorBulkKillConfirmed(t *testing.T) {
	kills := &killVerdicts{}
	d := newMonitorDashboard(selectionPanes(5), kills.callbacks())
	d = markPanes(d, "%1", "%2", "%3")

	d = pressMonitor(d, ctrlX())
	if d.killPrompt == nil {
		t.Fatal("expected a kill prompt over the selection")
	}
	if hints := StripANSI(d.buildHints()); !containsSubstring(hints, "Kill 3 panes? y/N") {
		t.Errorf("bottom line = %q, want the plural y/N question", hints)
	}
	if len(kills.killed) != 0 {
		t.Fatalf("killed %v before the confirmation", kills.killed)
	}

	d = pressMonitor(d, keyY())
	if strings.Join(kills.killed, ",") != "%1,%2,%3" {
		t.Fatalf("killed = %v, want every marked pane", kills.killed)
	}
	if got := paneIDs(d.allPanes); strings.Join(got, ",") != "%4,%5" {
		t.Errorf("monitored set = %v, want each killed pane's entry gone", got)
	}
	if got := paneIDs(d.panes); strings.Join(got, ",") != "%4,%5" {
		t.Errorf("view = %v, want only the survivors", got)
	}
	if d.selection.Active() {
		t.Errorf("selection still holds %d panes after a clean run", d.selection.Len())
	}
	if d.list.RegionCount() != 0 {
		t.Errorf("region count = %d, want 0 — the mode is left", d.list.RegionCount())
	}
	if got := d.flash.Text(); got != "killed 3 panes" {
		t.Errorf("flash = %q, want %q", got, "killed 3 panes")
	}
	if !d.dirty {
		t.Error("expected dirty = true")
	}
}

// TestMonitorBulkKillAnswersTheCapturedSet pins that the prompt is answered
// against the panes it opened over: a pane the poll dropped in between is
// reported rather than quietly leaving the set the human agreed to.
func TestMonitorBulkKillAnswersTheCapturedSet(t *testing.T) {
	kills := &killVerdicts{}
	d := newMonitorDashboard(selectionPanes(4), kills.callbacks())
	d = markPanes(d, "%1", "%2", "%3")
	d = pressMonitor(d, ctrlX())

	// The pane died on its own between the question and the answer.
	d.reloadFunc = func() []AttentionPane {
		return []AttentionPane{
			{PaneID: "%1", Session: "s", Name: "pane%1"},
			{PaneID: "%3", Session: "s", Name: "pane%3"},
			{PaneID: "%4", Session: "s", Name: "pane%4"},
		}
	}
	m, _ := d.Update(reloadTickMsg{})
	d = m.(*MonitorDashboard)
	if d.killPrompt == nil {
		t.Fatal("the poll closed the prompt")
	}

	d = pressMonitor(d, keyY())
	if strings.Join(kills.killed, ",") != "%1,%3" {
		t.Fatalf("killed = %v, want the two panes that were still there", kills.killed)
	}
	got := d.flash.Text()
	if !strings.Contains(got, "1 failed") || !strings.Contains(got, "%2") {
		t.Errorf("flash = %q, want the gone pane reported as a failure", got)
	}
	if !strings.Contains(got, "killed 2 panes") {
		t.Errorf("flash = %q, want the successes counted too", got)
	}
}

// TestMonitorBulkKillDeclined pins that a refused prompt costs nothing: no kill,
// and the whole Selection still stands.
func TestMonitorBulkKillDeclined(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  tea.KeyPressMsg
	}{
		{"n", tea.KeyPressMsg{Code: 'n', Text: "n"}},
		{"esc", tea.KeyPressMsg{Code: tea.KeyEscape}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kills := &killVerdicts{}
			d := newMonitorDashboard(selectionPanes(4), kills.callbacks())
			d = markPanes(d, "%1", "%2")
			d = pressMonitor(d, ctrlX())
			d = pressMonitor(d, tc.key)

			if d.killPrompt != nil {
				t.Error("prompt still open after the refusal")
			}
			if len(kills.killed) != 0 {
				t.Errorf("killed %v after the refusal", kills.killed)
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

// TestMonitorBulkKillSkipsOwnPane pins ADR-0205's rule inside the batch: the pane
// the dashboard runs in survives without failing the run, and the outcome says it
// was skipped rather than leaving an unexplained survivor.
func TestMonitorBulkKillSkipsOwnPane(t *testing.T) {
	kills := &killVerdicts{}
	d := newMonitorDashboard(selectionPanes(4), kills.callbacks(), WithMonitorDashboardCurrentPaneID("%2"))
	d = markPanes(d, "%1", "%2", "%3")
	d = pressMonitor(d, ctrlX())
	d = pressMonitor(d, keyY())

	if strings.Join(kills.killed, ",") != "%1,%3" {
		t.Fatalf("killed = %v, want the dashboard's own pane skipped", kills.killed)
	}
	if _, ok := d.paneByID("%2"); !ok {
		t.Error("the dashboard's own pane left the monitored set")
	}
	got := d.flash.Text()
	if !strings.Contains(got, "killed 2 panes") || !strings.Contains(got, "skipped the pane the dashboard is running in") {
		t.Errorf("flash = %q, want the successes and the skip", got)
	}
	if d.selection.Active() {
		t.Errorf("selection holds %d panes, want none — a skip is not a failure", d.selection.Len())
	}
}

// TestMonitorBulkKillPromptDisabled pins the setting governing in both directions:
// someone who turned the single-pane prompt off gets no plural prompt either.
func TestMonitorBulkKillPromptDisabled(t *testing.T) {
	kills := &killVerdicts{}
	d := newMonitorDashboard(selectionPanes(4), kills.callbacks(), WithMonitorDashboardKillPrompt(false))
	d = markPanes(d, "%1", "%2")
	d = pressMonitor(d, ctrlX())

	if d.killPrompt != nil {
		t.Error("a prompt opened although the setting is off")
	}
	if strings.Join(kills.killed, ",") != "%1,%2" {
		t.Fatalf("killed = %v, want both marked panes killed at once", kills.killed)
	}
	if d.selection.Active() {
		t.Errorf("selection holds %d panes after a clean run", d.selection.Len())
	}
}

// TestMonitorBulkKillFailures pins decision 6's reporting and collapse: one
// reason or a bare count, and the marks that remain are exactly the retryable
// ones — still in the region, so a retry needs no re-marking.
func TestMonitorBulkKillFailures(t *testing.T) {
	t.Run("one failure flashes its reason and stays marked", func(t *testing.T) {
		kills := &killVerdicts{errs: map[string]error{"%1": errors.New("no such pane")}}
		d := newMonitorDashboard(selectionPanes(4), kills.callbacks(), WithMonitorDashboardKillPrompt(false))
		d = markPanes(d, "%1", "%2", "%3")
		d = pressMonitor(d, ctrlX())

		got := d.flash.Text()
		if !strings.Contains(got, "no such pane") || !strings.Contains(got, "1 failed") {
			t.Errorf("flash = %q, want the one reason", got)
		}
		if d.selection.Len() != 1 || !d.selection.Has("%1") {
			t.Fatalf("selection = %d panes, want exactly the pane that failed", d.selection.Len())
		}
		if ids := paneIDs(d.panes); ids[len(ids)-1] != "%1" || d.list.RegionCount() != 1 {
			t.Errorf("view = %v with region %d, want the failed pane alone in the region", ids, d.list.RegionCount())
		}
		if _, ok := d.paneByID("%1"); !ok {
			t.Error("a failed kill took the pane out of the monitored set")
		}
	})

	t.Run("several failures flash a bare count", func(t *testing.T) {
		kills := &killVerdicts{errs: map[string]error{
			"%1": errors.New("no such pane"),
			"%2": errors.New("permission denied"),
		}}
		d := newMonitorDashboard(selectionPanes(4), kills.callbacks(), WithMonitorDashboardKillPrompt(false))
		d = markPanes(d, "%1", "%2", "%3")
		d = pressMonitor(d, ctrlX())

		got := d.flash.Text()
		if !strings.Contains(got, "2 failed") {
			t.Errorf("flash = %q, want a bare count", got)
		}
		if strings.Contains(got, "no such pane") || strings.Contains(got, "permission denied") {
			t.Errorf("flash = %q, want no reason when several failed", got)
		}
		if d.selection.Len() != 2 || !d.selection.Has("%1") || !d.selection.Has("%2") {
			t.Errorf("selection = %d panes, want exactly the two that failed", d.selection.Len())
		}
	})
}

// TestMonitorBulkKillEmptiesTheList pins that a plural kill quits the dashboard
// when nothing is left, exactly as a single one does.
func TestMonitorBulkKillEmptiesTheList(t *testing.T) {
	kills := &killVerdicts{}
	d := newMonitorDashboard(selectionPanes(2), kills.callbacks())
	d = markPanes(d, "%1", "%2")
	d = pressMonitor(d, ctrlX())

	m, cmd := d.Update(keyY())
	d = m.(*MonitorDashboard)
	if cmd == nil {
		t.Error("expected a quit cmd once the bulk kill emptied the list")
	}
	if d.result.Action != MonitorDashboardActionCancel {
		t.Errorf("action = %d, want MonitorDashboardActionCancel", d.result.Action)
	}
}

// TestMonitorBulkKillAbsentInPickerMode keeps the promise picker mode makes: no
// marks form there, so C-x has no plural path to take.
func TestMonitorBulkKillAbsentInPickerMode(t *testing.T) {
	kills := &killVerdicts{}
	d := newMonitorDashboard(selectionPanes(3), kills.callbacks(), WithMonitorDashboardPickerMode("alt"))
	d = pressMonitor(d, keyTab())
	d = pressMonitor(d, ctrlX())
	d = pressMonitor(d, keyY())

	if len(kills.killed) != 0 || d.killPrompt != nil || len(d.panes) != 3 {
		t.Errorf("picker mode was not inert: killed=%v prompt=%v panes=%d", kills.killed, d.killPrompt != nil, len(d.panes))
	}
}
