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
		projects []ProjectEntry
		setupFS  func() *deps.MockFileSystem
		expected []ExpandedPath
	}{
		{
			name:     "expands home directory",
			projects: []ProjectEntry{{Path: "~/projects/myapp"}},
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
			expected: []ExpandedPath{{Path: "/home/user/projects/myapp", DisplayDepth: 1}},
		},
		{
			name:     "filters non-directories",
			projects: []ProjectEntry{{Path: "/projects/file.txt"}, {Path: "/projects/dir"}},
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
			expected: []ExpandedPath{{Path: "/projects/dir", DisplayDepth: 1}},
		},
		{
			name:     "deduplicates paths",
			projects: []ProjectEntry{{Path: "/projects/app"}, {Path: "/projects/app"}},
			setupFS: func() *deps.MockFileSystem {
				return &deps.MockFileSystem{
					StatFunc: func(path string) (os.FileInfo, error) {
						return deps.MockFileInfo{IsDirVal: true}, nil
					},
				}
			},
			expected: []ExpandedPath{{Path: "/projects/app", DisplayDepth: 1}},
		},
		{
			name:     "handles non-existent paths",
			projects: []ProjectEntry{{Path: "/projects/nonexistent"}},
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
			projects: []ProjectEntry{{Path: "/symlink/project"}},
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
			expected: []ExpandedPath{{Path: "/real/project", DisplayDepth: 1}},
		},
		{
			name:     "deduplicates symlinks pointing to same path",
			projects: []ProjectEntry{{Path: "/symlink1/project"}, {Path: "/symlink2/project"}},
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
			expected: []ExpandedPath{{Path: "/real/project", DisplayDepth: 1}},
		},
		{
			name:     "propagates display_depth",
			projects: []ProjectEntry{{Path: "/projects/app", DisplayDepth: 3}},
			setupFS: func() *deps.MockFileSystem {
				return &deps.MockFileSystem{
					StatFunc: func(path string) (os.FileInfo, error) {
						return deps.MockFileInfo{IsDirVal: true}, nil
					},
				}
			},
			expected: []ExpandedPath{{Path: "/projects/app", DisplayDepth: 3}},
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
				if p.DisplayDepth != tt.expected[i].DisplayDepth {
					t.Errorf("project[%d].DisplayDepth = %d, want %d", i, p.DisplayDepth, tt.expected[i].DisplayDepth)
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
projects = [{ path = "~/Dev" }]

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
projects = [{ path = "~/Dev" }]

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
projects = [{ path = "~/Dev" }]
`,
			expectedCmds: 0,
			checkFirstCmd: nil,
		},
		{
			name: "exit defaults to false",
			toml: `
projects = [{ path = "~/Dev" }]

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

func TestProjectEntry(t *testing.T) {
	tests := []struct {
		name          string
		toml          string
		expectedCount int
		checkEntries  func(t *testing.T, entries []ProjectEntry)
	}{
		{
			name:          "object entry with display_depth",
			toml:          `projects = [{ path = "~/Dev/*/*", display_depth = 2 }]`,
			expectedCount: 1,
			checkEntries: func(t *testing.T, entries []ProjectEntry) {
				if entries[0].Path != "~/Dev/*/*" {
					t.Errorf("Path = %q, want %q", entries[0].Path, "~/Dev/*/*")
				}
				if entries[0].GetDisplayDepth() != 2 {
					t.Errorf("GetDisplayDepth() = %d, want 2", entries[0].GetDisplayDepth())
				}
			},
		},
		{
			name:          "object entry without display_depth defaults to 1",
			toml:          `projects = [{ path = "~/Dev/*" }]`,
			expectedCount: 1,
			checkEntries: func(t *testing.T, entries []ProjectEntry) {
				if entries[0].Path != "~/Dev/*" {
					t.Errorf("Path = %q, want %q", entries[0].Path, "~/Dev/*")
				}
				if entries[0].GetDisplayDepth() != 1 {
					t.Errorf("GetDisplayDepth() = %d, want 1", entries[0].GetDisplayDepth())
				}
			},
		},
		{
			name: "multiple entries",
			toml: `projects = [{ path = "~/simple/*" }, { path = "~/deep/*/*", display_depth = 2 }]`,
			expectedCount: 2,
			checkEntries: func(t *testing.T, entries []ProjectEntry) {
				if entries[0].Path != "~/simple/*" {
					t.Errorf("entries[0].Path = %q, want %q", entries[0].Path, "~/simple/*")
				}
				if entries[0].GetDisplayDepth() != 1 {
					t.Errorf("entries[0].GetDisplayDepth() = %d, want 1", entries[0].GetDisplayDepth())
				}
				if entries[1].Path != "~/deep/*/*" {
					t.Errorf("entries[1].Path = %q, want %q", entries[1].Path, "~/deep/*/*")
				}
				if entries[1].GetDisplayDepth() != 2 {
					t.Errorf("entries[1].GetDisplayDepth() = %d, want 2", entries[1].GetDisplayDepth())
				}
			},
		},
		{
			name: "array-of-tables syntax",
			toml: `
[[projects]]
path = "~/Dev/*"
display_depth = 3
`,
			expectedCount: 1,
			checkEntries: func(t *testing.T, entries []ProjectEntry) {
				if entries[0].GetDisplayDepth() != 3 {
					t.Errorf("GetDisplayDepth() = %d, want 3", entries[0].GetDisplayDepth())
				}
			},
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
			if len(cfg.Projects) != tt.expectedCount {
				t.Fatalf("got %d projects, want %d", len(cfg.Projects), tt.expectedCount)
			}
			if tt.checkEntries != nil {
				tt.checkEntries(t, cfg.Projects)
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
			toml:     `projects = [{ path = "~/Dev" }]`,
			expected: "first_unique_segment",
		},
		{
			name:     "explicit first_unique_segment",
			toml:     "projects = [{ path = \"~/Dev\" }]\ndisambiguation_strategy = \"first_unique_segment\"",
			expected: "first_unique_segment",
		},
		{
			name:     "explicit full_path",
			toml:     "projects = [{ path = \"~/Dev\" }]\ndisambiguation_strategy = \"full_path\"",
			expected: "full_path",
		},
		{
			name:     "invalid value defaults to first_unique_segment",
			toml:     "projects = [{ path = \"~/Dev\" }]\ndisambiguation_strategy = \"bogus\"",
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

func TestExpandProjectsRejectsDoubleStarGlob(t *testing.T) {
	tmpDir := t.TempDir()

	// Create nested dirs that ** would match
	os.MkdirAll(filepath.Join(tmpDir, "a", "b", "c"), 0755)

	cfg := &Config{Projects: []ProjectEntry{{Path: filepath.Join(tmpDir, "**")}}}
	result, err := cfg.ExpandProjects()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("got %d projects, want 0 (** patterns should be skipped)", len(result))
	}
}

func TestGetQuickAccessModifier(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected string
	}{
		{"default empty", "", "alt"},
		{"explicit alt", "alt", "alt"},
		{"explicit ctrl", "ctrl", "ctrl"},
		{"explicit disabled", "disabled", "disabled"},
		{"invalid value", "foo", "alt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{QuickAccessModifier: tt.value}
			if got := cfg.GetQuickAccessModifier(); got != tt.expected {
				t.Errorf("GetQuickAccessModifier() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestExpandProjectsDisplayDepth(t *testing.T) {
	// Test that display_depth is propagated through expansion.
	// This test uses the real filesystem with temp directories.
	tmpDir := t.TempDir()

	// Create: tmpDir/work/app, tmpDir/personal/app
	os.MkdirAll(filepath.Join(tmpDir, "work", "app"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "personal", "app"), 0755)

	cfg := &Config{Projects: []ProjectEntry{{Path: filepath.Join(tmpDir, "*", "*"), DisplayDepth: 2}}}
	result, err := cfg.ExpandProjects()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("got %d projects, want 2: %v", len(result), result)
	}

	for _, ep := range result {
		if ep.DisplayDepth != 2 {
			t.Errorf("path %q: DisplayDepth = %d, want 2", ep.Path, ep.DisplayDepth)
		}
	}
}

func TestExpandProjectsSkipsHiddenDirs(t *testing.T) {
	tmpDir := t.TempDir()

	os.MkdirAll(filepath.Join(tmpDir, "visible"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, ".hidden"), 0755)

	cfg := &Config{Projects: []ProjectEntry{{Path: filepath.Join(tmpDir, "*")}}}
	result, err := cfg.ExpandProjects()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("got %d projects, want 1: %v", len(result), result)
	}
	if filepath.Base(result[0].Path) != "visible" {
		t.Errorf("expected 'visible', got %q", filepath.Base(result[0].Path))
	}
}

func TestRemoveSubsumedPaths(t *testing.T) {
	tests := []struct {
		name     string
		input    []ExpandedPath
		expected []ExpandedPath
	}{
		{
			name:     "empty input",
			input:    nil,
			expected: nil,
		},
		{
			name: "no overlap",
			input: []ExpandedPath{
				{Path: "/a", DisplayDepth: 1},
				{Path: "/b", DisplayDepth: 1},
				{Path: "/c", DisplayDepth: 1},
			},
			expected: []ExpandedPath{
				{Path: "/a", DisplayDepth: 1},
				{Path: "/b", DisplayDepth: 1},
				{Path: "/c", DisplayDepth: 1},
			},
		},
		{
			name: "simple parent-child",
			input: []ExpandedPath{
				{Path: "/a", DisplayDepth: 1},
				{Path: "/a/b", DisplayDepth: 2},
			},
			expected: []ExpandedPath{
				{Path: "/a/b", DisplayDepth: 2},
			},
		},
		{
			name: "transitive subsumption",
			input: []ExpandedPath{
				{Path: "/a", DisplayDepth: 1},
				{Path: "/a/b", DisplayDepth: 1},
				{Path: "/a/b/c", DisplayDepth: 3},
			},
			expected: []ExpandedPath{
				{Path: "/a/b/c", DisplayDepth: 3},
			},
		},
		{
			name: "multiple independent subsumptions",
			input: []ExpandedPath{
				{Path: "/a", DisplayDepth: 1},
				{Path: "/a/x", DisplayDepth: 2},
				{Path: "/b", DisplayDepth: 1},
				{Path: "/b/y", DisplayDepth: 2},
			},
			expected: []ExpandedPath{
				{Path: "/a/x", DisplayDepth: 2},
				{Path: "/b/y", DisplayDepth: 2},
			},
		},
		{
			name: "no false positive on common prefix",
			input: []ExpandedPath{
				{Path: "/foo/bar", DisplayDepth: 1},
				{Path: "/foo/barbaz", DisplayDepth: 1},
			},
			expected: []ExpandedPath{
				{Path: "/foo/bar", DisplayDepth: 1},
				{Path: "/foo/barbaz", DisplayDepth: 1},
			},
		},
		{
			name: "order independent — child before parent",
			input: []ExpandedPath{
				{Path: "/a/b", DisplayDepth: 2},
				{Path: "/a", DisplayDepth: 1},
			},
			expected: []ExpandedPath{
				{Path: "/a/b", DisplayDepth: 2},
			},
		},
		{
			name: "parent with multiple children",
			input: []ExpandedPath{
				{Path: "/proj", DisplayDepth: 1},
				{Path: "/proj/v1", DisplayDepth: 2},
				{Path: "/proj/v2", DisplayDepth: 2},
			},
			expected: []ExpandedPath{
				{Path: "/proj/v1", DisplayDepth: 2},
				{Path: "/proj/v2", DisplayDepth: 2},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := removeSubsumedPaths(tt.input)

			if len(result) != len(tt.expected) {
				t.Fatalf("got %d paths, want %d: %v", len(result), len(tt.expected), result)
			}

			for i, p := range result {
				if p.Path != tt.expected[i].Path {
					t.Errorf("result[%d].Path = %q, want %q", i, p.Path, tt.expected[i].Path)
				}
				if p.DisplayDepth != tt.expected[i].DisplayDepth {
					t.Errorf("result[%d].DisplayDepth = %d, want %d", i, p.DisplayDepth, tt.expected[i].DisplayDepth)
				}
			}
		})
	}
}

func TestLoadIncludes(t *testing.T) {
	t.Run("basic include merges projects", func(t *testing.T) {
		tmpDir := t.TempDir()
		writeFile := func(name, content string) string {
			p := filepath.Join(tmpDir, name)
			if err := os.WriteFile(p, []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
			return p
		}

		writeFile("work.toml", `projects = [{ path = "~/Work/*" }]`)
		configPath := writeFile("config.toml", `
includes = ["work.toml"]
projects = [{ path = "~/Personal/*" }]
`)

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		if len(cfg.Projects) != 2 {
			t.Fatalf("got %d projects, want 2", len(cfg.Projects))
		}
		if cfg.Projects[0].Path != "~/Personal/*" {
			t.Errorf("projects[0].Path = %q, want %q", cfg.Projects[0].Path, "~/Personal/*")
		}
		if cfg.Projects[1].Path != "~/Work/*" {
			t.Errorf("projects[1].Path = %q, want %q", cfg.Projects[1].Path, "~/Work/*")
		}
	})

	t.Run("multiple includes in order", func(t *testing.T) {
		tmpDir := t.TempDir()
		writeFile := func(name, content string) string {
			p := filepath.Join(tmpDir, name)
			if err := os.WriteFile(p, []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
			return p
		}

		writeFile("a.toml", `projects = [{ path = "/a" }]`)
		writeFile("b.toml", `projects = [{ path = "/b" }]`)
		configPath := writeFile("config.toml", `
includes = ["a.toml", "b.toml"]
projects = [{ path = "/main" }]
`)

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		if len(cfg.Projects) != 3 {
			t.Fatalf("got %d projects, want 3", len(cfg.Projects))
		}
		expected := []string{"/main", "/a", "/b"}
		for i, want := range expected {
			if cfg.Projects[i].Path != want {
				t.Errorf("projects[%d].Path = %q, want %q", i, cfg.Projects[i].Path, want)
			}
		}
	})

	t.Run("tilde expansion in include path", func(t *testing.T) {
		tmpDir := t.TempDir()
		// Create the include file inside a "home" directory
		homeDir := filepath.Join(tmpDir, "home")
		os.MkdirAll(filepath.Join(homeDir, ".config", "pop"), 0755)

		includePath := filepath.Join(homeDir, ".config", "pop", "extra.toml")
		os.WriteFile(includePath, []byte(`projects = [{ path = "/extra" }]`), 0644)

		configPath := filepath.Join(tmpDir, "config.toml")
		os.WriteFile(configPath, []byte(`
includes = ["~/.config/pop/extra.toml"]
projects = [{ path = "/main" }]
`), 0644)

		d := &Deps{
			FS: &deps.MockFileSystem{
				UserHomeDirFunc: func() (string, error) {
					return homeDir, nil
				},
			},
		}

		cfg, err := LoadWith(d, configPath)
		if err != nil {
			t.Fatalf("LoadWith() error: %v", err)
		}
		if len(cfg.Projects) != 2 {
			t.Fatalf("got %d projects, want 2", len(cfg.Projects))
		}
		if cfg.Projects[1].Path != "/extra" {
			t.Errorf("projects[1].Path = %q, want %q", cfg.Projects[1].Path, "/extra")
		}
	})

	t.Run("relative path resolved against config dir", func(t *testing.T) {
		tmpDir := t.TempDir()
		subDir := filepath.Join(tmpDir, "conf")
		os.MkdirAll(subDir, 0755)

		os.WriteFile(filepath.Join(subDir, "extra.toml"), []byte(`projects = [{ path = "/extra" }]`), 0644)
		configPath := filepath.Join(subDir, "config.toml")
		os.WriteFile(configPath, []byte(`
includes = ["extra.toml"]
projects = [{ path = "/main" }]
`), 0644)

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		if len(cfg.Projects) != 2 {
			t.Fatalf("got %d projects, want 2", len(cfg.Projects))
		}
	})

	t.Run("missing include file prints warning and continues", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.toml")
		os.WriteFile(configPath, []byte(`
includes = ["nonexistent.toml"]
projects = [{ path = "/main" }]
`), 0644)

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("expected no error for missing include, got: %v", err)
		}
		if len(cfg.Projects) != 1 || cfg.Projects[0].Path != "/main" {
			t.Fatalf("expected 1 project from main config, got: %v", cfg.Projects)
		}
		if len(cfg.Warnings) != 1 {
			t.Fatalf("expected 1 warning, got: %v", cfg.Warnings)
		}
	})

	t.Run("included file scalar fields are ignored", func(t *testing.T) {
		tmpDir := t.TempDir()
		writeFile := func(name, content string) string {
			p := filepath.Join(tmpDir, name)
			os.WriteFile(p, []byte(content), 0644)
			return p
		}

		writeFile("extra.toml", `
exclude_current_dir = true
disambiguation_strategy = "full_path"
quick_access_modifier = "ctrl"
projects = [{ path = "/extra" }]

[[worktree.commands]]
key = "ctrl-x"
label = "test"
command = "echo test"
`)
		configPath := writeFile("config.toml", `
includes = ["extra.toml"]
projects = [{ path = "/main" }]
`)

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		// Main config values should be preserved (defaults)
		if cfg.ExcludeCurrentDir {
			t.Error("ExcludeCurrentDir should be false (main config default)")
		}
		if cfg.GetDisambiguationStrategy() != "first_unique_segment" {
			t.Errorf("DisambiguationStrategy = %q, want %q", cfg.GetDisambiguationStrategy(), "first_unique_segment")
		}
		if cfg.GetQuickAccessModifier() != "alt" {
			t.Errorf("QuickAccessModifier = %q, want %q", cfg.GetQuickAccessModifier(), "alt")
		}
		if cfg.Worktree != nil {
			t.Error("Worktree should be nil (from main config)")
		}
		// But projects should be merged
		if len(cfg.Projects) != 2 {
			t.Fatalf("got %d projects, want 2", len(cfg.Projects))
		}
	})

	t.Run("empty includes array works fine", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.toml")
		os.WriteFile(configPath, []byte(`
includes = []
projects = [{ path = "/main" }]
`), 0644)

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		if len(cfg.Projects) != 1 {
			t.Fatalf("got %d projects, want 1", len(cfg.Projects))
		}
	})

	t.Run("no includes field works fine", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.toml")
		os.WriteFile(configPath, []byte(`projects = [{ path = "/main" }]`), 0644)

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		if len(cfg.Projects) != 1 {
			t.Fatalf("got %d projects, want 1", len(cfg.Projects))
		}
	})
}

func TestExpandProjectsSubsumption(t *testing.T) {
	// Integration test: broad glob + specific glob with different display_depth
	tmpDir := t.TempDir()

	// Create: tmpDir/work/proj_a, tmpDir/personal/proj_c,
	//         tmpDir/personal/proj_d/v1, tmpDir/personal/proj_d/v2
	os.MkdirAll(filepath.Join(tmpDir, "work", "proj_a"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "personal", "proj_c"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "personal", "proj_d", "v1"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "personal", "proj_d", "v2"), 0755)

	cfg := &Config{Projects: []ProjectEntry{
		{Path: filepath.Join(tmpDir, "*", "*")},
		{Path: filepath.Join(tmpDir, "personal", "proj_d", "*"), DisplayDepth: 2},
	}}

	result, err := cfg.ExpandProjects()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have: proj_a, proj_c, proj_d/v1, proj_d/v2 (NOT proj_d)
	if len(result) != 4 {
		t.Fatalf("got %d projects, want 4: %v", len(result), result)
	}

	// proj_d should NOT be in the results
	for _, ep := range result {
		if filepath.Base(ep.Path) == "proj_d" {
			t.Errorf("proj_d should be subsumed but is present: %v", ep)
		}
	}

	// Children should have display_depth = 2
	for _, ep := range result {
		dir := filepath.Base(filepath.Dir(ep.Path))
		if dir == "proj_d" {
			if ep.DisplayDepth != 2 {
				t.Errorf("child %q: DisplayDepth = %d, want 2", ep.Path, ep.DisplayDepth)
			}
		}
	}
}
