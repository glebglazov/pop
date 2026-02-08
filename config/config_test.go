package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

func TestDefaultConfigPathWith(t *testing.T) {
	tests := []struct {
		name     string
		xdgHome  string
		userHome string
		expected string
	}{
		{
			name:     "uses XDG_CONFIG_HOME when set",
			xdgHome:  "/custom/config",
			userHome: "/home/user",
			expected: "/custom/config/pop/config.toml",
		},
		{
			name:     "falls back to ~/.config when XDG not set",
			xdgHome:  "",
			userHome: "/home/user",
			expected: "/home/user/.config/pop/config.toml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Deps{
				FS: &deps.MockFileSystem{
					GetenvFunc: func(key string) string {
						if key == "XDG_CONFIG_HOME" {
							return tt.xdgHome
						}
						return ""
					},
					UserHomeDirFunc: func() (string, error) {
						return tt.userHome, nil
					},
				},
			}

			result := DefaultConfigPathWith(d)

			if result != tt.expected {
				t.Errorf("DefaultConfigPathWith() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestExpandProjectsWith(t *testing.T) {
	tests := []struct {
		name     string
		projects []string
		setupFS  func() *deps.MockFileSystem
		expected []ExpandedPath
	}{
		{
			name:     "expands home directory",
			projects: []string{"~/projects/myapp"},
			setupFS: func() *deps.MockFileSystem {
				return &deps.MockFileSystem{
					UserHomeDirFunc: func() (string, error) {
						return "/home/user", nil
					},
					StatFunc: func(path string) (os.FileInfo, error) {
						if path == "/home/user/projects/myapp" {
							return deps.MockFileInfo{IsDirVal: true}, nil
						}
						return nil, os.ErrNotExist
					},
				}
			},
			expected: []ExpandedPath{{Path: "/home/user/projects/myapp", GlobSegments: 0}},
		},
		{
			name:     "filters non-directories",
			projects: []string{"/projects/file.txt", "/projects/dir"},
			setupFS: func() *deps.MockFileSystem {
				return &deps.MockFileSystem{
					StatFunc: func(path string) (os.FileInfo, error) {
						if path == "/projects/dir" {
							return deps.MockFileInfo{IsDirVal: true}, nil
						}
						if path == "/projects/file.txt" {
							return deps.MockFileInfo{IsDirVal: false}, nil
						}
						return nil, os.ErrNotExist
					},
				}
			},
			expected: []ExpandedPath{{Path: "/projects/dir", GlobSegments: 0}},
		},
		{
			name:     "deduplicates paths",
			projects: []string{"/projects/app", "/projects/app"},
			setupFS: func() *deps.MockFileSystem {
				return &deps.MockFileSystem{
					StatFunc: func(path string) (os.FileInfo, error) {
						return deps.MockFileInfo{IsDirVal: true}, nil
					},
				}
			},
			expected: []ExpandedPath{{Path: "/projects/app", GlobSegments: 0}},
		},
		{
			name:     "handles non-existent paths",
			projects: []string{"/projects/nonexistent"},
			setupFS: func() *deps.MockFileSystem {
				return &deps.MockFileSystem{
					StatFunc: func(path string) (os.FileInfo, error) {
						return nil, os.ErrNotExist
					},
				}
			},
			expected: nil,
		},
		{
			name:     "resolves symlinks to canonical paths",
			projects: []string{"/symlink/project"},
			setupFS: func() *deps.MockFileSystem {
				return &deps.MockFileSystem{
					EvalSymlinksFunc: func(path string) (string, error) {
						if path == "/symlink/project" {
							return "/real/project", nil
						}
						return path, nil
					},
					StatFunc: func(path string) (os.FileInfo, error) {
						if path == "/real/project" {
							return deps.MockFileInfo{IsDirVal: true}, nil
						}
						return nil, os.ErrNotExist
					},
				}
			},
			expected: []ExpandedPath{{Path: "/real/project", GlobSegments: 0}},
		},
		{
			name:     "deduplicates symlinks pointing to same path",
			projects: []string{"/symlink1/project", "/symlink2/project"},
			setupFS: func() *deps.MockFileSystem {
				return &deps.MockFileSystem{
					EvalSymlinksFunc: func(path string) (string, error) {
						// Both symlinks resolve to the same real path
						if path == "/symlink1/project" || path == "/symlink2/project" {
							return "/real/project", nil
						}
						return path, nil
					},
					StatFunc: func(path string) (os.FileInfo, error) {
						if path == "/real/project" {
							return deps.MockFileInfo{IsDirVal: true}, nil
						}
						return nil, os.ErrNotExist
					},
				}
			},
			expected: []ExpandedPath{{Path: "/real/project", GlobSegments: 0}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Deps{FS: tt.setupFS()}
			cfg := &Config{Projects: tt.projects}

			result, err := cfg.ExpandProjectsWith(d)

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(result) != len(tt.expected) {
				t.Errorf("got %d projects, want %d: %v", len(result), len(tt.expected), result)
				return
			}

			for i, p := range result {
				if p.Path != tt.expected[i].Path {
					t.Errorf("project[%d].Path = %q, want %q", i, p.Path, tt.expected[i].Path)
				}
				if p.GlobSegments != tt.expected[i].GlobSegments {
					t.Errorf("project[%d].GlobSegments = %d, want %d", i, p.GlobSegments, tt.expected[i].GlobSegments)
				}
			}
		})
	}
}

func TestExpandHomeWith(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		home     string
		expected string
	}{
		{
			name:     "expands tilde prefix",
			path:     "~/projects",
			home:     "/home/user",
			expected: "/home/user/projects",
		},
		{
			name:     "leaves absolute path unchanged",
			path:     "/absolute/path",
			home:     "/home/user",
			expected: "/absolute/path",
		},
		{
			name:     "leaves relative path unchanged",
			path:     "relative/path",
			home:     "/home/user",
			expected: "relative/path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Deps{
				FS: &deps.MockFileSystem{
					UserHomeDirFunc: func() (string, error) {
						return tt.home, nil
					},
				},
			}

			result := expandHomeWith(d, tt.path)

			if result != tt.expected {
				t.Errorf("expandHomeWith() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestLoadWorktreeCommands(t *testing.T) {
	tests := []struct {
		name           string
		toml           string
		expectedCmds   int
		checkFirstCmd  func(t *testing.T, cmd WorktreeCommand)
	}{
		{
			name: "loads single worktree command",
			toml: `
projects = ["~/Dev"]

[[worktree.commands]]
key = "ctrl-l"
label = "cleanup"
command = "echo cleanup"
exit = true
`,
			expectedCmds: 1,
			checkFirstCmd: func(t *testing.T, cmd WorktreeCommand) {
				if cmd.Key != "ctrl-l" {
					t.Errorf("Key = %q, want %q", cmd.Key, "ctrl-l")
				}
				if cmd.Label != "cleanup" {
					t.Errorf("Label = %q, want %q", cmd.Label, "cleanup")
				}
				if cmd.Command != "echo cleanup" {
					t.Errorf("Command = %q, want %q", cmd.Command, "echo cleanup")
				}
				if !cmd.Exit {
					t.Error("Exit = false, want true")
				}
			},
		},
		{
			name: "loads multiple worktree commands",
			toml: `
projects = ["~/Dev"]

[[worktree.commands]]
key = "ctrl-l"
label = "cleanup"
command = "echo cleanup"
exit = true

[[worktree.commands]]
key = "ctrl-o"
label = "open"
command = "echo open"
exit = false
`,
			expectedCmds: 2,
			checkFirstCmd: func(t *testing.T, cmd WorktreeCommand) {
				if cmd.Key != "ctrl-l" {
					t.Errorf("Key = %q, want %q", cmd.Key, "ctrl-l")
				}
			},
		},
		{
			name: "config without worktree section",
			toml: `
projects = ["~/Dev"]
`,
			expectedCmds: 0,
			checkFirstCmd: nil,
		},
		{
			name: "exit defaults to false",
			toml: `
projects = ["~/Dev"]

[[worktree.commands]]
key = "ctrl-t"
label = "test"
command = "echo test"
`,
			expectedCmds: 1,
			checkFirstCmd: func(t *testing.T, cmd WorktreeCommand) {
				if cmd.Exit {
					t.Error("Exit = true, want false (default)")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp config file
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "config.toml")
			if err := os.WriteFile(configPath, []byte(tt.toml), 0644); err != nil {
				t.Fatalf("failed to write temp config: %v", err)
			}

			cfg, err := Load(configPath)
			if err != nil {
				t.Fatalf("Load() error: %v", err)
			}

			// Check number of commands
			var cmdCount int
			if cfg.Worktree != nil {
				cmdCount = len(cfg.Worktree.Commands)
			}
			if cmdCount != tt.expectedCmds {
				t.Errorf("got %d commands, want %d", cmdCount, tt.expectedCmds)
			}

			// Check first command if expected
			if tt.checkFirstCmd != nil && cmdCount > 0 {
				tt.checkFirstCmd(t, cfg.Worktree.Commands[0])
			}
		})
	}
}

func TestUseGlobSegments(t *testing.T) {
	tests := []struct {
		name     string
		toml     string
		expected bool
	}{
		{
			name:     "defaults to true when not set",
			toml:     `projects = ["~/Dev"]`,
			expected: true,
		},
		{
			name:     "explicit true",
			toml:     "projects = [\"~/Dev\"]\nuse_glob_segments_in_display_path = true",
			expected: true,
		},
		{
			name:     "explicit false",
			toml:     "projects = [\"~/Dev\"]\nuse_glob_segments_in_display_path = false",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "config.toml")
			if err := os.WriteFile(configPath, []byte(tt.toml), 0644); err != nil {
				t.Fatalf("failed to write config: %v", err)
			}
			cfg, err := Load(configPath)
			if err != nil {
				t.Fatalf("Load() error: %v", err)
			}
			if cfg.UseGlobSegments() != tt.expected {
				t.Errorf("UseGlobSegments() = %v, want %v", cfg.UseGlobSegments(), tt.expected)
			}
		})
	}
}

func TestGetDisambiguationStrategy(t *testing.T) {
	tests := []struct {
		name     string
		toml     string
		expected string
	}{
		{
			name:     "defaults to first_unique_segment when not set",
			toml:     `projects = ["~/Dev"]`,
			expected: "first_unique_segment",
		},
		{
			name:     "explicit first_unique_segment",
			toml:     "projects = [\"~/Dev\"]\ndisambiguation_strategy = \"first_unique_segment\"",
			expected: "first_unique_segment",
		},
		{
			name:     "explicit full_path",
			toml:     "projects = [\"~/Dev\"]\ndisambiguation_strategy = \"full_path\"",
			expected: "full_path",
		},
		{
			name:     "invalid value defaults to first_unique_segment",
			toml:     "projects = [\"~/Dev\"]\ndisambiguation_strategy = \"bogus\"",
			expected: "first_unique_segment",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "config.toml")
			if err := os.WriteFile(configPath, []byte(tt.toml), 0644); err != nil {
				t.Fatalf("failed to write config: %v", err)
			}
			cfg, err := Load(configPath)
			if err != nil {
				t.Fatalf("Load() error: %v", err)
			}
			if cfg.GetDisambiguationStrategy() != tt.expected {
				t.Errorf("GetDisambiguationStrategy() = %q, want %q", cfg.GetDisambiguationStrategy(), tt.expected)
			}
		})
	}
}

func TestExpandProjectsGlobSegments(t *testing.T) {
	// Test that glob patterns produce correct GlobSegments count.
	// This test uses the real filesystem with temp directories.
	tmpDir := t.TempDir()

	// Create: tmpDir/work/app, tmpDir/personal/app
	os.MkdirAll(filepath.Join(tmpDir, "work", "app"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "personal", "app"), 0755)

	cfg := &Config{Projects: []string{filepath.Join(tmpDir, "*", "*")}}
	result, err := cfg.ExpandProjects()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("got %d projects, want 2: %v", len(result), result)
	}

	for _, ep := range result {
		if ep.GlobSegments != 2 {
			t.Errorf("path %q: GlobSegments = %d, want 2", ep.Path, ep.GlobSegments)
		}
	}
}
