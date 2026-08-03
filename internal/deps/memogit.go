package deps

import (
	"path/filepath"
	"strings"
	"sync"
)

// MemoGit wraps a Git and memoizes idempotent, read-only queries for the
// lifetime of the wrapper. It exists because the derivations a read pays for —
// a repository's identity, its common dir, its HEAD — are each asked once per
// caller that needs them and there are many callers per load: a Work load
// resolves one checkout's `--git-common-dir` well over a hundred times, and at
// tens of milliseconds a fork that is the whole of the load's wall clock.
//
// The memo answers a question about a checkout, not about a moment: it is
// correct only while nothing it caches can change, so its lifetime must be one
// load. Whoever constructs it owns that scope — see (*drain.Deps).WorkKinds,
// which builds one per snapshot so the next poll re-reads a moved HEAD.
//
// It is safe for concurrent use: a load fans out across goroutines. The inner
// git call runs without the lock held so cache hits never serialize behind a
// live subprocess; two concurrent misses on the same key simply both run once.
type MemoGit struct {
	inner Git
	mu    sync.Mutex
	cache map[string]gitResult
}

type gitResult struct {
	out string
	err error
}

func NewMemoGit(inner Git) *MemoGit {
	return &MemoGit{inner: inner, cache: map[string]gitResult{}}
}

// Inner is the git seam the memo forwards to. Callers that derive one memo per
// seam use it to recognise two seams that would fork the same subprocess.
func (c *MemoGit) Inner() Git { return c.inner }

// MemoizableGitArgs reports whether a git invocation is a read-only query whose
// result is stable for the duration of one load. Anything not listed passes
// straight through uncached, so mutating or volatile commands are never served
// from the cache.
func MemoizableGitArgs(args []string) bool {
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

// Command passes through: a memo keys on the directory a question was asked
// about, and a cwd-relative invocation names none.
func (c *MemoGit) Command(args ...string) (string, error) {
	return c.inner.Command(args...)
}

func (c *MemoGit) CommandInDir(dir string, args ...string) (string, error) {
	if !MemoizableGitArgs(args) {
		return c.inner.CommandInDir(dir, args...)
	}
	// Cleaning the directory is what makes two callers that spell the same
	// checkout differently share one fork; it costs no syscall, so a path that
	// only a stat could equate simply misses.
	key := filepath.Clean(dir) + "\x00" + strings.Join(args, "\x00")

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
