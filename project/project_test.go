package project

import (
	"fmt"
	"os"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
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

func TestDetectRepoContextWith_BareRepo(t *testing.T) {
	d := &Deps{
		Git: &deps.MockGit{
			CommandFunc: func(args ...string) (string, error) {
				// rev-parse --git-common-dir fails (not in worktree)
				return "", fmt.Errorf("not in git dir")
			},
			CommandInDirFunc: func(dir string, args ...string) (string, error) {
				if len(args) >= 2 && args[0] == "config" && args[1] == "--get" {
					return "true", nil
				}
				return "", nil
			},
		},
		FS: &deps.MockFileSystem{
			GetwdFunc: func() (string, error) {
				return "/projects/myrepo", nil
			},
			StatFunc: func(path string) (os.FileInfo, error) {
				if path == "/projects/myrepo/.git" {
					return deps.MockFileInfo{IsDirVal: true}, nil
				}
				return nil, os.ErrNotExist
			},
		},
	}

	ctx, err := DetectRepoContextWith(d)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ctx.IsBare {
		t.Error("expected IsBare to be true")
	}
	if ctx.RepoName != "myrepo" {
		t.Errorf("expected RepoName 'myrepo', got %q", ctx.RepoName)
	}
}

func TestDetectRepoContextWith_RegularRepo(t *testing.T) {
	d := &Deps{
		Git: &deps.MockGit{
			CommandFunc: func(args ...string) (string, error) {
				if len(args) >= 2 && args[0] == "rev-parse" {
					if args[1] == "--git-common-dir" {
						return ".git", nil // not a bare repo worktree
					}
					if args[1] == "--show-toplevel" {
						return "/projects/regular-repo", nil
					}
				}
				return "", nil
			},
			CommandInDirFunc: func(dir string, args ...string) (string, error) {
				// core.bare check returns false
				return "false", nil
			},
		},
		FS: &deps.MockFileSystem{
			GetwdFunc: func() (string, error) {
				return "/projects/regular-repo", nil
			},
			StatFunc: func(path string) (os.FileInfo, error) {
				return nil, os.ErrNotExist
			},
		},
	}

	ctx, err := DetectRepoContextWith(d)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ctx.IsBare {
		t.Error("expected IsBare to be false")
	}
	if ctx.RepoName != "regular-repo" {
		t.Errorf("expected RepoName 'regular-repo', got %q", ctx.RepoName)
	}
}

func TestHasWorktreesWith(t *testing.T) {
	tests := []struct {
		name     string
		setupFS  func() *deps.MockFileSystem
		expected bool
	}{
		{
			name: "has .bare directory",
			setupFS: func() *deps.MockFileSystem {
				return &deps.MockFileSystem{
					StatFunc: func(path string) (os.FileInfo, error) {
						if path == "/project/.bare" {
							return deps.MockFileInfo{IsDirVal: true}, nil
						}
						return nil, os.ErrNotExist
					},
				}
			},
			expected: true,
		},
		{
			name: "has .git dir with worktrees and core.bare=true",
			setupFS: func() *deps.MockFileSystem {
				return &deps.MockFileSystem{
					StatFunc: func(path string) (os.FileInfo, error) {
						switch path {
						case "/project/.bare":
							return nil, os.ErrNotExist
						case "/project/.git":
							return deps.MockFileInfo{IsDirVal: true}, nil
						case "/project/.git/worktrees":
							return deps.MockFileInfo{IsDirVal: true}, nil
						}
						return nil, os.ErrNotExist
					},
					ReadDirFunc: func(path string) ([]os.DirEntry, error) {
						if path == "/project/.git/worktrees" {
							return []os.DirEntry{
								deps.MockDirEntry{NameVal: "main", IsDirVal: true},
							}, nil
						}
						return nil, nil
					},
					ReadFileFunc: func(path string) ([]byte, error) {
						if path == "/project/.git/config" {
							return []byte("[core]\n\tbare = true\n"), nil
						}
						return nil, os.ErrNotExist
					},
				}
			},
			expected: true,
		},
		{
			name: "has .git dir but core.bare=false",
			setupFS: func() *deps.MockFileSystem {
				return &deps.MockFileSystem{
					StatFunc: func(path string) (os.FileInfo, error) {
						switch path {
						case "/project/.bare":
							return nil, os.ErrNotExist
						case "/project/.git":
							return deps.MockFileInfo{IsDirVal: true}, nil
						}
						return nil, os.ErrNotExist
					},
					ReadFileFunc: func(path string) ([]byte, error) {
						if path == "/project/.git/config" {
							return []byte("[core]\n\tbare = false\n"), nil
						}
						return nil, os.ErrNotExist
					},
				}
			},
			expected: false,
		},
		{
			name: "no .bare or .git directory",
			setupFS: func() *deps.MockFileSystem {
				return &deps.MockFileSystem{
					StatFunc: func(path string) (os.FileInfo, error) {
						return nil, os.ErrNotExist
					},
				}
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Deps{
				FS:  tt.setupFS(),
				Git: &deps.MockGit{},
			}

			result := HasWorktreesWith(d, "/project")

			if result != tt.expected {
				t.Errorf("HasWorktreesWith() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestListWorktreesForPathWith(t *testing.T) {
	tests := []struct {
		name     string
		setupFS  func() *deps.MockFileSystem
		expected []Worktree
		wantErr  bool
	}{
		{
			name: "finds worktrees by .git file",
			setupFS: func() *deps.MockFileSystem {
				return &deps.MockFileSystem{
					ReadDirFunc: func(path string) ([]os.DirEntry, error) {
						return []os.DirEntry{
							deps.MockDirEntry{NameVal: "main", IsDirVal: true},
							deps.MockDirEntry{NameVal: "feature", IsDirVal: true},
							deps.MockDirEntry{NameVal: ".bare", IsDirVal: true},
							deps.MockDirEntry{NameVal: ".git", IsDirVal: true},
							deps.MockDirEntry{NameVal: "README.md", IsDirVal: false},
						}, nil
					},
					StatFunc: func(path string) (os.FileInfo, error) {
						// .git files in worktrees (not directories)
						if path == "/project/main/.git" || path == "/project/feature/.git" {
							return deps.MockFileInfo{IsDirVal: false}, nil
						}
						return nil, os.ErrNotExist
					},
				}
			},
			expected: []Worktree{
				{Name: "main", Path: "/project/main"},
				{Name: "feature", Path: "/project/feature"},
			},
		},
		{
			name: "empty directory",
			setupFS: func() *deps.MockFileSystem {
				return &deps.MockFileSystem{
					ReadDirFunc: func(path string) ([]os.DirEntry, error) {
						return nil, nil
					},
				}
			},
			expected: nil,
		},
		{
			name: "directory read error",
			setupFS: func() *deps.MockFileSystem {
				return &deps.MockFileSystem{
					ReadDirFunc: func(path string) ([]os.DirEntry, error) {
						return nil, os.ErrPermission
					},
				}
			},
			expected: nil,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Deps{
				FS:  tt.setupFS(),
				Git: &deps.MockGit{},
			}

			result, err := ListWorktreesForPathWith(d, "/project")

			if (err != nil) != tt.wantErr {
				t.Errorf("ListWorktreesForPathWith() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if len(result) != len(tt.expected) {
				t.Errorf("got %d worktrees, want %d", len(result), len(tt.expected))
				return
			}

			for i, wt := range result {
				if wt.Name != tt.expected[i].Name || wt.Path != tt.expected[i].Path {
					t.Errorf("worktree[%d] = %+v, want %+v", i, wt, tt.expected[i])
				}
			}
		})
	}
}

func TestListWorktreesWith(t *testing.T) {
	tests := []struct {
		name      string
		gitOutput string
		gitErr    error
		expected  []Worktree
		wantErr   bool
	}{
		{
			name: "parses worktrees correctly",
			gitOutput: `worktree /projects/repo/main
branch refs/heads/master

worktree /projects/repo/feature
branch refs/heads/feature-branch

`,
			expected: []Worktree{
				{Name: "main", Path: "/projects/repo/main", Branch: "master"},
				{Name: "feature", Path: "/projects/repo/feature", Branch: "feature-branch"},
			},
		},
		{
			name:     "git command fails",
			gitErr:   fmt.Errorf("git error"),
			expected: nil,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Deps{
				Git: &deps.MockGit{
					CommandInDirFunc: func(dir string, args ...string) (string, error) {
						return tt.gitOutput, tt.gitErr
					},
				},
				FS: &deps.MockFileSystem{},
			}

			ctx := &RepoContext{GitRoot: "/projects/repo"}
			result, err := ListWorktreesWith(d, ctx)

			if (err != nil) != tt.wantErr {
				t.Errorf("ListWorktreesWith() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if len(result) != len(tt.expected) {
				t.Errorf("got %d worktrees, want %d", len(result), len(tt.expected))
				return
			}

			for i, wt := range result {
				if wt != tt.expected[i] {
					t.Errorf("worktree[%d] = %+v, want %+v", i, wt, tt.expected[i])
				}
			}
		})
	}
}
