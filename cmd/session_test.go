package cmd

import (
	"testing"

	"github.com/glebglazov/pop/ui"
)

func TestIsStandaloneSession(t *testing.T) {
	tests := []struct {
		name     string
		item     ui.Item
		expected bool
	}{
		{
			name:     "standalone session",
			item:     ui.Item{Path: "tmux:scratch"},
			expected: true,
		},
		{
			name:     "standalone session with slashes in name",
			item:     ui.Item{Path: "tmux:my/session"},
			expected: true,
		},
		{
			name:     "directory project",
			item:     ui.Item{Path: "/home/user/project"},
			expected: false,
		},
		{
			name:     "empty path",
			item:     ui.Item{Path: ""},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isStandaloneSession(tt.item)
			if result != tt.expected {
				t.Errorf("isStandaloneSession(%q) = %v, want %v", tt.item.Path, result, tt.expected)
			}
		})
	}
}
