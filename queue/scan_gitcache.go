package queue

import (
	"strings"
	"sync"

	"github.com/glebglazov/pop/internal/deps"
)

// scanGitCache wraps a deps.Git and memoizes idempotent, read-only git queries
// for the lifetime of a single Scan. A scan resolves the same repository
// coordinates many times — once per project to derive paths, then again per repo
// group to decide dispatches — issuing identical `rev-parse` and `worktree list`
// subprocesses against the same directories. A scan is a point-in-time snapshot,
// so caching these reads within it is safe and removes the redundant forks that
// dominate wall-clock on large project lists.
//
// It is safe for concurrent use: Scan fans resolution out across goroutines. The
// inner git call runs without the lock held so cache hits never serialize behind
// a live subprocess; two concurrent misses on the same key simply both run once.
type scanGitCache struct {
	inner deps.Git
	mu    sync.Mutex
	cache map[string]gitResult
}

type gitResult struct {
	out string
	err error
}

func newScanGitCache(inner deps.Git) *scanGitCache {
	return &scanGitCache{inner: inner, cache: map[string]gitResult{}}
}

// cacheableArgs reports whether a git invocation is a read-only query whose
// result is stable for the duration of a scan. Anything not listed passes
// straight through uncached, so mutating or volatile commands are never served
// from the cache.
func cacheableArgs(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "rev-parse":
		return true
	case "worktree":
		return len(args) >= 2 && args[1] == "list"
	}
	return false
}

func (c *scanGitCache) Command(args ...string) (string, error) {
	return c.inner.Command(args...)
}

func (c *scanGitCache) CommandInDir(dir string, args ...string) (string, error) {
	if !cacheableArgs(args) {
		return c.inner.CommandInDir(dir, args...)
	}
	key := dir + "\x00" + strings.Join(args, "\x00")

	c.mu.Lock()
	if r, ok := c.cache[key]; ok {
		c.mu.Unlock()
		return r.out, r.err
	}
	c.mu.Unlock()

	out, err := c.inner.CommandInDir(dir, args...)

	c.mu.Lock()
	c.cache[key] = gitResult{out: out, err: err}
	c.mu.Unlock()
	return out, err
}
