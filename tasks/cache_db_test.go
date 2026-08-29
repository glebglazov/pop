package tasks

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

// depsWithCacheHome builds a Deps whose filesystem seam answers XDG_CACHE_HOME
// with cacheHome (empty to exercise the ~/.cache fallback) and reports home as
// the user's home directory.
func depsWithCacheHome(t *testing.T, cacheHome, home string) *Deps {
	t.Helper()
	real := deps.NewRealFileSystem()
	d := &Deps{FS: &deps.MockFileSystem{
		GetenvFunc: func(key string) string {
			if key == "XDG_CACHE_HOME" {
				return cacheHome
			}
			return ""
		},
		UserHomeDirFunc: func() (string, error) { return home, nil },
		StatFunc:        real.Stat,
	}}
	t.Cleanup(func() { _ = d.CloseCacheDB() })
	return d
}

func TestCacheDBPathFollowsXDGCacheHomeAndItsFallback(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	explicit := depsWithCacheHome(t, filepath.Join(root, "xdg"), filepath.Join(root, "home"))
	if got, want := CacheDBPathWith(explicit), filepath.Join(root, "xdg", "pop", "cache.db"); got != want {
		t.Fatalf("cache path = %q, want %q", got, want)
	}

	fallback := depsWithCacheHome(t, "", filepath.Join(root, "home"))
	if got, want := CacheDBPathWith(fallback), filepath.Join(root, "home", ".cache", "pop", "cache.db"); got != want {
		t.Fatalf("fallback cache path = %q, want %q", got, want)
	}
}

// The cache database is created on demand, directory and all, and it lives
// outside the execution state store's data directory so read-path writes never
// contend with authoritative ones.
func TestCacheDBCreatesTheDatabaseOnDemandSeparateFromTheStore(t *testing.T) {
	t.Parallel()
	d := newTestDeps(t)

	if d.CacheDB() == nil {
		t.Fatal("CacheDB returned no handle on a writable cache directory")
	}
	path := CacheDBPathWith(d)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("cache database not created at %s: %v", path, err)
	}
	if filepath.Dir(path) == popDataDirWith(d) {
		t.Fatal("the cache database landed in the execution state store's data directory")
	}
	if got := DrainStorePathWith(d); got == path {
		t.Fatalf("cache database shares the store's path %q", got)
	}
	// The store handle is the cache's opposite number, and the two must not be
	// the same connection: opening both leaves two live files.
	if _, err := openDrainStore(d); err != nil {
		t.Fatalf("open execution-state store: %v", err)
	}
	if _, err := os.Stat(DrainStorePathWith(d)); err != nil {
		t.Fatalf("execution-state store not created: %v", err)
	}
}

// `rm cache.db` is a supported repair on a running pop: the handle it was using
// is dropped and the file comes back on the next access.
func TestCacheDBRecreatesADeletedDatabase(t *testing.T) {
	t.Parallel()
	d := newTestDeps(t)

	first := d.CacheDB()
	if first == nil {
		t.Fatal("CacheDB returned no handle")
	}
	path := CacheDBPathWith(d)
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			t.Fatalf("remove %s: %v", p, err)
		}
	}

	second := d.CacheDB()
	if second == nil {
		t.Fatal("CacheDB gave up after the database was deleted")
	}
	if second == first {
		t.Fatal("CacheDB kept the handle pointing at the deleted file")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("cache database not recreated: %v", err)
	}
}

// Every unusable cache is a miss, not an error: the caller is handed nil and
// carries on as if there were no cache at all.
func TestCacheDBDegradesToNoCacheWhenUnusable(t *testing.T) {
	t.Parallel()

	t.Run("uncreatable directory", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		// A regular file where the cache directory must go: MkdirAll cannot win.
		if err := os.WriteFile(filepath.Join(root, "pop"), []byte("not a directory"), 0o644); err != nil {
			t.Fatal(err)
		}
		d := depsWithCacheHome(t, root, root)
		if got := d.CacheDB(); got != nil {
			t.Fatal("CacheDB returned a handle with an uncreatable cache directory")
		}
	})

	t.Run("corrupt file, repaired by deleting it", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		d := depsWithCacheHome(t, root, root)
		path := CacheDBPathWith(d)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("this is not a sqlite database"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := d.CacheDB(); got != nil {
			t.Fatal("CacheDB returned a handle for a corrupt database")
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if got := d.CacheDB(); got == nil {
			t.Fatal("CacheDB stayed dead after the corrupt file was deleted")
		}
	})

	t.Run("unwritable directory", func(t *testing.T) {
		t.Parallel()
		if os.Geteuid() == 0 {
			t.Skip("root writes through mode bits")
		}
		root := t.TempDir()
		d := depsWithCacheHome(t, root, root)
		if err := os.MkdirAll(filepath.Dir(CacheDBPathWith(d)), 0o500); err != nil {
			t.Fatal(err)
		}
		if got := d.CacheDB(); got != nil {
			t.Fatal("CacheDB returned a handle inside an unwritable directory")
		}
	})
}

// A test that never isolated its cache directory must not write the developer's
// real cache.db; it runs without a cache instead of panicking, because a cache
// is optional by construction.
func TestCacheDBRefusesTheRealCacheDirUnderTest(t *testing.T) {
	t.Parallel()
	if prodCacheDirAtStartup == "" {
		t.Skip("no real cache directory to guard")
	}
	if cacheDBAllowed(filepath.Join(prodCacheDirAtStartup, cacheDBFile)) {
		t.Fatal("the real cache database was allowed under go test")
	}
}
