package config

import (
	"os"
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
		expected []string
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
			expected: []string{"/home/user/projects/myapp"},
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
			expected: []string{"/projects/dir"},
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
			expected: []string{"/projects/app"},
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
				if p != tt.expected[i] {
					t.Errorf("project[%d] = %q, want %q", i, p, tt.expected[i])
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
