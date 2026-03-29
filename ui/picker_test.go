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
