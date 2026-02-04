package project

import (
	"testing"
)

func TestParseWorktrees(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []Worktree
	}{
		{
			name:     "empty input",
			input:    "",
			expected: nil,
		},
		{
			name: "single worktree with branch",
			input: `worktree /path/to/main
branch refs/heads/master

`,
			expected: []Worktree{
				{Name: "main", Path: "/path/to/main", Branch: "master"},
			},
		},
		{
			name: "multiple worktrees",
			input: `worktree /projects/repo/main
branch refs/heads/master

worktree /projects/repo/feature
branch refs/heads/feature-branch

worktree /projects/repo/hotfix
branch refs/heads/hotfix-123

`,
			expected: []Worktree{
				{Name: "main", Path: "/projects/repo/main", Branch: "master"},
				{Name: "feature", Path: "/projects/repo/feature", Branch: "feature-branch"},
				{Name: "hotfix", Path: "/projects/repo/hotfix", Branch: "hotfix-123"},
			},
		},
		{
			name: "detached HEAD",
			input: `worktree /path/to/detached
detached

`,
			expected: []Worktree{
				{Name: "detached", Path: "/path/to/detached", Branch: "detached"},
			},
		},
		{
			name: "filters out .bare directory",
			input: `worktree /projects/repo/.bare
bare

worktree /projects/repo/main
branch refs/heads/master

`,
			expected: []Worktree{
				{Name: "main", Path: "/projects/repo/main", Branch: "master"},
			},
		},
		{
			name: "mixed worktrees with bare and detached",
			input: `worktree /projects/annual_calendar/.bare
bare

worktree /projects/annual_calendar/alfa
branch refs/heads/alfa

worktree /projects/annual_calendar/bravo
branch refs/heads/bravo

worktree /projects/annual_calendar/delta
detached

`,
			expected: []Worktree{
				{Name: "alfa", Path: "/projects/annual_calendar/alfa", Branch: "alfa"},
				{Name: "bravo", Path: "/projects/annual_calendar/bravo", Branch: "bravo"},
				{Name: "delta", Path: "/projects/annual_calendar/delta", Branch: "detached"},
			},
		},
		{
			name: "no trailing newline",
			input: `worktree /path/to/main
branch refs/heads/master`,
			expected: []Worktree{
				{Name: "main", Path: "/path/to/main", Branch: "master"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseWorktrees(tt.input)

			if len(result) != len(tt.expected) {
				t.Errorf("expected %d worktrees, got %d", len(tt.expected), len(result))
				return
			}

			for i, wt := range result {
				if wt.Name != tt.expected[i].Name {
					t.Errorf("worktree[%d].Name = %q, want %q", i, wt.Name, tt.expected[i].Name)
				}
				if wt.Path != tt.expected[i].Path {
					t.Errorf("worktree[%d].Path = %q, want %q", i, wt.Path, tt.expected[i].Path)
				}
				if wt.Branch != tt.expected[i].Branch {
					t.Errorf("worktree[%d].Branch = %q, want %q", i, wt.Branch, tt.expected[i].Branch)
				}
			}
		})
	}
}

func TestTmuxSessionName(t *testing.T) {
	tests := []struct {
		name         string
		ctx          *RepoContext
		worktreeName string
		expected     string
	}{
		{
			name:         "bare repo with worktree",
			ctx:          &RepoContext{RepoName: "myproject", IsBare: true},
			worktreeName: "main",
			expected:     "myproject/main",
		},
		{
			name:         "regular repo",
			ctx:          &RepoContext{RepoName: "myproject", IsBare: false},
			worktreeName: "main",
			expected:     "main",
		},
		{
			name:         "sanitizes dots",
			ctx:          &RepoContext{RepoName: "my.project", IsBare: true},
			worktreeName: "feature.1",
			expected:     "my_project/feature_1",
		},
		{
			name:         "sanitizes colons",
			ctx:          &RepoContext{RepoName: "project:v1", IsBare: true},
			worktreeName: "fix:bug",
			expected:     "project_v1/fix_bug",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TmuxSessionName(tt.ctx, tt.worktreeName)
			if result != tt.expected {
				t.Errorf("TmuxSessionName() = %q, want %q", result, tt.expected)
			}
		})
	}
}
