package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// newDashboard creates a dashboard ready for testing with sensible defaults.
func newDashboard(panes []AttentionPane, cb AttentionCallbacks, opts ...DashboardOption) *Dashboard {
	// Deep copy to prevent shared-slice mutations from leaking across subtests.
	copied := make([]AttentionPane, len(panes))
	copy(copied, panes)
	d := NewDashboard(copied, cb, nil, opts...)
	d.width = 80
	d.height = 20
	d.Init()
	return d
}

func TestDashboardInit(t *testing.T) {
	t.Run("cursor at last pane by default", func(t *testing.T) {
		panes := []AttentionPane{
			{PaneID: "%1", Session: "s1"},
			{PaneID: "%2", Session: "s2"},
			{PaneID: "%3", Session: "s3"},
		}
		d := newDashboard(panes, AttentionCallbacks{})
		if d.cursor != 2 {
			t.Errorf("cursor = %d, want 2", d.cursor)
		}
	})

	t.Run("cursor respects initialPaneID", func(t *testing.T) {
		panes := []AttentionPane{
			{PaneID: "%1", Session: "s1"},
			{PaneID: "%2", Session: "s2"},
		}
		d := newDashboard(panes, AttentionCallbacks{}, WithInitialPaneID("%1"))
		if d.cursor != 0 {
			t.Errorf("cursor = %d, want 0", d.cursor)
		}
	})

	t.Run("spinner starts when working panes present", func(t *testing.T) {
		panes := []AttentionPane{
			{PaneID: "%1", Session: "s1", Status: AttentionWorking},
		}
		cmd := NewDashboard(panes, AttentionCallbacks{}, nil).Init()
		if cmd == nil {
			t.Error("expected non-nil cmd for spinner tick")
		}
	})

	t.Run("reload tick starts when reloadFunc present", func(t *testing.T) {
		panes := []AttentionPane{
			{PaneID: "%1", Session: "s1"},
		}
		d := NewDashboard(panes, AttentionCallbacks{}, func() []AttentionPane { return nil })
		cmd := d.Init()
		if cmd == nil {
			t.Error("expected non-nil cmd for reload tick")
		}
	})
}

func TestDashboardNavigation(t *testing.T) {
	panes := []AttentionPane{
		{PaneID: "%1", Session: "s1"},
		{PaneID: "%2", Session: "s2"},
		{PaneID: "%3", Session: "s3"},
	}

	t.Run("up moves cursor up", func(t *testing.T) {
		d := newDashboard(panes, AttentionCallbacks{})
		d.cursor = 2
		m, _ := d.Update(tea.KeyPressMsg{Code: tea.KeyUp})
		d = m.(*Dashboard)
		if d.cursor != 1 {
			t.Errorf("cursor = %d, want 1", d.cursor)
		}
	})

	t.Run("down moves cursor down", func(t *testing.T) {
		d := newDashboard(panes, AttentionCallbacks{})
		d.cursor = 0
		m, _ := d.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		d = m.(*Dashboard)
		if d.cursor != 1 {
			t.Errorf("cursor = %d, want 1", d.cursor)
		}
	})

	t.Run("up wraps to bottom", func(t *testing.T) {
		d := newDashboard(panes, AttentionCallbacks{})
		d.cursor = 0
		m, _ := d.Update(tea.KeyPressMsg{Code: tea.KeyUp})
		d = m.(*Dashboard)
		if d.cursor != 2 {
			t.Errorf("cursor = %d, want 2", d.cursor)
		}
	})

	t.Run("down wraps to top", func(t *testing.T) {
		d := newDashboard(panes, AttentionCallbacks{})
		d.cursor = 2
		m, _ := d.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		d = m.(*Dashboard)
		if d.cursor != 0 {
			t.Errorf("cursor = %d, want 0", d.cursor)
		}
	})

	t.Run("k moves up", func(t *testing.T) {
		d := newDashboard(panes, AttentionCallbacks{})
		d.cursor = 1
		m, _ := d.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
		d = m.(*Dashboard)
		if d.cursor != 0 {
			t.Errorf("cursor = %d, want 0", d.cursor)
		}
	})

	t.Run("j moves down", func(t *testing.T) {
		d := newDashboard(panes, AttentionCallbacks{})
		d.cursor = 0
		m, _ := d.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
		d = m.(*Dashboard)
		if d.cursor != 1 {
			t.Errorf("cursor = %d, want 1", d.cursor)
		}
	})

	t.Run("h acts as back", func(t *testing.T) {
		d := newDashboard(panes, AttentionCallbacks{})
		m, cmd := d.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
		d = m.(*Dashboard)
		if cmd == nil {
			t.Error("expected quit cmd from back")
		}
	})
}

func TestDashboardEnter(t *testing.T) {
	t.Run("enter confirms selection", func(t *testing.T) {
		panes := []AttentionPane{
			{PaneID: "%1", Session: "s1"},
			{PaneID: "%2", Session: "s2"},
		}
		d := newDashboard(panes, AttentionCallbacks{})
		d.cursor = 1
		m, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		d = m.(*Dashboard)
		if d.result.Action != DashboardActionConfirm {
			t.Errorf("action = %d, want DashboardActionConfirm", d.result.Action)
		}
		if d.result.Selected == nil || d.result.Selected.PaneID != "%2" {
			t.Errorf("selected = %v, want pane %%2", d.result.Selected)
		}
		if cmd == nil {
			t.Error("expected quit cmd")
		}
	})

	t.Run("enter with no panes cancels", func(t *testing.T) {
		d := newDashboard(nil, AttentionCallbacks{})
		m, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		d = m.(*Dashboard)
		if d.result.Action != DashboardActionCancel {
			t.Errorf("action = %d, want DashboardActionCancel", d.result.Action)
		}
		if cmd == nil {
			t.Error("expected quit cmd")
		}
	})
}

func TestDashboardPickerMode(t *testing.T) {
	panes := []AttentionPane{
		{PaneID: "%1", Session: "s1", Status: AttentionUnread},
		{PaneID: "%2", Session: "s1", Status: AttentionClear},
		{PaneID: "%3", Session: "s1", Status: AttentionWorking},
	}

	t.Run("quick access confirms relative pane", func(t *testing.T) {
		d := newDashboard(panes, AttentionCallbacks{}, WithDashboardPickerMode("alt"))
		d.cursor = 2
		m, cmd := d.Update(tea.KeyPressMsg{Code: '2', Text: "2", Mod: tea.ModAlt})
		d = m.(*Dashboard)
		if d.result.Action != DashboardActionConfirm {
			t.Errorf("action = %d, want DashboardActionConfirm", d.result.Action)
		}
		if d.result.Selected == nil || d.result.Selected.PaneID != "%1" {
			t.Errorf("selected = %v, want pane %%1", d.result.Selected)
		}
		if cmd == nil {
			t.Error("expected quit cmd")
		}
	})

	t.Run("mutation keys are disabled", func(t *testing.T) {
		called := false
		cb := AttentionCallbacks{
			MarkClear:    func(string) { called = true },
			MarkUnread:   func(string) { called = true },
			ToggleFollow: func(string) { called = true },
			Unmonitor:    func(string) { called = true },
			SetNote:      func(string, string) { called = true },
		}
		d := newDashboard(panes, cb, WithDashboardPickerMode("alt"))
		d.cursor = 0
		for _, msg := range []tea.KeyPressMsg{
			{Code: 'r', Text: "r"},
			{Code: 'a', Text: "a", Mod: tea.ModCtrl},
			{Code: 'f', Text: "f"},
			{Code: 'x', Text: "x"},
			{Code: 'N', Text: "N"},
			{Code: 'p', Text: "p"},
		} {
			m, _ := d.Update(msg)
			d = m.(*Dashboard)
		}
		if called {
			t.Error("picker mode should not call mutation callbacks")
		}
		if d.dirty {
			t.Error("picker mode mutation keys should not mark dashboard dirty")
		}
		if d.result.Action != DashboardActionCancel {
			t.Errorf("action = %d, want no action", d.result.Action)
		}
	})
}

func TestDashboardPeek(t *testing.T) {
	t.Run("peek opens without clearing", func(t *testing.T) {
		panes := []AttentionPane{
			{PaneID: "%1", Session: "s1"},
			{PaneID: "%2", Session: "s2"},
		}
		d := newDashboard(panes, AttentionCallbacks{})
		d.cursor = 1
		m, cmd := d.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
		d = m.(*Dashboard)
		if d.result.Action != DashboardActionPeek {
			t.Errorf("action = %d, want DashboardActionPeek", d.result.Action)
		}
		if d.result.Selected == nil || d.result.Selected.PaneID != "%2" {
			t.Errorf("selected = %v, want pane %%2", d.result.Selected)
		}
		if cmd == nil {
			t.Error("expected quit cmd")
		}
	})

	t.Run("peek with no panes is no-op", func(t *testing.T) {
		d := newDashboard(nil, AttentionCallbacks{})
		m, _ := d.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
		d = m.(*Dashboard)
		if d.result.Action == DashboardActionPeek {
			t.Error("peek with no panes should not produce DashboardActionPeek")
		}
	})
}

func TestDashboardQuitAndBack(t *testing.T) {
	panes := []AttentionPane{
		{PaneID: "%1", Session: "s1"},
	}

	t.Run("esc when clean quits", func(t *testing.T) {
		d := newDashboard(panes, AttentionCallbacks{})
		m, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		d = m.(*Dashboard)
		if cmd == nil {
			t.Error("expected quit cmd")
		}
	})

	t.Run("esc when dirty returns refresh", func(t *testing.T) {
		d := newDashboard(panes, AttentionCallbacks{})
		d.dirty = true
		m, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		d = m.(*Dashboard)
		if d.result.Action != DashboardActionRefresh {
			t.Errorf("action = %d, want DashboardActionRefresh", d.result.Action)
		}
		if cmd == nil {
			t.Error("expected quit cmd")
		}
	})

	t.Run("ctrl+c cancels", func(t *testing.T) {
		d := newDashboard(panes, AttentionCallbacks{})
		m, cmd := d.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
		d = m.(*Dashboard)
		if d.result.Action != DashboardActionCancel {
			t.Errorf("action = %d, want DashboardActionCancel", d.result.Action)
		}
		if cmd == nil {
			t.Error("expected quit cmd")
		}
	})

	t.Run("back when clean quits", func(t *testing.T) {
		d := newDashboard(panes, AttentionCallbacks{})
		m, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
		d = m.(*Dashboard)
		if cmd == nil {
			t.Error("expected quit cmd")
		}
	})

	t.Run("back when dirty returns refresh", func(t *testing.T) {
		d := newDashboard(panes, AttentionCallbacks{})
		d.dirty = true
		m, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
		d = m.(*Dashboard)
		if d.result.Action != DashboardActionRefresh {
			t.Errorf("action = %d, want DashboardActionRefresh", d.result.Action)
		}
		if cmd == nil {
			t.Error("expected quit cmd")
		}
	})
}

func TestDashboardToggleClearUnread(t *testing.T) {
	panes := []AttentionPane{
		{PaneID: "%1", Session: "s1", Status: AttentionUnread},
		{PaneID: "%2", Session: "s2", Status: AttentionWorking},
		{PaneID: "%3", Session: "s3", Status: AttentionClear},
	}
	cb := AttentionCallbacks{
		MarkClear:  func(paneID string) {},
		MarkUnread: func(paneID string) {},
	}

	t.Run("r on unread flips to clear", func(t *testing.T) {
		d := newDashboard(panes, cb)
		d.cursor = 0 // %1 unread
		m, _ := d.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
		d = m.(*Dashboard)
		if d.panes[0].Status != AttentionClear {
			t.Errorf("status = %d, want AttentionClear", d.panes[0].Status)
		}
		if !d.dirty {
			t.Error("expected dirty = true")
		}
	})

	t.Run("r on clear flips to unread", func(t *testing.T) {
		d := newDashboard(panes, cb)
		d.cursor = 2 // %3 clear
		m, _ := d.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
		d = m.(*Dashboard)
		var found bool
		for _, p := range d.panes {
			if p.PaneID == "%3" && p.Status == AttentionUnread {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("pane %%3 not found with status AttentionUnread after toggle")
		}
		if !d.dirty {
			t.Error("expected dirty = true")
		}
	})

	t.Run("r keeps mutated pane under cursor after status sort", func(t *testing.T) {
		d := newDashboard(panes, cb)
		d.cursor = 0 // %1 unread moves from the unread group to the clear group
		m, _ := d.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
		d = m.(*Dashboard)
		if d.cursor != 0 {
			t.Errorf("cursor = %d, want 0", d.cursor)
		}
		if d.panes[d.cursor].PaneID != "%1" {
			t.Errorf("cursor on %s, want %%1", d.panes[d.cursor].PaneID)
		}
		m, _ = d.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		d = m.(*Dashboard)
		if d.protectedPaneID != "" {
			t.Errorf("protectedPaneID = %q, want empty after navigation", d.protectedPaneID)
		}
	})

	t.Run("r on virtual is no-op", func(t *testing.T) {
		virtualPanes := []AttentionPane{
			{PaneID: "%1", Session: "s1", Status: AttentionVirtual},
		}
		d := newDashboard(virtualPanes, cb)
		m, _ := d.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
		d = m.(*Dashboard)
		if d.panes[0].Status != AttentionVirtual {
			t.Errorf("status = %d, want AttentionVirtual", d.panes[0].Status)
		}
		if d.dirty {
			t.Error("expected dirty = false")
		}
	})
}

func TestDashboardMarkUnread(t *testing.T) {
	panes := []AttentionPane{
		{PaneID: "%1", Session: "s1", Status: AttentionClear},
	}
	cb := AttentionCallbacks{
		MarkUnread: func(paneID string) {},
	}

	t.Run("ctrl+a marks clear as unread", func(t *testing.T) {
		d := newDashboard(panes, cb)
		m, _ := d.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
		d = m.(*Dashboard)
		if d.panes[0].Status != AttentionUnread {
			t.Errorf("status = %d, want AttentionUnread", d.panes[0].Status)
		}
		if !d.dirty {
			t.Error("expected dirty = true")
		}
	})

	t.Run("ctrl+a on virtual is no-op", func(t *testing.T) {
		virtualPanes := []AttentionPane{
			{PaneID: "%1", Session: "s1", Status: AttentionVirtual},
		}
		d := newDashboard(virtualPanes, cb)
		m, _ := d.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
		d = m.(*Dashboard)
		if d.panes[0].Status != AttentionVirtual {
			t.Errorf("status = %d, want AttentionVirtual", d.panes[0].Status)
		}
	})
}

func TestDashboardFollowPane(t *testing.T) {
	panes := []AttentionPane{
		{PaneID: "%1", Session: "s1", Following: false},
	}
	cb := AttentionCallbacks{
		ToggleFollow: func(paneID string) {},
		SetNote:      func(paneID, note string) {},
	}

	t.Run("f toggles follow on", func(t *testing.T) {
		d := newDashboard(panes, cb)
		m, _ := d.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
		d = m.(*Dashboard)
		if !d.panes[0].Following {
			t.Error("expected following = true")
		}
		if !d.dirty {
			t.Error("expected dirty = true")
		}
	})

	t.Run("f on virtual is no-op", func(t *testing.T) {
		virtualPanes := []AttentionPane{
			{PaneID: "%1", Session: "s1", Status: AttentionVirtual},
		}
		d := newDashboard(virtualPanes, cb)
		m, _ := d.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
		d = m.(*Dashboard)
		if d.dirty {
			t.Error("expected dirty = false")
		}
	})

	t.Run("unfollow removes from follow view", func(t *testing.T) {
		followPanes := []AttentionPane{
			{PaneID: "%1", Session: "s1", Following: true},
			{PaneID: "%2", Session: "s2", Following: true},
		}
		d := newDashboard(followPanes, cb, WithFollowing(true))
		d.cursor = 0
		m, _ := d.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
		d = m.(*Dashboard)
		if len(d.panes) != 1 {
			t.Errorf("panes len = %d, want 1", len(d.panes))
		}
	})

	t.Run("follow clears note", func(t *testing.T) {
		notePanes := []AttentionPane{
			{PaneID: "%1", Session: "s1", Following: true, Note: "keep me"},
		}
		noteCb := AttentionCallbacks{
			ToggleFollow: func(paneID string) {},
			SetNote:      func(paneID, note string) {},
		}
		d := newDashboard(notePanes, noteCb)
		m, _ := d.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
		d = m.(*Dashboard)
		if d.panes[0].Note != "" {
			t.Errorf("note = %q, want empty", d.panes[0].Note)
		}
	})
}

func TestDashboardToggleFollowView(t *testing.T) {
	panes := []AttentionPane{
		{PaneID: "%1", Session: "s1", Following: true},
		{PaneID: "%2", Session: "s2", Following: false},
	}
	cb := AttentionCallbacks{}

	t.Run("F enters follow view", func(t *testing.T) {
		d := newDashboard(panes, cb)
		m, _ := d.Update(tea.KeyPressMsg{Code: 'F', Text: "F"})
		d = m.(*Dashboard)
		if !d.following {
			t.Error("expected following = true")
		}
		if len(d.panes) != 1 {
			t.Errorf("panes len = %d, want 1", len(d.panes))
		}
	})

	t.Run("F exits follow view", func(t *testing.T) {
		d := newDashboard(panes, cb, WithFollowing(true))
		m, _ := d.Update(tea.KeyPressMsg{Code: 'F', Text: "F"})
		d = m.(*Dashboard)
		if d.following {
			t.Error("expected following = false")
		}
		if len(d.panes) != 2 {
			t.Errorf("panes len = %d, want 2", len(d.panes))
		}
	})
}

func TestDashboardUnmonitor(t *testing.T) {
	cb := AttentionCallbacks{
		Unmonitor:  func(paneID string) {},
		MarkClear:  func(paneID string) {},
		MarkUnread: func(paneID string) {},
	}

	t.Run("x removes pane", func(t *testing.T) {
		panes := []AttentionPane{
			{PaneID: "%1", Session: "s1"},
			{PaneID: "%2", Session: "s2"},
			{PaneID: "%3", Session: "s3"},
		}
		d := newDashboard(panes, cb)
		d.cursor = 1
		m, _ := d.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
		d = m.(*Dashboard)
		if len(d.panes) != 2 {
			t.Errorf("panes len = %d, want 2", len(d.panes))
		}
		if len(d.allPanes) != 2 {
			t.Errorf("allPanes len = %d, want 2", len(d.allPanes))
		}
		if !d.dirty {
			t.Error("expected dirty = true")
		}
	})

	t.Run("x on virtual is no-op", func(t *testing.T) {
		virtualPanes := []AttentionPane{
			{PaneID: "%1", Session: "s1", Status: AttentionVirtual},
		}
		d := newDashboard(virtualPanes, cb)
		m, _ := d.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
		d = m.(*Dashboard)
		if d.dirty {
			t.Error("expected dirty = false")
		}
	})

	t.Run("unmonitor last pane cancels", func(t *testing.T) {
		single := []AttentionPane{{PaneID: "%1", Session: "s1"}}
		d := newDashboard(single, cb)
		m, cmd := d.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
		d = m.(*Dashboard)
		if d.result.Action != DashboardActionCancel {
			t.Errorf("action = %d, want DashboardActionCancel", d.result.Action)
		}
		if cmd == nil {
			t.Error("expected quit cmd")
		}
	})
}

func TestDashboardEditNote(t *testing.T) {
	panes := []AttentionPane{
		{PaneID: "%1", Session: "s1"},
		{PaneID: "%2", Session: "s2"},
	}
	cb := AttentionCallbacks{
		SetNote:      func(paneID, note string) {},
		ToggleFollow: func(paneID string) {},
	}

	t.Run("N enters note editing", func(t *testing.T) {
		d := newDashboard(panes, cb)
		m, _ := d.Update(tea.KeyPressMsg{Code: 'N', Text: "N"})
		d = m.(*Dashboard)
		if !d.editingNote {
			t.Error("expected editingNote = true")
		}
	})

	t.Run("type and save note", func(t *testing.T) {
		d := newDashboard(panes, cb)
		d.cursor = 0 // ensure we're editing pane %1
		// Enter edit mode
		m, _ := d.Update(tea.KeyPressMsg{Code: 'N', Text: "N"})
		d = m.(*Dashboard)
		// Type "hello"
		for _, ch := range "hello" {
			m, _ = d.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)})
			d = m.(*Dashboard)
		}
		// Save with Enter
		m, _ = d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		d = m.(*Dashboard)
		if d.panes[0].Note != "hello" {
			t.Errorf("note = %q, want hello", d.panes[0].Note)
		}
		if d.editingNote {
			t.Error("expected editingNote = false after save")
		}
		if !d.dirty {
			t.Error("expected dirty = true")
		}
	})

	t.Run("esc cancels note editing", func(t *testing.T) {
		d := newDashboard(panes, cb)
		m, _ := d.Update(tea.KeyPressMsg{Code: 'N', Text: "N"})
		d = m.(*Dashboard)
		m, _ = d.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
		d = m.(*Dashboard)
		m, _ = d.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		d = m.(*Dashboard)
		if d.editingNote {
			t.Error("expected editingNote = false after esc")
		}
		if d.panes[0].Note != "" {
			t.Errorf("note = %q, want empty (cancel should not save)", d.panes[0].Note)
		}
	})

	t.Run("N on virtual is no-op", func(t *testing.T) {
		virtualPanes := []AttentionPane{
			{PaneID: "%1", Session: "s1", Status: AttentionVirtual},
		}
		d := newDashboard(virtualPanes, cb)
		m, _ := d.Update(tea.KeyPressMsg{Code: 'N', Text: "N"})
		d = m.(*Dashboard)
		if d.editingNote {
			t.Error("expected editingNote = false")
		}
	})
}

func TestDashboardHelpOverlay(t *testing.T) {
	panes := []AttentionPane{{PaneID: "%1", Session: "s1"}}
	d := newDashboard(panes, AttentionCallbacks{})

	t.Run("f1 shows help", func(t *testing.T) {
		m, _ := d.Update(tea.KeyPressMsg{Code: tea.KeyF1})
		d = m.(*Dashboard)
		if !d.showHelp {
			t.Error("expected showHelp = true")
		}
	})

	t.Run("esc dismisses help", func(t *testing.T) {
		d.showHelp = true
		m, _ := d.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		d = m.(*Dashboard)
		if d.showHelp {
			t.Error("expected showHelp = false")
		}
	})

	t.Run("other keys swallowed in help", func(t *testing.T) {
		d.showHelp = true
		m, _ := d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		d = m.(*Dashboard)
		if d.result.Action != 0 {
			t.Errorf("expected no action in help, got %d", d.result.Action)
		}
		if !d.showHelp {
			t.Error("expected showHelp still true")
		}
	})
}

func TestDashboardReload(t *testing.T) {
	cb := AttentionCallbacks{}

	t.Run("preserves cursor on same pane", func(t *testing.T) {
		panes := []AttentionPane{
			{PaneID: "%1", Session: "s1"},
			{PaneID: "%2", Session: "s2"},
		}
		d := newDashboard(panes, cb)
		d.cursor = 1 // on %2
		d.reloadFunc = func() []AttentionPane {
			return []AttentionPane{
				{PaneID: "%1", Session: "s1"},
				{PaneID: "%2", Session: "s2"},
				{PaneID: "%3", Session: "s3"},
			}
		}
		m, _ := d.Update(reloadTickMsg{})
		d = m.(*Dashboard)
		if d.panes[d.cursor].PaneID != "%2" {
			t.Errorf("cursor on %s, want %%2", d.panes[d.cursor].PaneID)
		}
	})

	t.Run("moves cursor to bottom when pane removed", func(t *testing.T) {
		panes := []AttentionPane{
			{PaneID: "%1", Session: "s1"},
			{PaneID: "%2", Session: "s2"},
		}
		d := newDashboard(panes, cb)
		d.cursor = 1 // on %2
		d.reloadFunc = func() []AttentionPane {
			return []AttentionPane{{PaneID: "%1", Session: "s1"}}
		}
		m, _ := d.Update(reloadTickMsg{})
		d = m.(*Dashboard)
		if d.cursor != 0 {
			t.Errorf("cursor = %d, want 0", d.cursor)
		}
	})

	t.Run("handles empty reload result", func(t *testing.T) {
		panes := []AttentionPane{
			{PaneID: "%1", Session: "s1"},
		}
		d := newDashboard(panes, cb)
		d.reloadFunc = func() []AttentionPane { return nil }
		m, _ := d.Update(reloadTickMsg{})
		d = m.(*Dashboard)
		if d.cursor != 0 {
			t.Errorf("cursor = %d, want 0", d.cursor)
		}
		if len(d.panes) != 0 {
			t.Errorf("panes len = %d, want 0", len(d.panes))
		}
	})

	t.Run("keeps mutated row anchored across reload with updated last_active_at order", func(t *testing.T) {
		panes := []AttentionPane{
			{PaneID: "%1", Session: "s1"},
			{PaneID: "%2", Session: "s2"},
			{PaneID: "%3", Session: "s3"},
		}
		d := newDashboard(panes, AttentionCallbacks{
			MarkUnread: func(paneID string) {},
		})
		d.cursor = 1 // protect %2 at the middle row
		m, _ := d.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
		d = m.(*Dashboard)
		d.reloadFunc = func() []AttentionPane {
			// The reload function supplies normal sorted order after updated
			// last_active_at values move the surrounding panes.
			return []AttentionPane{
				{PaneID: "%3", Session: "s3"},
				{PaneID: "%1", Session: "s1"},
				{PaneID: "%2", Session: "s2", Status: AttentionUnread},
			}
		}
		m, _ = d.Update(reloadTickMsg{})
		d = m.(*Dashboard)
		want := []string{"%3", "%2", "%1"}
		for i, pane := range d.panes {
			if pane.PaneID != want[i] {
				t.Errorf("panes[%d] = %s, want %s", i, pane.PaneID, want[i])
			}
		}
		if d.cursor != 1 || d.panes[d.cursor].PaneID != "%2" {
			t.Errorf("cursor = %d on %s, want 1 on %%2", d.cursor, d.panes[d.cursor].PaneID)
		}
	})
}

func TestDashboardView(t *testing.T) {
	t.Run("renders non-empty dashboard", func(t *testing.T) {
		panes := []AttentionPane{
			{PaneID: "%1", Session: "s1", Name: "alpha"},
			{PaneID: "%2", Session: "s2", Name: "beta"},
		}
		d := newDashboard(panes, AttentionCallbacks{})
		view := d.View().Content
		if view == "" {
			t.Error("view returned empty string")
		}
		if !containsSubstring(view, "alpha") {
			t.Error("view missing pane name")
		}
	})

	t.Run("renders empty dashboard", func(t *testing.T) {
		d := newDashboard(nil, AttentionCallbacks{})
		view := d.View().Content
		if view == "" {
			t.Error("view returned empty string")
		}
		if !containsSubstring(view, "No active panes") {
			t.Error("view missing empty message")
		}
	})

	t.Run("topic-derived name renders dimmed", func(t *testing.T) {
		const dimSeq = "38;5;241" // colorDim foreground

		plain := []AttentionPane{
			{PaneID: "%1", Session: "s1", Name: "s1 (node)", Status: AttentionClear},
		}
		topic := []AttentionPane{
			{PaneID: "%1", Session: "s1", Name: "s1 (refactor auth)", Status: AttentionClear, TopicDerived: true},
		}

		plainView := newDashboard(plain, AttentionCallbacks{}).View().Content
		topicView := newDashboard(topic, AttentionCallbacks{}).View().Content

		// Both share the dimmed hint line, so compare counts: the topic name is
		// dimmed in both the list row and the right header, adding occurrences.
		if strings.Count(topicView, dimSeq) <= strings.Count(plainView, dimSeq) {
			t.Errorf("expected more dim sequences for topic-derived name: topic=%d plain=%d",
				strings.Count(topicView, dimSeq), strings.Count(plainView, dimSeq))
		}
		// The name text itself is preserved (only styled, not altered).
		if !containsSubstring(StripANSI(topicView), "refactor auth") {
			t.Error("topic name text missing from view")
		}
	})

	t.Run("virtual pane renders circle icon", func(t *testing.T) {
		panes := []AttentionPane{
			{PaneID: "%1", Session: "s1", Name: "virtual", Status: AttentionVirtual},
			{PaneID: "%2", Session: "s2", Name: "idle", Status: AttentionClear},
		}
		d := newDashboard(panes, AttentionCallbacks{})
		view := d.View().Content
		if !containsSubstring(view, "○") {
			t.Error("expected ○ icon for virtual pane")
		}
	})
}
