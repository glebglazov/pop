package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func jumpPanes() []AttentionPane {
	return []AttentionPane{
		{PaneID: "%1", Session: "s1"},
		{PaneID: "%2", Session: "s2"},
		{PaneID: "%3", Session: "s3"},
		{PaneID: "%4", Session: "s4"},
	}
}

func pressG(d *MonitorDashboard) *MonitorDashboard {
	m, _ := d.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	return m.(*MonitorDashboard)
}

// TestMonitorDashboardGGAndG pins the list vocabulary the Work dashboard already
// speaks: gg lands on the first pane, G on the last, and a lone g waits rather
// than jumping.
func TestMonitorDashboardGGAndG(t *testing.T) {
	t.Run("G jumps to the last pane", func(t *testing.T) {
		d := newMonitorDashboard(jumpPanes(), AttentionCallbacks{})
		m, _ := d.Update(tea.KeyPressMsg{Code: 'G', Text: "G"})
		d = m.(*MonitorDashboard)
		if d.cursor != 3 {
			t.Fatalf("cursor = %d, want 3", d.cursor)
		}
	})

	t.Run("gg jumps to the first pane", func(t *testing.T) {
		d := newMonitorDashboard(jumpPanes(), AttentionCallbacks{})
		d.cursor = 3
		d = pressG(d)
		if d.cursor != 3 {
			t.Fatalf("a lone g moved the cursor to %d, want 3", d.cursor)
		}
		d = pressG(d)
		if d.cursor != 0 {
			t.Fatalf("cursor after gg = %d, want 0", d.cursor)
		}
	})

	t.Run("another key cancels a half-typed gg", func(t *testing.T) {
		d := newMonitorDashboard(jumpPanes(), AttentionCallbacks{})
		d.cursor = 3
		d = pressG(d)
		m, _ := d.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
		d = m.(*MonitorDashboard)
		d = pressG(d)
		if d.cursor != 2 {
			t.Fatalf("cursor = %d, want 2 — the g after k must arm, not jump", d.cursor)
		}
	})

	t.Run("empty list stays put", func(t *testing.T) {
		d := newMonitorDashboard(nil, AttentionCallbacks{})
		m, _ := d.Update(tea.KeyPressMsg{Code: 'G', Text: "G"})
		d = m.(*MonitorDashboard)
		if d.cursor != 0 {
			t.Fatalf("cursor = %d, want 0", d.cursor)
		}
	})
}

// TestMonitorDashboardHelpDocumentsJumps keeps the C-h overlay the complete
// listing (ADR-0204) in both modes.
func TestMonitorDashboardHelpDocumentsJumps(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []MonitorDashboardOption
	}{
		{"normal", nil},
		{"picker", []MonitorDashboardOption{WithMonitorDashboardPickerMode("alt")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newMonitorDashboard(jumpPanes(), AttentionCallbacks{}, tc.opts...)
			d.showHelp = true
			if view := d.View().Content; !containsSubstring(view, "gg / G") {
				t.Fatalf("%s help view does not document gg/G:\n%s", tc.name, view)
			}
		})
	}
}
