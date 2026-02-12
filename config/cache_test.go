package config

import (
	"encoding/json"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/deps"
)

func TestDefaultCachePathWith(t *testing.T) {
	tests := []struct {
		name     string
		xdgCache string
		userHome string
		expected string
	}{
		{
			name:     "uses XDG_CACHE_HOME when set",
			xdgCache: "/custom/cache",
			expected: "/custom/cache/pop/glob_cache.json",
		},
		{
			name:     "falls back to ~/.cache when XDG not set",
			xdgCache: "",
			userHome: "/home/user",
			expected: "/home/user/.cache/pop/glob_cache.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Deps{
				FS: &deps.MockFileSystem{
					GetenvFunc: func(key string) string {
						if key == "XDG_CACHE_HOME" {
							return tt.xdgCache
						}
						return ""
					},
					UserHomeDirFunc: func() (string, error) {
						return tt.userHome, nil
					},
				},
			}

			result := DefaultCachePathWith(d)

			if result != tt.expected {
				t.Errorf("DefaultCachePathWith() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestLoadGlobCache(t *testing.T) {
	tests := []struct {
		name        string
		fileContent string
		fileErr     error
		wantEntries int
	}{
		{
			name:        "missing file returns empty cache",
			fileErr:     os.ErrNotExist,
			wantEntries: 0,
		},
		{
			name:        "invalid JSON returns empty cache",
			fileContent: "not json",
			wantEntries: 0,
		},
		{
			name:        "wrong version returns empty cache",
			fileContent: `{"version": 99, "entries": {}}`,
			wantEntries: 0,
		},
		{
			name:        "valid cache file",
			fileContent: `{"version": 1, "entries": {"/path/*": {"base_path": "/path", "matches": ["/path/a"], "dir_mtimes": {}}}}`,
			wantEntries: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Deps{
				FS: &deps.MockFileSystem{
					ReadFileFunc: func(path string) ([]byte, error) {
						if tt.fileErr != nil {
							return nil, tt.fileErr
						}
						return []byte(tt.fileContent), nil
					},
				},
			}

			cache := loadGlobCache(d, "/test/cache.json")

			if cache == nil {
				t.Fatal("loadGlobCache returned nil")
			}
			if cache.Version != 1 {
				t.Errorf("Version = %d, want 1", cache.Version)
			}
			if len(cache.Entries) != tt.wantEntries {
				t.Errorf("got %d entries, want %d", len(cache.Entries), tt.wantEntries)
			}
		})
	}
}

func TestSaveGlobCache(t *testing.T) {
	var savedPath string
	var savedData []byte
	var mkdirPath string

	d := &Deps{
		FS: &deps.MockFileSystem{
			MkdirAllFunc: func(path string, perm os.FileMode) error {
				mkdirPath = path
				return nil
			},
			WriteFileFunc: func(path string, data []byte, perm os.FileMode) error {
				savedPath = path
				savedData = data
				return nil
			},
		},
	}

	cache := &GlobCache{
		Version: 1,
		Entries: map[string]GlobCacheEntry{
			"/test/*": {
				BasePath: "/test",
				Matches:  []string{"/test/a"},
				DirMtimes: map[string]time.Time{
					"/test": time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
				},
			},
		},
	}

	saveGlobCache(d, "/cache/dir/glob_cache.json", cache)

	if mkdirPath != "/cache/dir" {
		t.Errorf("MkdirAll path = %q, want %q", mkdirPath, "/cache/dir")
	}
	if savedPath != "/cache/dir/glob_cache.json" {
		t.Errorf("WriteFile path = %q, want %q", savedPath, "/cache/dir/glob_cache.json")
	}

	// Verify round-trip
	var loaded GlobCache
	if err := json.Unmarshal(savedData, &loaded); err != nil {
		t.Fatalf("failed to unmarshal saved data: %v", err)
	}
	if len(loaded.Entries) != 1 {
		t.Errorf("loaded %d entries, want 1", len(loaded.Entries))
	}
}

func TestIsCacheEntryValid(t *testing.T) {
	now := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	later := now.Add(time.Hour)

	tests := []struct {
		name     string
		entry    GlobCacheEntry
		statFunc func(path string) (os.FileInfo, error)
		want     bool
	}{
		{
			name: "all mtimes match",
			entry: GlobCacheEntry{
				DirMtimes: map[string]time.Time{
					"/dir1": now,
					"/dir2": now,
				},
			},
			statFunc: func(path string) (os.FileInfo, error) {
				return deps.MockFileInfo{IsDirVal: true, ModTimeVal: now}, nil
			},
			want: true,
		},
		{
			name: "mtime changed",
			entry: GlobCacheEntry{
				DirMtimes: map[string]time.Time{
					"/dir1": now,
				},
			},
			statFunc: func(path string) (os.FileInfo, error) {
				return deps.MockFileInfo{IsDirVal: true, ModTimeVal: later}, nil
			},
			want: false,
		},
		{
			name: "directory removed",
			entry: GlobCacheEntry{
				DirMtimes: map[string]time.Time{
					"/dir1": now,
				},
			},
			statFunc: func(path string) (os.FileInfo, error) {
				return nil, os.ErrNotExist
			},
			want: false,
		},
		{
			name: "empty dir_mtimes is valid",
			entry: GlobCacheEntry{
				DirMtimes: map[string]time.Time{},
			},
			statFunc: func(path string) (os.FileInfo, error) {
				return nil, os.ErrNotExist
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Deps{
				FS: &deps.MockFileSystem{
					StatFunc: tt.statFunc,
				},
			}

			got := isCacheEntryValid(d, tt.entry)

			if got != tt.want {
				t.Errorf("isCacheEntryValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCountWildcardDepth(t *testing.T) {
	tests := []struct {
		pattern string
		want    int
	}{
		{"*", 1},
		{"*/*", 2},
		{"*/foo", 1},
		{"*/*/*", 3},
		{"foo/bar", 0},
		{"*/*/baz", 2},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			got := countWildcardDepth(tt.pattern)

			if got != tt.want {
				t.Errorf("countWildcardDepth(%q) = %d, want %d", tt.pattern, got, tt.want)
			}
		})
	}
}

func TestCollectDirMtimes(t *testing.T) {
	baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	childTime := time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)

	t.Run("single level pattern tracks only base", func(t *testing.T) {
		d := &Deps{
			FS: &deps.MockFileSystem{
				StatFunc: func(path string) (os.FileInfo, error) {
					if path == "/base" {
						return deps.MockFileInfo{IsDirVal: true, ModTimeVal: baseTime}, nil
					}
					return nil, os.ErrNotExist
				},
			},
		}

		mtimes := collectDirMtimes(d, "/base", "*")

		if len(mtimes) != 1 {
			t.Fatalf("got %d entries, want 1", len(mtimes))
		}
		if !mtimes["/base"].Equal(baseTime) {
			t.Errorf("base mtime = %v, want %v", mtimes["/base"], baseTime)
		}
	})

	t.Run("two level pattern tracks base and children", func(t *testing.T) {
		d := &Deps{
			FS: &deps.MockFileSystem{
				StatFunc: func(path string) (os.FileInfo, error) {
					switch path {
					case "/base":
						return deps.MockFileInfo{IsDirVal: true, ModTimeVal: baseTime}, nil
					case "/base/child1", "/base/child2":
						return deps.MockFileInfo{IsDirVal: true, ModTimeVal: childTime}, nil
					default:
						return nil, os.ErrNotExist
					}
				},
				ReadDirFunc: func(path string) ([]os.DirEntry, error) {
					if path == "/base" {
						return []os.DirEntry{
							deps.MockDirEntry{NameVal: "child1", IsDirVal: true},
							deps.MockDirEntry{NameVal: "child2", IsDirVal: true},
						}, nil
					}
					return nil, nil
				},
			},
		}

		mtimes := collectDirMtimes(d, "/base", "*/*")

		if len(mtimes) != 3 {
			t.Fatalf("got %d entries, want 3: %v", len(mtimes), mtimes)
		}
		if !mtimes["/base"].Equal(baseTime) {
			t.Errorf("base mtime wrong")
		}
		if !mtimes["/base/child1"].Equal(childTime) {
			t.Errorf("child1 mtime wrong")
		}
		if !mtimes["/base/child2"].Equal(childTime) {
			t.Errorf("child2 mtime wrong")
		}
	})

	t.Run("skips non-directory entries", func(t *testing.T) {
		d := &Deps{
			FS: &deps.MockFileSystem{
				StatFunc: func(path string) (os.FileInfo, error) {
					switch path {
					case "/base":
						return deps.MockFileInfo{IsDirVal: true, ModTimeVal: baseTime}, nil
					case "/base/dir":
						return deps.MockFileInfo{IsDirVal: true, ModTimeVal: childTime}, nil
					case "/base/file":
						return deps.MockFileInfo{IsDirVal: false, ModTimeVal: childTime}, nil
					default:
						return nil, os.ErrNotExist
					}
				},
				ReadDirFunc: func(path string) ([]os.DirEntry, error) {
					if path == "/base" {
						return []os.DirEntry{
							deps.MockDirEntry{NameVal: "dir", IsDirVal: true},
							deps.MockDirEntry{NameVal: "file", IsDirVal: false},
						}, nil
					}
					return nil, nil
				},
			},
		}

		mtimes := collectDirMtimes(d, "/base", "*/*")

		if len(mtimes) != 2 {
			t.Fatalf("got %d entries, want 2 (base + dir): %v", len(mtimes), mtimes)
		}
		if _, ok := mtimes["/base/file"]; ok {
			t.Error("file should not be tracked")
		}
	})
}

func TestExpandProjectsWith_CacheHit(t *testing.T) {
	now := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)

	cacheData, _ := json.Marshal(GlobCache{
		Version: 1,
		Entries: map[string]GlobCacheEntry{
			"/home/user/Dev/*": {
				BasePath: "/home/user/Dev",
				Matches:  []string{"/home/user/Dev/project1", "/home/user/Dev/project2"},
				DirMtimes: map[string]time.Time{
					"/home/user/Dev": now,
				},
			},
		},
	})

	dirFSCalled := false
	writeFileCalled := false

	d := &Deps{
		FS: &deps.MockFileSystem{
			UserHomeDirFunc: func() (string, error) { return "/home/user", nil },
			ReadFileFunc: func(path string) ([]byte, error) {
				if strings.Contains(path, "glob_cache.json") {
					return cacheData, nil
				}
				return nil, os.ErrNotExist
			},
			StatFunc: func(path string) (os.FileInfo, error) {
				if path == "/home/user/Dev" {
					return deps.MockFileInfo{IsDirVal: true, ModTimeVal: now}, nil
				}
				if path == "/home/user/Dev/project1" || path == "/home/user/Dev/project2" {
					return deps.MockFileInfo{IsDirVal: true}, nil
				}
				return nil, os.ErrNotExist
			},
			DirFSFunc: func(dir string) fs.FS {
				dirFSCalled = true
				return nil
			},
			WriteFileFunc: func(path string, data []byte, perm os.FileMode) error {
				writeFileCalled = true
				return nil
			},
		},
	}

	cfg := &Config{Projects: []ProjectEntry{{Path: "~/Dev/*"}}}
	result, err := cfg.ExpandProjectsWith(d)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("got %d projects, want 2", len(result))
	}
	if dirFSCalled {
		t.Error("DirFS should not be called on cache hit")
	}
	if writeFileCalled {
		t.Error("WriteFile should not be called on cache hit (no cache update)")
	}
}

func TestExpandProjectsWith_CacheStillValidatesIsDir(t *testing.T) {
	now := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)

	cacheData, _ := json.Marshal(GlobCache{
		Version: 1,
		Entries: map[string]GlobCacheEntry{
			"/home/user/Dev/*": {
				BasePath: "/home/user/Dev",
				Matches:  []string{"/home/user/Dev/valid", "/home/user/Dev/deleted"},
				DirMtimes: map[string]time.Time{
					"/home/user/Dev": now,
				},
			},
		},
	})

	d := &Deps{
		FS: &deps.MockFileSystem{
			UserHomeDirFunc: func() (string, error) { return "/home/user", nil },
			ReadFileFunc: func(path string) ([]byte, error) {
				if strings.Contains(path, "glob_cache.json") {
					return cacheData, nil
				}
				return nil, os.ErrNotExist
			},
			StatFunc: func(path string) (os.FileInfo, error) {
				if path == "/home/user/Dev" {
					return deps.MockFileInfo{IsDirVal: true, ModTimeVal: now}, nil
				}
				if path == "/home/user/Dev/valid" {
					return deps.MockFileInfo{IsDirVal: true}, nil
				}
				// /home/user/Dev/deleted no longer exists
				return nil, os.ErrNotExist
			},
		},
	}

	cfg := &Config{Projects: []ProjectEntry{{Path: "~/Dev/*"}}}
	result, err := cfg.ExpandProjectsWith(d)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("got %d projects, want 1 (deleted should be filtered)", len(result))
	}
	if len(result) > 0 && result[0].Path != "/home/user/Dev/valid" {
		t.Errorf("expected /home/user/Dev/valid, got %s", result[0].Path)
	}
}

func TestExpandProjectsWith_CacheMiss_MtimeChanged(t *testing.T) {
	cachedTime := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	currentTime := time.Date(2025, 6, 2, 0, 0, 0, 0, time.UTC)

	cacheData, _ := json.Marshal(GlobCache{
		Version: 1,
		Entries: map[string]GlobCacheEntry{
			"/home/user/Dev/*": {
				BasePath: "/home/user/Dev",
				Matches:  []string{"/home/user/Dev/old_project"},
				DirMtimes: map[string]time.Time{
					"/home/user/Dev": cachedTime,
				},
			},
		},
	})

	var savedData []byte
	d := &Deps{
		FS: &deps.MockFileSystem{
			UserHomeDirFunc: func() (string, error) { return "/home/user", nil },
			ReadFileFunc: func(path string) ([]byte, error) {
				if strings.Contains(path, "glob_cache.json") {
					return cacheData, nil
				}
				return nil, os.ErrNotExist
			},
			// Stat returns a DIFFERENT mtime than cached, triggering invalidation
			StatFunc: func(path string) (os.FileInfo, error) {
				if path == "/home/user/Dev" {
					return deps.MockFileInfo{IsDirVal: true, ModTimeVal: currentTime}, nil
				}
				if path == "/home/user/Dev/new_project" {
					return deps.MockFileInfo{IsDirVal: true}, nil
				}
				return nil, os.ErrNotExist
			},
			// DirFS + Glob will be called since cache is invalid
			DirFSFunc: func(dir string) fs.FS {
				return &deps.MockFS{
					Dirs: map[string][]string{
						".": {"new_project"},
					},
				}
			},
			MkdirAllFunc: func(path string, perm os.FileMode) error { return nil },
			WriteFileFunc: func(path string, data []byte, perm os.FileMode) error {
				savedData = data
				return nil
			},
		},
	}

	cfg := &Config{Projects: []ProjectEntry{{Path: "~/Dev/*"}}}
	result, err := cfg.ExpandProjectsWith(d)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("got %d projects, want 1", len(result))
	}
	if len(result) > 0 && result[0].Path != "/home/user/Dev/new_project" {
		t.Errorf("expected /home/user/Dev/new_project, got %s", result[0].Path)
	}
	// Cache should have been saved
	if savedData == nil {
		t.Error("cache was not saved after miss")
	}
}

func TestExpandProjectsWith_ExactPathsSkipCache(t *testing.T) {
	d := &Deps{
		FS: &deps.MockFileSystem{
			UserHomeDirFunc: func() (string, error) { return "/home/user", nil },
			StatFunc: func(path string) (os.FileInfo, error) {
				if path == "/home/user/exact/project" {
					return deps.MockFileInfo{IsDirVal: true}, nil
				}
				return nil, os.ErrNotExist
			},
		},
	}

	cfg := &Config{Projects: []ProjectEntry{{Path: "~/exact/project"}}}
	result, err := cfg.ExpandProjectsWith(d)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("got %d projects, want 1", len(result))
	}
	if len(result) > 0 && result[0].Path != "/home/user/exact/project" {
		t.Errorf("expected /home/user/exact/project, got %s", result[0].Path)
	}
}
