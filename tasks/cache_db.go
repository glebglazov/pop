package tasks

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/glebglazov/pop/store"
)

// cacheDBFile is the machine-local Cache database's name. It is `cache.db`, not
// a name for whichever derived answer lands in it first: this is the home for
// every answer pop may recompute rather than lose (ADR-0243 decision 1).
const cacheDBFile = "cache.db"

// popCacheDirWith returns pop's base cache directory, respecting XDG_CACHE_HOME
// with the ~/.cache/pop fallback — the cache-side twin of popDataDirWith. It is
// deliberately outside the data dir: what lives under it is disposable, and
// deleting it is a valid repair.
func popCacheDirWith(d *Deps) string {
	if xdgCache := d.FS.Getenv("XDG_CACHE_HOME"); xdgCache != "" {
		return filepath.Join(xdgCache, "pop")
	}
	home, err := d.FS.UserHomeDir()
	if err != nil {
		return filepath.Join("/tmp", "pop-cache")
	}
	return filepath.Join(home, ".cache", "pop")
}

// CacheDBPathWith returns the path to the machine-local Cache database.
func CacheDBPathWith(d *Deps) string {
	return filepath.Join(popCacheDirWith(d), cacheDBFile)
}

// cacheDBHolder is the process-cached Cache handle. It sits behind a pointer on
// Deps for the same reasons storeCache does: a shallow copy of Deps shares the
// one handle, and the mutex never rides a value copy.
//
// failed records that the open at path did not work, so a run with an
// unwritable cache directory or a corrupt file pays the failure once instead of
// on every read. It is cleared when the file at path disappears, which is how a
// human repairs a corrupt cache: delete it, and the next access builds a fresh
// one.
type cacheDBHolder struct {
	mu     sync.Mutex
	path   string
	handle *store.Cache
	failed bool
}

// depsCacheDBInitMu guards the lazy allocation of the holder for a Deps built
// from a bare literal (tests) that never went through DefaultDeps.
var depsCacheDBInitMu sync.Mutex

func (d *Deps) cacheDBHolder() *cacheDBHolder {
	depsCacheDBInitMu.Lock()
	defer depsCacheDBInitMu.Unlock()
	if d.cacheDB == nil {
		d.cacheDB = &cacheDBHolder{}
	}
	return d.cacheDB
}

// CacheDB returns the process-cached Cache database handle, creating the cache
// directory and the file on first use. It returns nil — never an error — when
// the cache is unusable for any reason at all: an uncreatable directory, a
// database that will not open, a corrupt file, a schema from a newer pop. Every
// tenant's read then misses and every write drops, which is the designed
// behaviour of having no cache (ADR-0243 decision 4). No caller is ever made to
// handle a cache problem.
//
// The handle is dropped and reopened when the file underneath it has gone: `rm
// cache.db` is a supported repair on a running pop, and the next access
// recreates it.
//
// SQLite cannot ride the filesystem seam, so this uses os directly; the path is
// still derived through the seam-aware popCacheDirWith.
func (d *Deps) CacheDB() *store.Cache {
	c := d.cacheDBHolder()
	c.mu.Lock()
	defer c.mu.Unlock()

	path := CacheDBPathWith(d)
	if c.path != path {
		// The derived path changed (a test redirected its cache dir): drop
		// whatever the old one left behind.
		_ = c.handle.Close()
		c.handle, c.failed, c.path = nil, false, path
	}
	_, statErr := os.Stat(path)
	if statErr != nil {
		// The file is gone: a cached handle now writes to a deleted inode, and a
		// remembered failure describes a file that no longer exists.
		if c.handle != nil {
			_ = c.handle.Close()
			c.handle = nil
		}
		c.failed = false
	}
	if c.handle != nil {
		return c.handle
	}
	if c.failed || !cacheDBAllowed(path) {
		return nil
	}
	c.failed = true // cleared on success below; a failure below needs no branch
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil
	}
	h, err := store.OpenCache(path)
	if err != nil {
		return nil
	}
	c.handle, c.failed = h, false
	return h
}

// CloseCacheDB closes the process-cached Cache handle and drops it, so the next
// CacheDB call reopens. The queue daemon loop and test cleanup call it; one-shot
// CLI runs rely on process exit, which is WAL-safe.
func (d *Deps) CloseCacheDB() error {
	c := d.cacheDBHolder()
	c.mu.Lock()
	defer c.mu.Unlock()
	err := c.handle.Close()
	c.handle, c.failed, c.path = nil, false, ""
	return err
}

// prodCacheDirAtStartup is the developer's real cache directory, resolved once
// at package load — before any test calls t.Setenv — mirroring how
// prodDataDirAtStartup is snapshotted for the store guard.
var prodCacheDirAtStartup = realProductionCacheDir()

// cacheDBAllowed is the test-isolation backstop. Under `go test`, a test that
// never redirected XDG_CACHE_HOME would otherwise write the developer's real
// cache database. Unlike guardTestStorePath this does not panic: the cache is
// optional by construction, so the honest response to "this test is not
// isolated" is the same as to every other unusable cache — run without one. A
// panic would instead fail hundreds of tests that legitimately never think
// about the cache.
func cacheDBAllowed(path string) bool {
	if !testing.Testing() || prodCacheDirAtStartup == "" {
		return true
	}
	return filepath.Dir(path) != prodCacheDirAtStartup
}

// realProductionCacheDir resolves pop's cache directory from the *real* process
// environment (not the filesystem seam), mirroring popCacheDirWith.
func realProductionCacheDir() string {
	if xdgCache := os.Getenv("XDG_CACHE_HOME"); xdgCache != "" {
		return filepath.Join(xdgCache, "pop")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".cache", "pop")
}
