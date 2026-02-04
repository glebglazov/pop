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

func TestBuildHintsWithCustomCommands(t *testing.T) {
	commands := []CustomCommand{
		{Key: "ctrl-l", Label: "cleanup", Command: "echo", Exit: true},
	}

	items := []Item{{Name: "test", Path: "/test"}}
	picker := NewPicker(items, WithCustomCommands(commands))

	hints := picker.buildHints()

	// Should contain the custom command hint
	if !containsSubstring(hints, "C-l cleanup") {
		t.Errorf("hints should contain custom command, got: %q", hints)
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
