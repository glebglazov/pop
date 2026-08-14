package ui

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// killRecorder is the kill callback plus the log of what it was asked to
// destroy, so a test can assert on the pane the dashboard chose as well as on
// the rows it kept.
type killRecorder struct {
	killed []string
	err    error
}

func (k *killRecorder) callbacks() AttentionCallbacks {
	return AttentionCallbacks{
		Unmonitor: func(string) {},
		KillPane: func(paneID string) error {
			k.killed = append(k.killed, paneID)
			return k.err
		},
	}
}

func ctrlX() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl} }

func threePanes() []AttentionPane {
	return []AttentionPane{
		{PaneID: "%1", Session: "s1", Name: "one"},
		{PaneID: "%2", Session: "s2", Name: "two"},
		{PaneID: "%3", Session: "s3", Name: "three"},
	}
}

func TestDashboardKillPanePrompt(t *testing.T) {
	t.Run("prompt then y kills the pane it named", func(t *testing.T) {
		kills := &killRecorder{}
		d := newMonitorDashboard(threePanes(), kills.callbacks())
		d.cursor = 1

		m, _ := d.Update(ctrlX())
		d = m.(*MonitorDashboard)
		if d.killPrompt == nil {
			t.Fatal("expected a kill prompt")
		}
		if len(kills.killed) != 0 {
			t.Fatalf("killed %v before confirmation", kills.killed)
		}
		if hints := d.buildHints(); !containsSubstring(hints, "Kill two? y/N") {
			t.Errorf("bottom line = %q, want the y/N question", hints)
		}

		m, _ = d.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
		d = m.(*MonitorDashboard)
		if d.killPrompt != nil {
			t.Error("prompt still open after confirmation")
		}
		if len(kills.killed) != 1 || kills.killed[0] != "%2" {
			t.Fatalf("killed = %v, want [%%2]", kills.killed)
		}
		if len(d.panes) != 2 || d.panes[0].PaneID != "%1" || d.panes[1].PaneID != "%3" {
			t.Errorf("panes = %v, want %%1 and %%3", d.panes)
		}
		if len(d.allPanes) != 2 {
			t.Errorf("allPanes len = %d, want 2", len(d.allPanes))
		}
		if d.cursor != 1 {
			t.Errorf("cursor = %d, want 1 (clamped to the same index)", d.cursor)
		}
		if !d.dirty {
			t.Error("expected dirty = true")
		}
		if got := d.flash.Text(); got != "killed two" {
			t.Errorf("flash = %q, want %q", got, "killed two")
		}
	})

	t.Run("cursor cannot move while the prompt is open", func(t *testing.T) {
		kills := &killRecorder{}
		d := newMonitorDashboard(threePanes(), kills.callbacks())
		d.cursor = 1

		m, _ := d.Update(ctrlX())
		d = m.(*MonitorDashboard)
		for _, key := range []tea.KeyPressMsg{
			{Code: 'k', Text: "k"},
			{Code: 'j', Text: "j"},
			{Code: 'p', Mod: tea.ModCtrl},
		} {
			m, _ = d.Update(key)
			d = m.(*MonitorDashboard)
		}
		if d.cursor != 1 {
			t.Fatalf("cursor = %d, want 1 — navigation must be inert", d.cursor)
		}

		m, _ = d.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
		d = m.(*MonitorDashboard)
		if len(kills.killed) != 1 || kills.killed[0] != "%2" {
			t.Errorf("killed = %v, want the pane the prompt named (%%2)", kills.killed)
		}
	})

	for _, tc := range []struct {
		name string
		key  tea.KeyPressMsg
	}{
		{"n", tea.KeyPressMsg{Code: 'n', Text: "n"}},
		{"enter", tea.KeyPressMsg{Code: tea.KeyEnter}},
		{"esc", tea.KeyPressMsg{Code: tea.KeyEscape}},
	} {
		t.Run("prompt declined by "+tc.name+" leaves the pane alive", func(t *testing.T) {
			kills := &killRecorder{}
			d := newMonitorDashboard(threePanes(), kills.callbacks())
			d.cursor = 1

			m, _ := d.Update(ctrlX())
			d = m.(*MonitorDashboard)
			m, cmd := d.Update(tc.key)
			d = m.(*MonitorDashboard)

			if d.killPrompt != nil {
				t.Error("prompt still open after cancel")
			}
			if len(kills.killed) != 0 {
				t.Errorf("killed %v after cancel", kills.killed)
			}
			if len(d.panes) != 3 {
				t.Errorf("panes len = %d, want 3", len(d.panes))
			}
			if cmd != nil {
				t.Error("cancelling the prompt should not quit the dashboard")
			}
		})
	}

	t.Run("unknown key is ignored", func(t *testing.T) {
		kills := &killRecorder{}
		d := newMonitorDashboard(threePanes(), kills.callbacks())
		m, _ := d.Update(ctrlX())
		d = m.(*MonitorDashboard)
		m, _ = d.Update(tea.KeyPressMsg{Code: 'z', Text: "z"})
		d = m.(*MonitorDashboard)
		if d.killPrompt == nil {
			t.Error("an unrelated key closed the prompt")
		}
		if len(kills.killed) != 0 {
			t.Errorf("killed %v on an unrelated key", kills.killed)
		}
	})

	t.Run("prompt disabled kills without asking", func(t *testing.T) {
		kills := &killRecorder{}
		d := newMonitorDashboard(threePanes(), kills.callbacks(), WithMonitorDashboardKillPrompt(false))
		d.cursor = 0

		m, _ := d.Update(ctrlX())
		d = m.(*MonitorDashboard)
		if d.killPrompt != nil {
			t.Error("prompt opened although the setting is off")
		}
		if len(kills.killed) != 1 || kills.killed[0] != "%1" {
			t.Fatalf("killed = %v, want [%%1]", kills.killed)
		}
		if len(d.panes) != 2 {
			t.Errorf("panes len = %d, want 2", len(d.panes))
		}
		if got := d.flash.Text(); got != "killed one" {
			t.Errorf("flash = %q, want %q", got, "killed one")
		}
	})
}

func TestDashboardKillPaneRefusals(t *testing.T) {
	t.Run("refuses the pane the dashboard runs in", func(t *testing.T) {
		for _, promptEnabled := range []bool{true, false} {
			kills := &killRecorder{}
			opts := []MonitorDashboardOption{WithMonitorDashboardCurrentPaneID("%2")}
			if !promptEnabled {
				opts = append(opts, WithMonitorDashboardKillPrompt(false))
			}
			d := newMonitorDashboard(threePanes(), kills.callbacks(), opts...)
			d.cursor = 1

			m, _ := d.Update(ctrlX())
			d = m.(*MonitorDashboard)
			if len(kills.killed) != 0 {
				t.Errorf("prompt=%v: killed %v, want the current pane refused", promptEnabled, kills.killed)
			}
			if d.killPrompt != nil {
				t.Errorf("prompt=%v: opened a prompt for the current pane", promptEnabled)
			}
			if got := d.flash.Text(); !containsSubstring(got, "cannot kill the pane the dashboard is running in") {
				t.Errorf("prompt=%v: flash = %q, want the refusal reason", promptEnabled, got)
			}
			if len(d.panes) != 3 {
				t.Errorf("prompt=%v: panes len = %d, want 3", promptEnabled, len(d.panes))
			}
		}
	})

	t.Run("does nothing in picker mode", func(t *testing.T) {
		kills := &killRecorder{}
		d := newMonitorDashboard(threePanes(), kills.callbacks(), WithMonitorDashboardPickerMode("alt"))
		d.cursor = 1

		m, _ := d.Update(ctrlX())
		d = m.(*MonitorDashboard)
		if len(kills.killed) != 0 {
			t.Errorf("killed %v in picker mode", kills.killed)
		}
		if d.killPrompt != nil {
			t.Error("picker mode opened a kill prompt")
		}
		if len(d.panes) != 3 || d.flash.Text() != "" {
			t.Errorf("picker mode was not inert: panes=%d flash=%q", len(d.panes), d.flash.Text())
		}
	})

	t.Run("failed kill keeps the row and reports it", func(t *testing.T) {
		kills := &killRecorder{err: errors.New("no such pane")}
		d := newMonitorDashboard(threePanes(), kills.callbacks(), WithMonitorDashboardKillPrompt(false))
		d.cursor = 2

		m, _ := d.Update(ctrlX())
		d = m.(*MonitorDashboard)
		if len(d.panes) != 3 || len(d.allPanes) != 3 {
			t.Errorf("panes=%d allPanes=%d, want the row left in place", len(d.panes), len(d.allPanes))
		}
		if got := d.flash.Text(); !containsSubstring(got, "no such pane") {
			t.Errorf("flash = %q, want the kill error", got)
		}
	})
}

func TestDashboardKillPaneEmptiesList(t *testing.T) {
	kills := &killRecorder{}
	d := newMonitorDashboard([]AttentionPane{{PaneID: "%1", Session: "s1", Name: "one"}}, kills.callbacks())

	m, _ := d.Update(ctrlX())
	d = m.(*MonitorDashboard)
	m, cmd := d.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	d = m.(*MonitorDashboard)

	if cmd == nil {
		t.Error("expected a quit cmd once the kill emptied the list")
	}
	if d.result.Action != MonitorDashboardActionCancel {
		t.Errorf("action = %d, want MonitorDashboardActionCancel", d.result.Action)
	}
}

func TestDashboardKillPaneIsAdvertised(t *testing.T) {
	d := newMonitorDashboard(threePanes(), AttentionCallbacks{})
	if hints := d.buildHints(); !containsSubstring(hints, "C-x kill") {
		t.Errorf("hints = %q, want the kill key listed", hints)
	}
	d.showHelp = true
	if view := d.View().Content; !containsSubstring(view, "Kill pane") {
		t.Error("help overlay missing the kill entry")
	}

	picker := newMonitorDashboard(threePanes(), AttentionCallbacks{}, WithMonitorDashboardPickerMode("alt"))
	if hints := picker.buildHints(); containsSubstring(hints, "kill") {
		t.Errorf("picker hints = %q, should not offer a kill", hints)
	}
}
