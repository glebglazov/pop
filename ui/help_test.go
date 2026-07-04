package ui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

func TestHelpKeysMatchesCtrlH(t *testing.T) {
	msg := tea.KeyPressMsg{Code: 'h', Mod: tea.ModCtrl}
	if !key.Matches(msg, HelpKeys) {
		t.Error("HelpKeys should match ctrl+h")
	}

	other := tea.KeyPressMsg{Code: 'h', Mod: tea.ModAlt}
	if key.Matches(other, HelpKeys) {
		t.Error("HelpKeys should not match alt+h")
	}
}

func TestRenderHelpOverlay(t *testing.T) {
	entries := []HelpEntry{
		{"C-a", "Create worktree"},
		{"C-d", "Delete"},
	}
	view := RenderHelpOverlay("Help", entries, 60, 10)

	if !containsSubstring(view, "Help") {
		t.Error("overlay should render title in input box")
	}
	if !containsSubstring(view, "Create worktree") {
		t.Error("overlay should render entry description")
	}
	if !containsSubstring(view, "C-a") {
		t.Error("overlay should render entry key")
	}
	if !containsSubstring(view, "C-h toggle") {
		t.Error("overlay should render C-h toggle footer")
	}
	if !containsSubstring(view, "Esc close") {
		t.Error("overlay should render Esc close footer")
	}

	// Columns should be aligned: both keys padded to the same width.
	lines := strings.Split(view, "\n")
	var keyLines []string
	for _, line := range lines {
		if strings.Contains(line, "Create worktree") || strings.Contains(line, "Delete") {
			keyLines = append(keyLines, line)
		}
	}
	if len(keyLines) != 2 {
		t.Fatalf("expected 2 key lines, got %d", len(keyLines))
	}
	idx1 := strings.Index(keyLines[0], "Create worktree")
	idx2 := strings.Index(keyLines[1], "Delete")
	if idx1 <= 0 || idx2 <= 0 || idx1 != idx2 {
		t.Errorf("descriptions are not aligned: %d vs %d", idx1, idx2)
	}
}

func TestToggleHelp(t *testing.T) {
	t.Run("opens help on C-h", func(t *testing.T) {
		show := false
		if !ToggleHelp(&show, tea.KeyPressMsg{Code: 'h', Mod: tea.ModCtrl}) {
			t.Error("expected C-h to be handled")
		}
		if !show {
			t.Error("expected showHelp to be true")
		}
	})

	t.Run("closes help on second C-h", func(t *testing.T) {
		show := true
		if !ToggleHelp(&show, tea.KeyPressMsg{Code: 'h', Mod: tea.ModCtrl}) {
			t.Error("expected second C-h to be handled")
		}
		if show {
			t.Error("expected showHelp to be false")
		}
	})

	t.Run("closes help on esc", func(t *testing.T) {
		show := true
		if !ToggleHelp(&show, tea.KeyPressMsg{Code: tea.KeyEscape}) {
			t.Error("expected esc to be handled")
		}
		if show {
			t.Error("expected showHelp to be false")
		}
	})

	t.Run("swallows other keys while open", func(t *testing.T) {
		show := true
		if !ToggleHelp(&show, tea.KeyPressMsg{Code: tea.KeyEnter}) {
			t.Error("expected enter in help mode to be handled")
		}
		if !show {
			t.Error("expected showHelp to remain true")
		}
	})

	t.Run("ignores non-help keys when closed", func(t *testing.T) {
		show := false
		if ToggleHelp(&show, tea.KeyPressMsg{Code: tea.KeyEnter}) {
			t.Error("expected enter to not be handled when help is closed")
		}
		if show {
			t.Error("expected showHelp to remain false")
		}
	})
}
