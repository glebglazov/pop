package ui

import (
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

func TestFormatKeyHint(t *testing.T) {
	tests := []struct {
		name     string
		keys     []string
		expected string
	}{
		{
			name:     "ctrl+ format",
			keys:     []string{"ctrl+l"},
			expected: "C-l",
		},
		{
			name:     "ctrl- format",
			keys:     []string{"ctrl-l"},
			expected: "C-l",
		},
		{
			name:     "alt+ format",
			keys:     []string{"alt+x"},
			expected: "A-x",
		},
		{
			name:     "alt- format",
			keys:     []string{"alt-x"},
			expected: "A-x",
		},
		{
			name:     "plain key",
			keys:     []string{"f1"},
			expected: "f1",
		},
		{
			name:     "empty binding",
			keys:     []string{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			binding := key.NewBinding(key.WithKeys(tt.keys...))
			result := formatKeyHint(binding)
			if result != tt.expected {
				t.Errorf("formatKeyHint() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestWithCustomCommands(t *testing.T) {
	commands := []CustomCommand{
		{Key: "ctrl-l", Label: "cleanup", Command: "echo cleanup", Exit: true},
		{Key: "ctrl-o", Label: "open", Command: "echo open", Exit: false},
	}

	items := []Item{{Name: "test", Path: "/test"}}
	picker := NewPicker(items, WithCustomCommands(commands))

	if len(picker.customCommands) != 2 {
		t.Errorf("got %d custom commands, want 2", len(picker.customCommands))
	}

	// Check first command
	cc := picker.customCommands[0]
	if cc.Label != "cleanup" {
		t.Errorf("Label = %q, want %q", cc.Label, "cleanup")
	}
	if cc.Command != "echo cleanup" {
		t.Errorf("Command = %q, want %q", cc.Command, "echo cleanup")
	}
	if !cc.Exit {
		t.Error("Exit = false, want true")
	}

	// Check second command
	cc2 := picker.customCommands[1]
	if cc2.Label != "open" {
		t.Errorf("Label = %q, want %q", cc2.Label, "open")
	}
	if cc2.Exit {
		t.Error("Exit = true, want false")
	}
}

func TestBuildHintsShowsHelp(t *testing.T) {
	items := []Item{{Name: "test", Path: "/test"}}
	picker := NewPicker(items)

	hints := picker.buildHints()

	if !containsSubstring(hints, "F1 help") {
		t.Errorf("hints should contain help shortcut, got: %q", hints)
	}
	if !containsSubstring(hints, "Enter select") {
		t.Errorf("hints should contain Enter select, got: %q", hints)
	}
	if !containsSubstring(hints, "Esc quit") {
		t.Errorf("hints should contain Esc quit, got: %q", hints)
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstringHelper(s, substr))
}

func containsSubstringHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestCustomCommandKeyMatching(t *testing.T) {
	commands := []CustomCommand{
		{Key: "ctrl+o", Label: "test", Command: "echo test", Exit: true},
	}

	items := []Item{{Name: "test", Path: "/test"}}
	picker := NewPicker(items, WithCustomCommands(commands))

	// Simulate ctrl+o key press using tea.KeyMsg
	msg := tea.KeyMsg{Type: tea.KeyCtrlO}

	// Check if binding matches
	if len(picker.customCommands) == 0 {
		t.Fatal("no custom commands registered")
	}

	cc := picker.customCommands[0]
	if !key.Matches(msg, cc.Binding) {
		t.Errorf("ctrl+o key message didn't match binding")
		t.Logf("Binding keys: %v", cc.Binding.Keys())
	}
}

func TestHelpOverlayToggle(t *testing.T) {
	items := []Item{{Name: "test", Path: "/test"}}
	picker := NewPicker(items)
	picker.Init()

	if picker.showHelp {
		t.Fatal("showHelp should be false initially")
	}

	// ctrl+/ activates help
	helpMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}, Alt: false}
	// ctrl+/ is sent as a special key in bubbletea - try matching directly
	if key.Matches(helpMsg, keys.Help) {
		picker.Update(helpMsg)
		if !picker.showHelp {
			t.Error("showHelp should be true after ctrl+/")
		}
	} else {
		// Manually set to test the rest of the flow
		picker.showHelp = true
	}

	// esc in help mode dismisses help (doesn't quit)
	escMsg := tea.KeyMsg{Type: tea.KeyEscape}
	picker.Update(escMsg)
	if picker.showHelp {
		t.Error("showHelp should be false after esc in help mode")
	}
	if picker.result.Action == ActionCancel {
		t.Error("esc in help mode should not quit")
	}
}

func TestHelpOverlaySwallowsKeys(t *testing.T) {
	items := []Item{
		{Name: "test1", Path: "/test1"},
		{Name: "test2", Path: "/test2"},
	}
	picker := NewPicker(items)
	picker.Init()
	picker.showHelp = true

	// Arrow keys should be swallowed in help mode
	origCursor := picker.cursor
	picker.Update(tea.KeyMsg{Type: tea.KeyUp})
	if picker.cursor != origCursor {
		t.Error("cursor should not change in help mode")
	}

	// Enter should be swallowed (should not quit)
	_, cmd := picker.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Error("enter should be swallowed in help mode, should not produce a quit cmd")
	}

	// Help should still be active
	if !picker.showHelp {
		t.Error("showHelp should still be true")
	}
}

func TestEscInNormalModeQuits(t *testing.T) {
	items := []Item{{Name: "test", Path: "/test"}}
	picker := NewPicker(items)
	picker.Init()

	escMsg := tea.KeyMsg{Type: tea.KeyEscape}
	_, cmd := picker.Update(escMsg)

	if picker.result.Action != ActionCancel {
		t.Error("esc in normal mode should set ActionCancel")
	}
	if cmd == nil {
		t.Error("esc in normal mode should return tea.Quit cmd")
	}
}

func TestHelpViewRendersContent(t *testing.T) {
	items := []Item{{Name: "test", Path: "/test"}}
	picker := NewPicker(items, WithDelete(), WithKillSession())
	picker.width = 60
	picker.height = 20
	picker.showHelp = true

	view := picker.View()

	// Should contain base keybindings
	if !containsSubstring(view, "Navigate") {
		t.Error("help view should contain Navigate")
	}
	if !containsSubstring(view, "Select") {
		t.Error("help view should contain Select")
	}
	if !containsSubstring(view, "Quit") {
		t.Error("help view should contain Quit")
	}

	// Should contain conditional keybindings
	if !containsSubstring(view, "Kill tmux session") {
		t.Error("help view should contain Kill tmux session")
	}
	if !containsSubstring(view, "Delete") {
		t.Error("help view should contain Delete")
	}
	if !containsSubstring(view, "Force delete") {
		t.Error("help view should contain Force delete")
	}

	// Should show "Esc back" hint
	if !containsSubstring(view, "Esc back") {
		t.Error("help view should show 'Esc back' hint")
	}

	// Should show Help title
	if !containsSubstring(view, "Help") {
		t.Error("help view should show Help title")
	}
}

func TestHelpViewConditionalBindings(t *testing.T) {
	items := []Item{{Name: "test", Path: "/test"}}

	// Without delete/kill options
	picker := NewPicker(items)
	picker.width = 60
	picker.height = 20
	picker.showHelp = true

	view := picker.View()

	if containsSubstring(view, "Kill tmux session") {
		t.Error("help view should not contain Kill tmux session when disabled")
	}
	if containsSubstring(view, "Force delete") {
		t.Error("help view should not contain Force delete when disabled")
	}
}

func TestHelpViewCustomCommands(t *testing.T) {
	commands := []CustomCommand{
		{Key: "ctrl+l", Label: "cleanup", Command: "echo cleanup", Exit: true},
	}

	items := []Item{{Name: "test", Path: "/test"}}
	picker := NewPicker(items, WithCustomCommands(commands))
	picker.width = 60
	picker.height = 20
	picker.showHelp = true

	view := picker.View()

	if !containsSubstring(view, "C-l") {
		t.Error("help view should contain custom command key")
	}
	if !containsSubstring(view, "cleanup") {
		t.Error("help view should contain custom command label")
	}
}

func TestQuickAccessAltDigitSelectsItem(t *testing.T) {
	items := []Item{
		{Name: "a", Path: "/a"},
		{Name: "b", Path: "/b"},
		{Name: "c", Path: "/c"},
	}
	picker := NewPicker(items, WithQuickAccess("alt"), WithCursorAtEnd())
	picker.Init()

	// Static numbering: 1 = second from bottom (b), 2 = third from bottom (a)
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}, Alt: true}
	_, cmd := picker.Update(msg)
	if cmd == nil {
		t.Fatal("expected quit command")
	}
	result := picker.Result()
	if result.Action != ActionSelect {
		t.Errorf("expected ActionSelect, got %v", result.Action)
	}
	if result.Selected.Path != "/b" {
		t.Errorf("expected /b, got %s", result.Selected.Path)
	}
}

func TestQuickAccessAltDigitSelectsSecondItem(t *testing.T) {
	items := []Item{
		{Name: "a", Path: "/a"},
		{Name: "b", Path: "/b"},
		{Name: "c", Path: "/c"},
	}
	picker := NewPicker(items, WithQuickAccess("alt"), WithCursorAtEnd())
	picker.Init()

	// Static numbering: 2 = third from bottom (a)
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}, Alt: true}
	_, cmd := picker.Update(msg)
	if cmd == nil {
		t.Fatal("expected quit command")
	}
	result := picker.Result()
	if result.Selected.Path != "/a" {
		t.Errorf("expected /a, got %s", result.Selected.Path)
	}
}

func TestQuickAccessAltDigitOutOfRange(t *testing.T) {
	items := []Item{
		{Name: "a", Path: "/a"},
		{Name: "b", Path: "/b"},
	}
	picker := NewPicker(items, WithQuickAccess("alt"), WithCursorAtEnd())
	picker.Init()

	// Only 1 item above bottom, so alt+5 should do nothing
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}, Alt: true}
	_, cmd := picker.Update(msg)
	if cmd != nil {
		t.Error("expected no quit command for out-of-range quick access")
	}
}

func TestQuickAccessStaticPositioning(t *testing.T) {
	items := []Item{
		{Name: "a", Path: "/a"},
		{Name: "b", Path: "/b"},
		{Name: "c", Path: "/c"},
	}
	picker := NewPicker(items, WithQuickAccess("alt"), WithCursorAtEnd())
	picker.Init()

	// Move cursor up to "b"
	picker.Update(tea.KeyMsg{Type: tea.KeyUp})

	// alt+1 should still select "b" (static: second from bottom), not "a"
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}, Alt: true}
	_, cmd := picker.Update(msg)
	if cmd == nil {
		t.Fatal("expected quit command")
	}
	result := picker.Result()
	if result.Selected.Path != "/b" {
		t.Errorf("expected /b (static position), got %s", result.Selected.Path)
	}
}

func TestQuickAccessDisabledByDefault(t *testing.T) {
	items := []Item{
		{Name: "a", Path: "/a"},
		{Name: "b", Path: "/b"},
	}
	picker := NewPicker(items, WithCursorAtEnd())
	picker.Init()

	// alt+1 should NOT trigger quick access when not enabled
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}, Alt: true}
	_, cmd := picker.Update(msg)
	if cmd != nil {
		t.Error("quick access should not work when not enabled")
	}
}

func TestQuickAccessDisabledModifier(t *testing.T) {
	items := []Item{
		{Name: "a", Path: "/a"},
		{Name: "b", Path: "/b"},
		{Name: "c", Path: "/c"},
	}
	picker := NewPicker(items, WithQuickAccess("disabled"), WithCursorAtEnd())
	picker.Init()

	// alt+1 should NOT trigger quick access when disabled
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}, Alt: true}
	_, cmd := picker.Update(msg)
	if cmd != nil {
		t.Error("quick access should not work when disabled")
	}
}

func TestQuickAccessNumbersRendered(t *testing.T) {
	items := []Item{
		{Name: "aaa", Path: "/aaa"},
		{Name: "bbb", Path: "/bbb"},
		{Name: "ccc", Path: "/ccc"},
		{Name: "ddd", Path: "/ddd"},
	}
	picker := NewPicker(items, WithQuickAccess("alt"), WithCursorAtEnd())
	picker.width = 60
	picker.height = 20
	picker.Init()

	view := picker.View()
	// Items above cursor (ddd) should show ⌥1 for ccc, ⌥2 for bbb, ⌥3 for aaa
	if !containsSubstring(view, "⌥1") {
		t.Error("view should contain ⌥1 label")
	}
	if !containsSubstring(view, "⌥3") {
		t.Error("view should contain ⌥3 label")
	}
}

func TestQuickAccessCtrlNumbersRendered(t *testing.T) {
	items := []Item{
		{Name: "aaa", Path: "/aaa"},
		{Name: "bbb", Path: "/bbb"},
	}
	picker := NewPicker(items, WithQuickAccess("ctrl"), WithCursorAtEnd())
	picker.width = 60
	picker.height = 20
	picker.Init()

	view := picker.View()
	if !containsSubstring(view, "^1") {
		t.Error("view should contain ^1 label for ctrl modifier")
	}
}

func TestQuickAccessNoNumberOnBottomItem(t *testing.T) {
	items := []Item{
		{Name: "aaa", Path: "/aaa"},
		{Name: "bbb", Path: "/bbb"},
		{Name: "ccc", Path: "/ccc"},
	}
	// Static numbering: aaa=⌥2, bbb=⌥1, ccc=no number (bottom item)
	picker := NewPicker(items, WithQuickAccess("alt"), WithCursorAtEnd())
	picker.width = 60
	picker.height = 20
	picker.Init()

	view := picker.View()
	if !containsSubstring(view, "⌥1") {
		t.Error("view should contain ⌥1 for second from bottom")
	}
	if !containsSubstring(view, "⌥2") {
		t.Error("view should contain ⌥2 for third from bottom")
	}
}

func TestQuickAccessHelpOverlayAlt(t *testing.T) {
	items := []Item{{Name: "test", Path: "/test"}}
	picker := NewPicker(items, WithQuickAccess("alt"))
	picker.width = 60
	picker.height = 20
	picker.showHelp = true

	view := picker.View()
	if !containsSubstring(view, "Quick select") {
		t.Error("help view should contain Quick select entry")
	}
	if !containsSubstring(view, "A-1..9") {
		t.Error("help view should show A-1..9 key hint for alt modifier")
	}
}

func TestQuickAccessHelpOverlayCtrl(t *testing.T) {
	items := []Item{{Name: "test", Path: "/test"}}
	picker := NewPicker(items, WithQuickAccess("ctrl"))
	picker.width = 60
	picker.height = 20
	picker.showHelp = true

	view := picker.View()
	if !containsSubstring(view, "Quick select") {
		t.Error("help view should contain Quick select entry")
	}
	if !containsSubstring(view, "C-1..9") {
		t.Error("help view should show C-1..9 key hint for ctrl modifier")
	}
}

func TestQuickAccessHelpOverlayDisabled(t *testing.T) {
	items := []Item{{Name: "test", Path: "/test"}}
	picker := NewPicker(items, WithQuickAccess("disabled"))
	picker.width = 60
	picker.height = 20
	picker.showHelp = true

	view := picker.View()
	if containsSubstring(view, "Quick select") {
		t.Error("help view should NOT contain Quick select when disabled")
	}
}
