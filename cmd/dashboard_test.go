package cmd

import (
	"testing"
	"time"

	"github.com/glebglazov/pop/history"
)

func TestPathBase(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "regular path",
			path:     "/home/user/project",
			expected: "project",
		},
		{
			name:     "tmux prefix path without slash",
			path:     "tmux:scratch",
			expected: "tmux:scratch",
		},
		{
			name:     "tmux prefix path with slash",
			path:     "tmux:project/worktree",
			expected: "worktree",
		},
		{
			name:     "no slashes",
			path:     "standalone",
			expected: "standalone",
		},
		{
			name:     "trailing slash stripped externally",
			path:     "/a/b/c",
			expected: "c",
		},
		{
			name:     "single segment with leading slash",
			path:     "/root",
			expected: "root",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := pathBase(tt.path)
			if result != tt.expected {
				t.Errorf("pathBase(%q) = %q, want %q", tt.path, result, tt.expected)
			}
		})
	}
}

func TestSessionAccessTime(t *testing.T) {
	now := time.Now()
	hist := &history.History{
		Entries: []history.Entry{
			{Path: "/home/user/my.project", LastAccess: now.Add(-2 * time.Hour)},
			{Path: "/home/user/game_server", LastAccess: now.Add(-1 * time.Hour)},
		},
	}

	tests := []struct {
		name     string
		session  string
		hist     *history.History
		expected int64
	}{
		{
			name:     "exact match via sanitized base",
			session:  "my_project", // sanitize turns . into _
			hist:     hist,
			expected: now.Add(-2 * time.Hour).Unix(),
		},
		{
			name:     "exact match without sanitization",
			session:  "game_server",
			hist:     hist,
			expected: now.Add(-1 * time.Hour).Unix(),
		},
		{
			name:     "no match returns zero",
			session:  "nonexistent",
			hist:     hist,
			expected: 0,
		},
		{
			name:     "nil history returns zero",
			session:  "anything",
			hist:     nil,
			expected: 0,
		},
		{
			name:     "worktree session partial match",
			session:  "repo/game_server",
			hist:     hist,
			expected: now.Add(-1 * time.Hour).Unix(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sessionAccessTime(tt.session, tt.hist, nil)
			if result != tt.expected {
				t.Errorf("sessionAccessTime(%q) = %d, want %d", tt.session, result, tt.expected)
			}
		})
	}
}
