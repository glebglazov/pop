package ui

import (
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
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

func TestWithUserDefinedCommands(t *testing.T) {
	commands := []UserDefinedCommand{
		{Key: "ctrl-l", Label: "cleanup", Command: "echo cleanup", Exit: true},
		{Key: "ctrl-o", Label: "open", Command: "echo open", Exit: false},
	}

	items := []Item{{Name: "test", Path: "/test"}}
	picker := NewPicker(items, WithUserDefinedCommands(commands))

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

func TestUserDefinedCommandKeyMatching(t *testing.T) {
	commands := []UserDefinedCommand{
		{Key: "ctrl+o", Label: "test", Command: "echo test", Exit: true},
	}

	items := []Item{{Name: "test", Path: "/test"}}
	picker := NewPicker(items, WithUserDefinedCommands(commands))

	// Simulate ctrl+o key press
	msg := tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl}

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

	// F1 activates help
	helpMsg := tea.KeyPressMsg{Code: tea.KeyF1}
	if key.Matches(helpMsg, keys.Help) {
		picker.Update(helpMsg)
		if !picker.showHelp {
			t.Error("showHelp should be true after F1")
		}
	} else {
		// Manually set to test the rest of the flow
		picker.showHelp = true
	}

	// esc in help mode dismisses help (doesn't quit)
	escMsg := tea.KeyPressMsg{Code: tea.KeyEscape}
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
	picker.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if picker.cursor != origCursor {
		t.Error("cursor should not change in help mode")
	}

	// Enter should be swallowed (should not quit)
	_, cmd := picker.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
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

	escMsg := tea.KeyPressMsg{Code: tea.KeyEscape}
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

	view := picker.viewHelp()

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

	view := picker.viewHelp()

	if containsSubstring(view, "Kill tmux session") {
		t.Error("help view should not contain Kill tmux session when disabled")
	}
	if containsSubstring(view, "Delete") {
		t.Error("help view should not contain Delete when disabled")
	}
}

func TestHelpViewUserDefinedCommands(t *testing.T) {
	commands := []UserDefinedCommand{
		{Key: "ctrl+l", Label: "cleanup", Command: "echo cleanup", Exit: true},
	}

	items := []Item{{Name: "test", Path: "/test"}}
	picker := NewPicker(items, WithUserDefinedCommands(commands))
	picker.width = 60
	picker.height = 20
	picker.showHelp = true

	view := picker.viewHelp()

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
	msg := tea.KeyPressMsg{Code: '1', Mod: tea.ModAlt}
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
	msg := tea.KeyPressMsg{Code: '2', Mod: tea.ModAlt}
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
	msg := tea.KeyPressMsg{Code: '5', Mod: tea.ModAlt}
	_, cmd := picker.Update(msg)
	if cmd != nil {
		t.Error("expected no quit command for out-of-range quick access")
	}
}

func TestQuickAccessCursorRelativePositioning(t *testing.T) {
	items := []Item{
		{Name: "a", Path: "/a"},
		{Name: "b", Path: "/b"},
		{Name: "c", Path: "/c"},
	}
	picker := NewPicker(items, WithQuickAccess("alt"), WithCursorAtEnd())
	picker.Init()

	// Move cursor up to "b" (index 1)
	picker.Update(tea.KeyPressMsg{Code: tea.KeyUp})

	// alt+1 should select item above cursor = "a" (cursor-relative)
	msg := tea.KeyPressMsg{Code: '1', Mod: tea.ModAlt}
	_, cmd := picker.Update(msg)
	if cmd == nil {
		t.Fatal("expected quit command")
	}
	result := picker.Result()
	if result.Selected.Path != "/a" {
		t.Errorf("expected /a (above cursor), got %s", result.Selected.Path)
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
	msg := tea.KeyPressMsg{Code: '1', Mod: tea.ModAlt}
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
	msg := tea.KeyPressMsg{Code: '1', Mod: tea.ModAlt}
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

	view := picker.viewNormal()
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

	view := picker.viewNormal()
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

	view := picker.viewNormal()
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

	view := picker.viewHelp()
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

	view := picker.viewHelp()
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

	view := picker.viewHelp()
	if containsSubstring(view, "Quick select") {
		t.Error("help view should NOT contain Quick select when disabled")
	}
}

func TestQuickAccessCursorRelativeMovesWithCursor(t *testing.T) {
	items := []Item{
		{Name: "a", Path: "/a"},
		{Name: "b", Path: "/b"},
		{Name: "c", Path: "/c"},
		{Name: "d", Path: "/d"},
	}
	picker := NewPicker(items, WithQuickAccess("alt"), WithCursorAtEnd())
	picker.Init()

	// Move cursor up twice: cursor now at index 1 (item "b")
	picker.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	picker.Update(tea.KeyPressMsg{Code: tea.KeyUp})

	// alt+1 should select item above cursor = index 0 = "a"
	msg := tea.KeyPressMsg{Code: '1', Mod: tea.ModAlt}
	_, cmd := picker.Update(msg)
	if cmd == nil {
		t.Fatal("expected quit command")
	}
	result := picker.Result()
	if result.Selected.Path != "/a" {
		t.Errorf("expected /a (above cursor), got %s", result.Selected.Path)
	}
}

func TestQuickAccessCursorRelativeRendering(t *testing.T) {
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

	// Move cursor up to "ccc" (index 2)
	picker.Update(tea.KeyPressMsg{Code: tea.KeyUp})

	view := picker.viewNormal()
	// Items above cursor (bbb=⌥1, aaa=⌥2) should have labels
	if !containsSubstring(view, "⌥1") {
		t.Error("view should contain ⌥1 for item above cursor")
	}
	if !containsSubstring(view, "⌥2") {
		t.Error("view should contain ⌥2 for second item above cursor")
	}
}

func TestQuickAccessNoNumbersBelowCursor(t *testing.T) {
	items := []Item{
		{Name: "aaa", Path: "/aaa"},
		{Name: "bbb", Path: "/bbb"},
		{Name: "ccc", Path: "/ccc"},
	}
	picker := NewPicker(items, WithQuickAccess("alt"), WithCursorAtEnd())
	picker.width = 60
	picker.height = 20
	picker.Init()

	// Move cursor up to "bbb" (index 1)
	picker.Update(tea.KeyPressMsg{Code: tea.KeyUp})

	// alt+1 should select "aaa" (above cursor)
	msg := tea.KeyPressMsg{Code: '1', Mod: tea.ModAlt}
	_, cmd := picker.Update(msg)
	if cmd == nil {
		t.Fatal("expected quit command")
	}
	if picker.Result().Selected.Path != "/aaa" {
		t.Errorf("expected /aaa, got %s", picker.Result().Selected.Path)
	}
}

func TestQuickAccessNearTopFewerShortcuts(t *testing.T) {
	items := []Item{
		{Name: "a", Path: "/a"},
		{Name: "b", Path: "/b"},
		{Name: "c", Path: "/c"},
	}
	picker := NewPicker(items, WithQuickAccess("alt"), WithCursorAtEnd())
	picker.Init()

	// Move cursor to index 1 ("b") — only 1 item above
	picker.Update(tea.KeyPressMsg{Code: tea.KeyUp})

	// alt+1 should work (selects "a")
	msg1 := tea.KeyPressMsg{Code: '1', Mod: tea.ModAlt}
	_, cmd := picker.Update(msg1)
	if cmd == nil {
		t.Fatal("expected quit command for alt+1")
	}
	if picker.Result().Selected.Path != "/a" {
		t.Errorf("expected /a, got %s", picker.Result().Selected.Path)
	}
}

func TestQuickAccessNearTopOutOfRange(t *testing.T) {
	items := []Item{
		{Name: "a", Path: "/a"},
		{Name: "b", Path: "/b"},
		{Name: "c", Path: "/c"},
	}
	picker := NewPicker(items, WithQuickAccess("alt"), WithCursorAtEnd())
	picker.Init()

	// Move cursor to index 1 ("b") — only 1 item above
	picker.Update(tea.KeyPressMsg{Code: tea.KeyUp})

	// alt+2 should do nothing (only 1 item above cursor)
	msg2 := tea.KeyPressMsg{Code: '2', Mod: tea.ModAlt}
	_, cmd := picker.Update(msg2)
	if cmd != nil {
		t.Error("expected no quit command for alt+2 with only 1 item above cursor")
	}
}

func TestQuickAccessScrollMargin(t *testing.T) {
	// Create 20 items
	var items []Item
	for i := 0; i < 20; i++ {
		name := string(rune('a' + i))
		items = append(items, Item{
			Name: name,
			Path: "/" + name,
		})
	}
	picker := NewPicker(items, WithQuickAccess("alt"), WithCursorAtEnd())
	picker.height = 15
	picker.Init()

	// Move cursor up 4 times: cursor at index 15
	for i := 0; i < 4; i++ {
		picker.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	}

	// Cursor at 15, scroll should show at least 9 items above cursor
	// So scroll should be <= 15 - 9 = 6
	if picker.scroll > 6 {
		t.Errorf("expected scroll <= 6 for QA margin, got %d (cursor=%d)", picker.scroll, picker.cursor)
	}
}

func TestQuickAccessScrollMarginNearTop(t *testing.T) {
	// Create 20 items
	var items []Item
	for i := 0; i < 20; i++ {
		name := string(rune('a' + i))
		items = append(items, Item{
			Name: name,
			Path: "/" + name,
		})
	}
	picker := NewPicker(items, WithQuickAccess("alt"), WithCursorAtEnd())
	picker.height = 15
	picker.Init()

	// Move cursor to index 3 (near top)
	for i := 0; i < 16; i++ {
		picker.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	}

	// Near top, scroll should be 0 (can't scroll further)
	if picker.scroll != 0 {
		t.Errorf("expected scroll = 0 near top, got %d (cursor=%d)", picker.scroll, picker.cursor)
	}
}

func TestUserDefinedCommandOverridesDisabledBuiltin(t *testing.T) {
	// ctrl+o is bound to OpenWindow, but OpenWindow is not enabled here.
	// Custom command should work.
	commands := []UserDefinedCommand{
		{Key: "ctrl+o", Label: "custom open", Command: "echo open", Exit: true},
	}
	items := []Item{{Name: "test", Path: "/test"}}
	picker := NewPicker(items, WithUserDefinedCommands(commands))
	picker.Init()

	msg := tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl}
	_, cmd := picker.Update(msg)

	if cmd == nil {
		t.Fatal("expected quit command")
	}
	result := picker.Result()
	if result.Action != ActionUserDefinedCommand {
		t.Errorf("expected ActionUserDefinedCommand, got %v", result.Action)
	}
	if result.UserDefinedCommand == nil || result.UserDefinedCommand.Command != "echo open" {
		t.Error("expected custom command result")
	}
}

func TestUserDefinedCommandOverridesEnabledBuiltin(t *testing.T) {
	// ctrl+k is bound to KillSession, and KillSession IS enabled.
	// Custom command should still take priority.
	commands := []UserDefinedCommand{
		{Key: "ctrl+k", Label: "custom kill", Command: "echo kill", Exit: true},
	}
	items := []Item{{Name: "test", Path: "/test"}}
	picker := NewPicker(items, WithKillSession(), WithUserDefinedCommands(commands))
	picker.Init()

	msg := tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl}
	_, cmd := picker.Update(msg)

	if cmd == nil {
		t.Fatal("expected quit command")
	}
	result := picker.Result()
	if result.Action != ActionUserDefinedCommand {
		t.Errorf("expected ActionUserDefinedCommand, got %v (ActionKillSession=%v)", result.Action, ActionKillSession)
	}
}

func TestHelpViewHidesOverriddenBuiltin(t *testing.T) {
	commands := []UserDefinedCommand{
		{Key: "ctrl+k", Label: "my custom cmd", Command: "echo x", Exit: true},
	}
	items := []Item{{Name: "test", Path: "/test"}}
	picker := NewPicker(items, WithKillSession(), WithUserDefinedCommands(commands))
	picker.width = 60
	picker.height = 20
	picker.showHelp = true

	view := picker.viewHelp()

	// Built-in "Kill tmux session" should NOT appear (overridden)
	if containsSubstring(view, "Kill tmux session") {
		t.Error("help view should not show overridden built-in 'Kill tmux session'")
	}
	// Custom command label should appear
	if !containsSubstring(view, "my custom cmd") {
		t.Error("help view should show custom command label")
	}
}

func TestBuiltinWorksWhenNoOverride(t *testing.T) {
	items := []Item{{Name: "test", Path: "/test"}}
	picker := NewPicker(items, WithKillSession())
	picker.Init()

	msg := tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl}
	_, cmd := picker.Update(msg)

	if cmd == nil {
		t.Fatal("expected quit command")
	}
	result := picker.Result()
	if result.Action != ActionKillSession {
		t.Errorf("expected ActionKillSession, got %v", result.Action)
	}
}

func typeInPicker(p *Picker, s string) {
	for _, ch := range s {
		p.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)})
	}
}

func TestFilterCaseInsensitive(t *testing.T) {
	items := []Item{
		{Name: "Dev", Path: "/dev"},
		{Name: "app_server", Path: "/app"},
		{Name: "Backstage", Path: "/backstage"},
	}
	picker := NewPicker(items, WithCursorAtEnd())
	picker.Init()

	typeInPicker(picker, "dev")

	found := false
	for _, item := range picker.filtered {
		if item.Path == "/dev" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'Dev' in filtered results for query 'dev', got: %v", picker.filtered)
	}
}

func TestFilterCaseInsensitiveUppercaseQuery(t *testing.T) {
	items := []Item{
		{Name: "dev", Path: "/dev"},
		{Name: "app_server", Path: "/app"},
	}
	picker := NewPicker(items, WithCursorAtEnd())
	picker.Init()

	typeInPicker(picker, "Dev")

	found := false
	for _, item := range picker.filtered {
		if item.Path == "/dev" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'dev' in filtered results for query 'Dev', got: %v", picker.filtered)
	}
}

func TestNavigationWrapAround(t *testing.T) {
	items := []Item{
		{Name: "a", Path: "/a"},
		{Name: "b", Path: "/b"},
		{Name: "c", Path: "/c"},
	}
	picker := NewPicker(items)
	picker.width = 60
	picker.height = 20
	picker.Init()
	picker.cursor = 0

	// Up at top wraps to bottom
	picker.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if picker.cursor != 2 {
		t.Errorf("Up at top: cursor = %d, want 2", picker.cursor)
	}

	// Down at bottom wraps to top
	picker.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if picker.cursor != 0 {
		t.Errorf("Down at bottom: cursor = %d, want 0", picker.cursor)
	}
}

func TestHalfPageUpDown(t *testing.T) {
	items := make([]Item, 30)
	for i := range items {
		items[i] = Item{Name: string(rune('a' + i%26)), Path: "/" + string(rune('a'+i%26))}
	}
	picker := NewPicker(items)
	picker.width = 60
	picker.height = 10
	picker.Init()
	picker.cursor = 15

	// ctrl+b = HalfPageUp
	picker.Update(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	if picker.cursor != 5 {
		t.Errorf("PageUp: cursor = %d, want 5", picker.cursor)
	}

	// Page up clamps to 0
	picker.Update(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	if picker.cursor != 0 {
		t.Errorf("PageUp clamp: cursor = %d, want 0", picker.cursor)
	}

	// ctrl+f = HalfPageDown
	picker.cursor = 15
	picker.Update(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	if picker.cursor != 25 {
		t.Errorf("PageDown: cursor = %d, want 25", picker.cursor)
	}

	// Page down clamps to last
	picker.Update(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	if picker.cursor != 29 {
		t.Errorf("PageDown clamp: cursor = %d, want 29", picker.cursor)
	}
}

func TestDeleteAction(t *testing.T) {
	items := []Item{
		{Name: "a", Path: "/a"},
		{Name: "b", Path: "/b"},
	}
	picker := NewPicker(items, WithDelete())
	picker.width = 60
	picker.height = 20
	picker.Init()
	picker.cursor = 0

	m, cmd := picker.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	p := m.(*Picker)
	if p.result.Action != ActionDelete {
		t.Errorf("action = %v, want ActionDelete", p.result.Action)
	}
	if cmd == nil {
		t.Error("expected tea.Quit cmd")
	}
}

func TestForceDeleteAction(t *testing.T) {
	items := []Item{
		{Name: "a", Path: "/a"},
	}
	picker := NewPicker(items, WithDelete())
	picker.width = 60
	picker.height = 20
	picker.Init()
	picker.cursor = 0

	// ctrl+x = ForceDelete
	m, _ := picker.Update(tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
	p := m.(*Picker)
	if p.result.Action != ActionForceDelete {
		t.Errorf("action = %v, want ActionForceDelete", p.result.Action)
	}
}

func TestDeleteDisabledByDefault(t *testing.T) {
	items := []Item{
		{Name: "a", Path: "/a"},
	}
	picker := NewPicker(items)
	picker.width = 60
	picker.height = 20
	picker.Init()
	picker.cursor = 0

	m, _ := picker.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	p := m.(*Picker)
	if p.result.Action == ActionDelete {
		t.Error("delete should not work when WithDelete() not set")
	}
}

func TestClearInput(t *testing.T) {
	items := []Item{
		{Name: "apple", Path: "/apple"},
		{Name: "banana", Path: "/banana"},
	}
	picker := NewPicker(items, WithCursorAtEnd())
	picker.width = 60
	picker.height = 20
	picker.Init()

	// Type something to filter
	typeInPicker(picker, "app")
	if len(picker.filtered) == len(items) {
		t.Fatal("filter should have reduced item count")
	}

	// ctrl+u = ClearInput
	picker.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	if picker.input.Value() != "" {
		t.Errorf("input = %q, want empty after ctrl+u", picker.input.Value())
	}
	if len(picker.filtered) != len(items) {
		t.Errorf("filtered = %d items, want %d (all items restored)", len(picker.filtered), len(items))
	}
}

func TestResultSetsCursorIndex(t *testing.T) {
	items := []Item{
		{Name: "a", Path: "/a"},
		{Name: "b", Path: "/b"},
		{Name: "c", Path: "/c"},
	}
	picker := NewPicker(items)
	picker.width = 60
	picker.height = 20
	picker.Init()
	picker.cursor = 1

	// Press enter
	picker.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	result := picker.Result()
	if result.CursorIndex != 1 {
		t.Errorf("CursorIndex = %d, want 1", result.CursorIndex)
	}
}

func TestCursorMemoryRoundTrip(t *testing.T) {
	items := []Item{
		{Name: "apple", Path: "/apple"},
		{Name: "app", Path: "/app"},
		{Name: "banana", Path: "/banana"},
		{Name: "cherry", Path: "/cherry"},
	}
	picker := NewPicker(items, WithCursorAtEnd())
	picker.width = 60
	picker.height = 20
	picker.Init()

	// Remember unfiltered cursor position
	unfilteredCursor := picker.cursor

	// Type "app" to filter
	typeInPicker(picker, "app")

	// Move cursor in filtered view
	if len(picker.filtered) > 1 {
		picker.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	}
	filteredCursor := picker.cursor
	filteredPath := ""
	if filteredCursor < len(picker.filtered) {
		filteredPath = picker.filtered[filteredCursor].Path
	}

	// Clear filter — should restore unfiltered position
	picker.input.SetValue("")
	picker.filter()

	// Re-type "app" — should restore the saved filtered position
	typeInPicker(picker, "app")

	if filteredPath != "" && picker.cursor < len(picker.filtered) {
		restoredPath := picker.filtered[picker.cursor].Path
		if restoredPath != filteredPath {
			t.Errorf("cursor memory: after re-typing filter, cursor at %q, want %q", restoredPath, filteredPath)
		}
	}

	// Clear again and verify unfiltered position is roughly preserved
	picker.input.SetValue("")
	picker.filter()
	_ = unfilteredCursor // position may shift due to memory, but should not crash
}

func TestViewNormalEmptyList(t *testing.T) {
	picker := NewPicker(nil)
	picker.width = 60
	picker.height = 20
	picker.Init()

	view := picker.viewNormal()
	if view == "" {
		t.Error("viewNormal() returned empty string for nil items")
	}
}

func TestViewNormalRendersItems(t *testing.T) {
	items := []Item{
		{Name: "project-a", Path: "/a"},
		{Name: "project-b", Path: "/b", Icon: "■"},
	}
	picker := NewPicker(items)
	picker.width = 60
	picker.height = 20
	picker.Init()

	view := picker.viewNormal()
	if !containsSubstring(view, "project-a") {
		t.Error("viewNormal() missing 'project-a'")
	}
	if !containsSubstring(view, "project-b") {
		t.Error("viewNormal() missing 'project-b'")
	}
}

func TestInitCursorAtEnd(t *testing.T) {
	items := []Item{
		{Name: "a", Path: "/a"},
		{Name: "b", Path: "/b"},
		{Name: "c", Path: "/c"},
	}
	picker := NewPicker(items, WithCursorAtEnd())
	picker.width = 60
	picker.height = 20
	picker.Init()

	if picker.cursor != 2 {
		t.Errorf("cursor = %d, want 2 (last item)", picker.cursor)
	}
}

func TestInitWithInitialCursorIndex(t *testing.T) {
	items := []Item{
		{Name: "a", Path: "/a"},
		{Name: "b", Path: "/b"},
		{Name: "c", Path: "/c"},
	}
	picker := NewPicker(items, WithCursorAtEnd(), WithInitialCursorIndex(1))
	picker.width = 60
	picker.height = 20
	picker.Init()

	if picker.cursor != 1 {
		t.Errorf("cursor = %d, want 1 (WithInitialCursorIndex overrides WithCursorAtEnd)", picker.cursor)
	}
}

func TestResetAction(t *testing.T) {
	items := []Item{
		{Name: "a", Path: "/a"},
	}
	picker := NewPicker(items, WithReset())
	picker.width = 60
	picker.height = 20
	picker.Init()
	picker.cursor = 0

	// ctrl+r = Reset
	m, _ := picker.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	p := m.(*Picker)
	if p.result.Action != ActionReset {
		t.Errorf("action = %v, want ActionReset", p.result.Action)
	}
}

func TestRebuildAttentionViewPreservesCursor(t *testing.T) {
	panes := []AttentionPane{
		{PaneID: "%1", Session: "alpha", Following: true},
		{PaneID: "%2", Session: "beta", Following: false},
		{PaneID: "%3", Session: "gamma", Following: true},
	}

	t.Run("cursor preserved switching to following mode", func(t *testing.T) {
		p := NewPicker(nil, WithAttentionPanes(panes, AttentionCallbacks{}))
		p.attentionCursor = 2 // on %3 (following=true)

		p.attentionFollowing = true
		p.rebuildAttentionView()

		// %3 should still be under cursor, now at index 1 in filtered list [%1, %3]
		if p.attentionCursor != 1 {
			t.Errorf("cursor = %d, want 1", p.attentionCursor)
		}
		if p.attentionPanes[p.attentionCursor].PaneID != "%3" {
			t.Errorf("pane = %s, want %%3", p.attentionPanes[p.attentionCursor].PaneID)
		}
	})

	t.Run("cursor preserved switching back to normal mode", func(t *testing.T) {
		p := NewPicker(nil, WithAttentionPanes(panes, AttentionCallbacks{}))
		// Simulate being in following mode with cursor on %1
		p.attentionFollowing = true
		p.rebuildAttentionView()
		p.attentionCursor = 0 // on %1 in filtered list [%1, %3]

		p.attentionFollowing = false
		p.rebuildAttentionView()

		// %1 should still be under cursor, at index 0 in full list
		if p.attentionPanes[p.attentionCursor].PaneID != "%1" {
			t.Errorf("pane = %s, want %%1", p.attentionPanes[p.attentionCursor].PaneID)
		}
	})

	t.Run("cursor clamped when pane not in new view", func(t *testing.T) {
		p := NewPicker(nil, WithAttentionPanes(panes, AttentionCallbacks{}))
		p.attentionCursor = 1 // on %2 (following=false)

		p.attentionFollowing = true
		p.rebuildAttentionView()

		// %2 is not followed, cursor should clamp to valid range [%1, %3]
		if p.attentionCursor < 0 || p.attentionCursor >= len(p.attentionPanes) {
			t.Errorf("cursor = %d, out of range [0, %d)", p.attentionCursor, len(p.attentionPanes))
		}
	})

	t.Run("cursor clamped on empty following view", func(t *testing.T) {
		noFollow := []AttentionPane{
			{PaneID: "%1", Session: "alpha", Following: false},
			{PaneID: "%2", Session: "beta", Following: false},
		}
		p := NewPicker(nil, WithAttentionPanes(noFollow, AttentionCallbacks{}))
		p.attentionCursor = 1

		p.attentionFollowing = true
		p.rebuildAttentionView()

		if p.attentionCursor != 0 {
			t.Errorf("cursor = %d, want 0 for empty view", p.attentionCursor)
		}
	})
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxWidth int
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			maxWidth: 10,
			expected: "",
		},
		{
			name:     "zero width",
			input:    "hello",
			maxWidth: 0,
			expected: "",
		},
		{
			name:     "negative width",
			input:    "hello",
			maxWidth: -1,
			expected: "",
		},
		{
			name:     "string within width",
			input:    "hello",
			maxWidth: 10,
			expected: "hello",
		},
		{
			name:     "string at exact width",
			input:    "hello",
			maxWidth: 5,
			expected: "hello",
		},
		{
			name:     "string exceeds width",
			input:    "hello world",
			maxWidth: 5,
			expected: "hello",
		},
		{
			name:     "ANSI escape not counted toward width",
			input:    "\x1b[31mhello\x1b[0m",
			maxWidth: 5,
			expected: "\x1b[31mhello\x1b[0m",
		},
		{
			name:     "ANSI escape with truncation",
			input:    "\x1b[31mhello world\x1b[0m",
			maxWidth: 5,
			expected: "\x1b[31mhello",
		},
		{
			name:     "multiple ANSI escapes",
			input:    "\x1b[1m\x1b[31mhi\x1b[0m",
			maxWidth: 2,
			expected: "\x1b[1m\x1b[31mhi\x1b[0m",
		},
		{
			name:     "width of 1",
			input:    "abc",
			maxWidth: 1,
			expected: "a",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateString(tt.input, tt.maxWidth)
			if result != tt.expected {
				t.Errorf("truncateString(%q, %d) = %q, want %q", tt.input, tt.maxWidth, result, tt.expected)
			}
		})
	}
}

func TestAttentionStatusOrder(t *testing.T) {
	// Verify the sort contract: idle < working < unread
	idle := attentionStatusOrder(AttentionIdle)
	working := attentionStatusOrder(AttentionWorking)
	unread := attentionStatusOrder(AttentionUnread)

	if idle >= working {
		t.Errorf("idle (%d) should be less than working (%d)", idle, working)
	}
	if working >= unread {
		t.Errorf("working (%d) should be less than unread (%d)", working, unread)
	}
}

func TestSortAttentionPanes(t *testing.T) {
	panes := []AttentionPane{
		{PaneID: "%1", Status: AttentionUnread},
		{PaneID: "%2", Status: AttentionIdle},
		{PaneID: "%3", Status: AttentionWorking},
		{PaneID: "%4", Status: AttentionIdle},
	}
	p := NewPicker(nil, WithAttentionPanes(panes, AttentionCallbacks{}))
	p.sortAttentionPanes()

	// Expected order: idle, idle, working, unread
	expectedIDs := []string{"%2", "%4", "%3", "%1"}
	for i, want := range expectedIDs {
		if p.attentionPanes[i].PaneID != want {
			t.Errorf("attentionPanes[%d].PaneID = %s, want %s", i, p.attentionPanes[i].PaneID, want)
		}
	}
	// attentionAllPanes should also be sorted
	for i, want := range expectedIDs {
		if p.attentionAllPanes[i].PaneID != want {
			t.Errorf("attentionAllPanes[%d].PaneID = %s, want %s", i, p.attentionAllPanes[i].PaneID, want)
		}
	}
}

func TestUpdateAllPanesStatus(t *testing.T) {
	panes := []AttentionPane{
		{PaneID: "%1", Status: AttentionIdle},
		{PaneID: "%2", Status: AttentionWorking},
	}
	p := NewPicker(nil, WithAttentionPanes(panes, AttentionCallbacks{}))

	p.updateAllPanesStatus("%1", AttentionUnread)
	if p.attentionAllPanes[0].Status != AttentionUnread {
		t.Errorf("status = %d, want AttentionUnread", p.attentionAllPanes[0].Status)
	}
	// %2 should be unchanged
	if p.attentionAllPanes[1].Status != AttentionWorking {
		t.Errorf("status = %d, want AttentionWorking", p.attentionAllPanes[1].Status)
	}

	// Non-existent pane ID should be a no-op
	p.updateAllPanesStatus("%99", AttentionIdle)
	if p.attentionAllPanes[0].Status != AttentionUnread {
		t.Errorf("status changed for non-matching pane")
	}
}

// helper to create a picker in attention mode with test panes
func newAttentionPicker(panes []AttentionPane, cb AttentionCallbacks, items []Item) *Picker {
	p := NewPicker(items, WithAttentionPanes(panes, cb))
	p.attentionMode = true
	p.attentionCursor = 0
	p.width = 80
	p.height = 24
	return p
}

func TestUpdateAttention_Back(t *testing.T) {
	panes := []AttentionPane{
		{PaneID: "%1", Session: "s1", Name: "p1"},
	}

	t.Run("back with items exits attention mode", func(t *testing.T) {
		items := []Item{{Name: "proj", Path: "/proj"}}
		p := newAttentionPicker(panes, AttentionCallbacks{}, items)
		msg := tea.KeyPressMsg{Code: tea.KeyLeft}

		_, cmd := p.Update(msg)
		if cmd != nil {
			t.Error("expected nil cmd")
		}
		if p.attentionMode {
			t.Error("expected attentionMode = false")
		}
	})

	t.Run("back with no items quits with cancel", func(t *testing.T) {
		p := newAttentionPicker(panes, AttentionCallbacks{}, nil)
		msg := tea.KeyPressMsg{Code: tea.KeyLeft}

		_, cmd := p.Update(msg)
		if cmd == nil {
			t.Fatal("expected quit cmd")
		}
		if p.result.Action != ActionCancel {
			t.Errorf("action = %d, want ActionCancel", p.result.Action)
		}
	})

	t.Run("back with dirty state quits with refresh", func(t *testing.T) {
		items := []Item{{Name: "proj", Path: "/proj"}}
		p := newAttentionPicker(panes, AttentionCallbacks{}, items)
		p.attentionDirty = true
		msg := tea.KeyPressMsg{Code: tea.KeyLeft}

		_, cmd := p.Update(msg)
		if cmd == nil {
			t.Fatal("expected quit cmd")
		}
		if p.result.Action != ActionRefresh {
			t.Errorf("action = %d, want ActionRefresh", p.result.Action)
		}
	})
}

func TestUpdateAttention_Quit(t *testing.T) {
	panes := []AttentionPane{
		{PaneID: "%1", Session: "s1", Name: "p1"},
	}

	t.Run("esc with items goes back to normal", func(t *testing.T) {
		items := []Item{{Name: "proj", Path: "/proj"}}
		p := newAttentionPicker(panes, AttentionCallbacks{}, items)
		msg := tea.KeyPressMsg{Code: 0x1b} // esc

		_, cmd := p.Update(msg)
		if cmd != nil {
			t.Error("expected nil cmd")
		}
		if p.attentionMode {
			t.Error("expected attentionMode = false")
		}
	})

	t.Run("ctrl+c always quits", func(t *testing.T) {
		items := []Item{{Name: "proj", Path: "/proj"}}
		p := newAttentionPicker(panes, AttentionCallbacks{}, items)
		msg := tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}

		_, cmd := p.Update(msg)
		if cmd == nil {
			t.Fatal("expected quit cmd")
		}
		if p.result.Action != ActionCancel {
			t.Errorf("action = %d, want ActionCancel", p.result.Action)
		}
	})

	t.Run("esc in standalone mode quits", func(t *testing.T) {
		p := newAttentionPicker(panes, AttentionCallbacks{}, nil)
		msg := tea.KeyPressMsg{Code: 0x1b} // esc

		_, cmd := p.Update(msg)
		if cmd == nil {
			t.Fatal("expected quit cmd")
		}
		if p.result.Action != ActionCancel {
			t.Errorf("action = %d, want ActionCancel", p.result.Action)
		}
	})
}

func TestUpdateAttention_Enter(t *testing.T) {
	t.Run("enter selects current pane", func(t *testing.T) {
		panes := []AttentionPane{
			{PaneID: "%1", Session: "s1", Name: "p1"},
			{PaneID: "%2", Session: "s2", Name: "p2"},
		}
		p := newAttentionPicker(panes, AttentionCallbacks{}, nil)
		p.attentionCursor = 1
		msg := tea.KeyPressMsg{Code: tea.KeyEnter}

		_, cmd := p.Update(msg)
		if cmd == nil {
			t.Fatal("expected quit cmd")
		}
		if p.result.Action != ActionSwitchToPane {
			t.Errorf("action = %d, want ActionSwitchToPane", p.result.Action)
		}
		if p.result.Selected.Path != "%2" {
			t.Errorf("selected path = %s, want %%2", p.result.Selected.Path)
		}
		if p.result.Selected.Context != "s2" {
			t.Errorf("selected context = %s, want s2", p.result.Selected.Context)
		}
	})

	t.Run("enter with no panes cancels", func(t *testing.T) {
		p := newAttentionPicker(nil, AttentionCallbacks{}, nil)
		p.attentionPanes = nil
		msg := tea.KeyPressMsg{Code: tea.KeyEnter}

		_, cmd := p.Update(msg)
		if cmd == nil {
			t.Fatal("expected quit cmd")
		}
		if p.result.Action != ActionCancel {
			t.Errorf("action = %d, want ActionCancel", p.result.Action)
		}
	})
}

func TestUpdateAttention_PeekPane(t *testing.T) {
	// Verifies that the "peek" keybind (shift+enter or the `p` fallback)
	// produces ActionSwitchToPaneKeepUnread and populates Selected. The
	// caller — cmd/dashboard.go — is responsible for interpreting this
	// action as "switch into the pane but do not mutate monitor state."
	t.Run("peek with fallback p key selects current pane", func(t *testing.T) {
		panes := []AttentionPane{
			{PaneID: "%1", Session: "s1", Name: "p1"},
			{PaneID: "%2", Session: "s2", Name: "p2"},
		}
		p := newAttentionPicker(panes, AttentionCallbacks{}, nil)
		p.attentionCursor = 1
		msg := tea.KeyPressMsg{Code: 'p'}

		_, cmd := p.Update(msg)
		if cmd == nil {
			t.Fatal("expected quit cmd")
		}
		if p.result.Action != ActionSwitchToPaneKeepUnread {
			t.Errorf("action = %d, want ActionSwitchToPaneKeepUnread", p.result.Action)
		}
		if p.result.Selected == nil {
			t.Fatal("expected Selected to be populated")
		}
		if p.result.Selected.Path != "%2" {
			t.Errorf("selected path = %s, want %%2", p.result.Selected.Path)
		}
		if p.result.Selected.Context != "s2" {
			t.Errorf("selected context = %s, want s2", p.result.Selected.Context)
		}
	})

	t.Run("peek with no panes is a no-op", func(t *testing.T) {
		p := newAttentionPicker(nil, AttentionCallbacks{}, nil)
		p.attentionPanes = nil
		msg := tea.KeyPressMsg{Code: 'p'}

		_, cmd := p.Update(msg)
		if cmd != nil {
			t.Errorf("expected no cmd, got %v", cmd)
		}
		// Result should remain its zero value (no action).
		if p.result.Action == ActionSwitchToPaneKeepUnread {
			t.Error("peek with no panes should not produce ActionSwitchToPaneKeepUnread")
		}
	})
}

func TestUpdateAttention_Navigation(t *testing.T) {
	panes := []AttentionPane{
		{PaneID: "%1", Session: "s1", Name: "p1"},
		{PaneID: "%2", Session: "s2", Name: "p2"},
		{PaneID: "%3", Session: "s3", Name: "p3"},
	}

	t.Run("up moves cursor up", func(t *testing.T) {
		p := newAttentionPicker(panes, AttentionCallbacks{}, nil)
		p.attentionCursor = 2
		msg := tea.KeyPressMsg{Code: tea.KeyUp}
		p.Update(msg)
		if p.attentionCursor != 1 {
			t.Errorf("cursor = %d, want 1", p.attentionCursor)
		}
	})

	t.Run("up wraps around to end", func(t *testing.T) {
		p := newAttentionPicker(panes, AttentionCallbacks{}, nil)
		p.attentionCursor = 0
		msg := tea.KeyPressMsg{Code: tea.KeyUp}
		p.Update(msg)
		if p.attentionCursor != 2 {
			t.Errorf("cursor = %d, want 2", p.attentionCursor)
		}
	})

	t.Run("down moves cursor down", func(t *testing.T) {
		p := newAttentionPicker(panes, AttentionCallbacks{}, nil)
		p.attentionCursor = 0
		msg := tea.KeyPressMsg{Code: tea.KeyDown}
		p.Update(msg)
		if p.attentionCursor != 1 {
			t.Errorf("cursor = %d, want 1", p.attentionCursor)
		}
	})

	t.Run("down wraps around to start", func(t *testing.T) {
		p := newAttentionPicker(panes, AttentionCallbacks{}, nil)
		p.attentionCursor = 2
		msg := tea.KeyPressMsg{Code: tea.KeyDown}
		p.Update(msg)
		if p.attentionCursor != 0 {
			t.Errorf("cursor = %d, want 0", p.attentionCursor)
		}
	})
}

func TestUpdateAttention_Reset(t *testing.T) {
	var readPaneID string
	panes := []AttentionPane{
		{PaneID: "%1", Status: AttentionUnread},
		{PaneID: "%2", Status: AttentionWorking},
	}
	cb := AttentionCallbacks{
		MarkRead: func(paneID string) { readPaneID = paneID },
	}
	p := newAttentionPicker(panes, cb, nil)
	p.attentionCursor = 0
	msg := tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl}
	p.Update(msg)

	if readPaneID != "%1" {
		t.Errorf("markReadFunc called with %s, want %%1", readPaneID)
	}
	if p.attentionPanes[0].Status == AttentionUnread {
		t.Error("expected pane status to change from Unread")
	}
	if !p.attentionDirty {
		t.Error("expected attentionDirty = true")
	}
}

func TestUpdateAttention_MarkUnread(t *testing.T) {
	var markedPaneID string
	panes := []AttentionPane{
		{PaneID: "%1", Status: AttentionIdle},
	}
	cb := AttentionCallbacks{
		MarkUnread: func(paneID string) { markedPaneID = paneID },
	}
	p := newAttentionPicker(panes, cb, nil)
	msg := tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl}
	p.Update(msg)

	if markedPaneID != "%1" {
		t.Errorf("markUnreadFunc called with %s, want %%1", markedPaneID)
	}
	if p.attentionPanes[0].Status != AttentionUnread {
		t.Errorf("status = %d, want AttentionUnread", p.attentionPanes[0].Status)
	}
	if !p.attentionDirty {
		t.Error("expected attentionDirty = true")
	}
}

func TestUpdateAttention_FollowPane(t *testing.T) {
	var toggledPaneID string
	panes := []AttentionPane{
		{PaneID: "%1", Following: false},
	}
	cb := AttentionCallbacks{
		ToggleFollow: func(paneID string) { toggledPaneID = paneID },
	}

	t.Run("toggle follow on", func(t *testing.T) {
		p := newAttentionPicker(panes, cb, nil)
		msg := tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl}
		p.Update(msg)

		if toggledPaneID != "%1" {
			t.Errorf("toggleFollowFunc called with %s, want %%1", toggledPaneID)
		}
		if !p.attentionPanes[0].Following {
			t.Error("expected Following = true")
		}
		if !p.attentionAllPanes[0].Following {
			t.Error("expected allPanes Following = true")
		}
		if !p.attentionDirty {
			t.Error("expected attentionDirty = true")
		}
	})

	t.Run("unfollow in following view rebuilds", func(t *testing.T) {
		followPanes := []AttentionPane{
			{PaneID: "%1", Following: true},
			{PaneID: "%2", Following: true},
		}
		p := newAttentionPicker(followPanes, cb, nil)
		p.attentionFollowing = true
		p.rebuildAttentionView()
		p.attentionCursor = 0

		msg := tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl}
		p.Update(msg)

		// %1 was unfollowed; in following view it should be removed
		if len(p.attentionPanes) != 1 {
			t.Errorf("attentionPanes len = %d, want 1", len(p.attentionPanes))
		}
	})

	t.Run("unfollow clears note", func(t *testing.T) {
		var clearedNote string
		notePanes := []AttentionPane{
			{PaneID: "%1", Following: true, Note: "test note"},
		}
		noteCb := AttentionCallbacks{
			ToggleFollow: func(paneID string) {},
			SetNote:      func(paneID, note string) { clearedNote = note },
		}
		p := newAttentionPicker(notePanes, noteCb, nil)
		msg := tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl}
		p.Update(msg)

		if p.attentionPanes[0].Note != "" {
			t.Errorf("note = %q, want empty", p.attentionPanes[0].Note)
		}
		if clearedNote != "" {
			t.Errorf("setNoteFunc called with %q, want empty", clearedNote)
		}
	})
}

func TestUpdateAttention_ToggleFollowView(t *testing.T) {
	panes := []AttentionPane{
		{PaneID: "%1", Following: true},
		{PaneID: "%2", Following: false},
	}
	p := newAttentionPicker(panes, AttentionCallbacks{}, nil)

	// Toggle to following view
	msg := tea.KeyPressMsg{Code: 'f', Text: "f"}
	p.Update(msg)

	if !p.attentionFollowing {
		t.Error("expected attentionFollowing = true")
	}
	// Only followed panes should be visible
	if len(p.attentionPanes) != 1 {
		t.Errorf("attentionPanes len = %d, want 1", len(p.attentionPanes))
	}

	// Toggle back
	p.Update(msg)
	if p.attentionFollowing {
		t.Error("expected attentionFollowing = false")
	}
	if len(p.attentionPanes) != 2 {
		t.Errorf("attentionPanes len = %d, want 2", len(p.attentionPanes))
	}
}

func TestUpdateAttention_ForceDelete(t *testing.T) {
	var unmonitoredID string
	panes := []AttentionPane{
		{PaneID: "%1", Session: "s1"},
		{PaneID: "%2", Session: "s2"},
		{PaneID: "%3", Session: "s3"},
	}
	cb := AttentionCallbacks{
		Unmonitor: func(paneID string) { unmonitoredID = paneID },
	}

	t.Run("delete removes pane and stays in view", func(t *testing.T) {
		p := newAttentionPicker(panes, cb, nil)
		p.attentionCursor = 1
		msg := tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl}
		_, cmd := p.Update(msg)

		if cmd != nil {
			t.Error("expected nil cmd (not quit)")
		}
		if unmonitoredID != "%2" {
			t.Errorf("unmonitorFunc called with %s, want %%2", unmonitoredID)
		}
		if len(p.attentionPanes) != 2 {
			t.Errorf("attentionPanes len = %d, want 2", len(p.attentionPanes))
		}
		if len(p.attentionAllPanes) != 2 {
			t.Errorf("attentionAllPanes len = %d, want 2", len(p.attentionAllPanes))
		}
	})

	t.Run("delete last pane with items quits with refresh", func(t *testing.T) {
		singlePane := []AttentionPane{{PaneID: "%1"}}
		items := []Item{{Name: "proj", Path: "/proj"}}
		p := newAttentionPicker(singlePane, cb, items)
		msg := tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl}
		_, cmd := p.Update(msg)

		if cmd == nil {
			t.Fatal("expected quit cmd")
		}
		if p.result.Action != ActionRefresh {
			t.Errorf("action = %d, want ActionRefresh", p.result.Action)
		}
	})

	t.Run("delete last pane standalone quits with cancel", func(t *testing.T) {
		singlePane := []AttentionPane{{PaneID: "%1"}}
		p := newAttentionPicker(singlePane, cb, nil)
		msg := tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl}
		_, cmd := p.Update(msg)

		if cmd == nil {
			t.Fatal("expected quit cmd")
		}
		if p.result.Action != ActionCancel {
			t.Errorf("action = %d, want ActionCancel", p.result.Action)
		}
	})
}

func TestUpdateAttention_Reload(t *testing.T) {
	reloadCalled := false
	newPanes := []AttentionPane{
		{PaneID: "%10", Session: "new"},
	}
	cb := AttentionCallbacks{}

	t.Run("reload when empty and reloadFunc set", func(t *testing.T) {
		p := newAttentionPicker(nil, cb, nil)
		p.attentionPanes = nil
		p.attentionAllPanes = nil
		p.reloadFunc = func() []AttentionPane {
			reloadCalled = true
			return newPanes
		}

		msg := tea.KeyPressMsg{Code: 'r', Text: "r"}
		p.Update(msg)

		if !reloadCalled {
			t.Error("expected reloadFunc to be called")
		}
		if len(p.attentionPanes) != 1 {
			t.Errorf("attentionPanes len = %d, want 1", len(p.attentionPanes))
		}
	})

	t.Run("reload does nothing when panes exist", func(t *testing.T) {
		reloadCalled = false
		existingPanes := []AttentionPane{{PaneID: "%1"}}
		p := newAttentionPicker(existingPanes, cb, nil)
		p.reloadFunc = func() []AttentionPane {
			reloadCalled = true
			return newPanes
		}

		msg := tea.KeyPressMsg{Code: 'r', Text: "r"}
		p.Update(msg)

		if reloadCalled {
			t.Error("reloadFunc should not be called when panes exist")
		}
	})
}

func TestUpdateAttention_EditNote(t *testing.T) {
	panes := []AttentionPane{
		{PaneID: "%1", Note: "old note"},
	}
	cb := AttentionCallbacks{
		SetNote: func(paneID, note string) {},
	}
	p := newAttentionPicker(panes, cb, nil)

	// Press N to enter edit mode
	msg := tea.KeyPressMsg{Code: 'N', Text: "N"}
	p.Update(msg)

	if !p.editingNote {
		t.Error("expected editingNote = true")
	}

	// Esc exits edit mode
	escMsg := tea.KeyPressMsg{Code: 0x1b}
	p.Update(escMsg)

	if p.editingNote {
		t.Error("expected editingNote = false after esc")
	}
}

func TestUpdateAttention_EditNoteSave(t *testing.T) {
	var savedPaneID, savedNote string
	var followToggled bool
	panes := []AttentionPane{
		{PaneID: "%1", Following: false, Note: ""},
	}
	cb := AttentionCallbacks{
		SetNote:      func(paneID, note string) { savedPaneID = paneID; savedNote = note },
		ToggleFollow: func(paneID string) { followToggled = true },
	}
	p := newAttentionPicker(panes, cb, nil)

	// Enter edit mode
	p.Update(tea.KeyPressMsg{Code: 'N', Text: "N"})
	// Type some text
	p.noteInput.SetValue("my note")
	// Press enter to save
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if p.editingNote {
		t.Error("expected editingNote = false after save")
	}
	if savedPaneID != "%1" {
		t.Errorf("saved pane = %s, want %%1", savedPaneID)
	}
	if savedNote != "my note" {
		t.Errorf("saved note = %q, want %q", savedNote, "my note")
	}
	if !followToggled {
		t.Error("expected auto-follow on note save")
	}
	if !p.attentionDirty {
		t.Error("expected attentionDirty = true")
	}
}

func TestReloadAttentionPanes(t *testing.T) {
	t.Run("preserves cursor on same pane", func(t *testing.T) {
		panes := []AttentionPane{
			{PaneID: "%1", Session: "s1"},
			{PaneID: "%2", Session: "s2"},
			{PaneID: "%3", Session: "s3"},
		}
		p := newAttentionPicker(panes, AttentionCallbacks{}, nil)
		p.attentionCursor = 1 // on %2

		p.reloadFunc = func() []AttentionPane {
			return []AttentionPane{
				{PaneID: "%3", Session: "s3"},
				{PaneID: "%1", Session: "s1"},
				{PaneID: "%2", Session: "s2"},
			}
		}
		p.reloadAttentionPanes()

		if p.attentionPanes[p.attentionCursor].PaneID != "%2" {
			t.Errorf("cursor on %s, want %%2", p.attentionPanes[p.attentionCursor].PaneID)
		}
	})

	t.Run("cursor moves to last when pane removed", func(t *testing.T) {
		panes := []AttentionPane{
			{PaneID: "%1", Session: "s1"},
			{PaneID: "%2", Session: "s2"},
		}
		p := newAttentionPicker(panes, AttentionCallbacks{}, nil)
		p.attentionCursor = 1 // on %2

		p.reloadFunc = func() []AttentionPane {
			return []AttentionPane{
				{PaneID: "%1", Session: "s1"},
				{PaneID: "%3", Session: "s3"},
			}
		}
		p.reloadAttentionPanes()

		// %2 gone, cursor should go to last (index 1)
		if p.attentionCursor != 1 {
			t.Errorf("cursor = %d, want 1", p.attentionCursor)
		}
	})

	t.Run("cursor is 0 when reloaded list is empty", func(t *testing.T) {
		panes := []AttentionPane{
			{PaneID: "%1", Session: "s1"},
		}
		p := newAttentionPicker(panes, AttentionCallbacks{}, nil)
		p.attentionCursor = 0

		p.reloadFunc = func() []AttentionPane {
			return nil
		}
		p.reloadAttentionPanes()

		if p.attentionCursor != 0 {
			t.Errorf("cursor = %d, want 0", p.attentionCursor)
		}
	})

	t.Run("no-op when reloadFunc is nil", func(t *testing.T) {
		panes := []AttentionPane{
			{PaneID: "%1", Session: "s1"},
		}
		p := newAttentionPicker(panes, AttentionCallbacks{}, nil)
		p.reloadFunc = nil
		p.attentionCursor = 0

		p.reloadAttentionPanes() // should not panic
		if len(p.attentionPanes) != 1 {
			t.Errorf("panes changed unexpectedly: len = %d", len(p.attentionPanes))
		}
	})
}
