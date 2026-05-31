package monitor

import (
	"fmt"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

func TestTmuxPaneInfo(t *testing.T) {
	t.Run("parses tab-separated session and command", func(t *testing.T) {
		tmux := &deps.MockTmux{
			CommandFunc: func(args ...string) (string, error) {
				if args[0] == "display-message" {
					return "project-a\topencode", nil
				}
				return "", nil
			},
		}
		session, cmdName, err := TmuxPaneInfo(tmux, "%1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if session != "project-a" {
			t.Errorf("session = %q, want %q", session, "project-a")
		}
		if cmdName != "opencode" {
			t.Errorf("cmdName = %q, want %q", cmdName, "opencode")
		}
	})

	t.Run("propagates tmux errors", func(t *testing.T) {
		tmux := &deps.MockTmux{
			CommandFunc: func(args ...string) (string, error) {
				return "", fmt.Errorf("pane not found")
			},
		}
		_, _, err := TmuxPaneInfo(tmux, "%nope")
		if err == nil {
			t.Error("expected error, got nil")
		}
	})

	t.Run("errors on malformed output", func(t *testing.T) {
		tmux := &deps.MockTmux{
			CommandFunc: func(args ...string) (string, error) {
				return "no-tab-here", nil
			},
		}
		_, _, err := TmuxPaneInfo(tmux, "%1")
		if err == nil {
			t.Error("expected error on malformed output, got nil")
		}
	})
}

func TestIsActiveTmuxPane(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		err      error
		expected bool
	}{
		{
			name:     "active pane",
			output:   "1 1 1",
			expected: true,
		},
		{
			name:     "inactive pane",
			output:   "0 1 1",
			expected: false,
		},
		{
			name:     "detached session",
			output:   "1 1 0",
			expected: false,
		},
		{
			name:     "error returns false",
			err:      fmt.Errorf("pane not found"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := &deps.MockTmux{
				CommandFunc: func(args ...string) (string, error) {
					return tt.output, tt.err
				},
			}
			result := IsActiveTmuxPane(tmux, "%1")
			if result != tt.expected {
				t.Errorf("got %v, want %v", result, tt.expected)
			}
		})
	}
}
