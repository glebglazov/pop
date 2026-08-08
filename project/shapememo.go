package project

import "sync"

// ShapeMemo memoizes a configured path's repository shape — whether it is a bare
// repo with worktrees, and the worktrees it holds — for the lifetime of the
// wrapper. It exists because every Work read surface expands the same configured
// project paths more than once per load: repo-group resolution, binding
// resolution and the Routine scan each ask, and each ask stats the path and
// reads and parses the whole of the repository's git config (ADR-0189's
// 2026-08-08 amendment).
//
// The memo answers a question about a moment, not about content: which projects
// exist and which repositories are bare can both change under it, so its
// lifetime must be one load and an entry that outlived the load could name a
// project that has since been deleted. Whoever constructs it owns that scope —
// see (*drain.Deps).WithGitMemo, which builds a fresh one per load alongside the
// Git fact memo.
//
// It is safe for concurrent use: path expansion fans a goroutine per configured
// path. The probe runs without the lock held so hits never serialize behind a
// filesystem read; two concurrent misses on the same path simply both run once.
type ShapeMemo struct {
	mu        sync.Mutex
	bare      map[string]bool
	worktrees map[string]worktreeResult
}

type worktreeResult struct {
	list []Worktree
	err  error
}

// NewShapeMemo returns an empty memo scoped to the caller's load.
func NewShapeMemo() *ShapeMemo {
	return &ShapeMemo{bare: map[string]bool{}, worktrees: map[string]worktreeResult{}}
}

// hasWorktrees serves the bare-with-worktrees verdict for path, probing through
// compute on a miss. A nil memo means no load has claimed a scope, so it always
// probes.
func (m *ShapeMemo) hasWorktrees(path string, compute func() bool) bool {
	if m == nil {
		return compute()
	}
	m.mu.Lock()
	v, ok := m.bare[path]
	m.mu.Unlock()
	if ok {
		return v
	}
	v = compute()
	m.mu.Lock()
	m.bare[path] = v
	m.mu.Unlock()
	return v
}

// listWorktrees serves path's worktree list, probing through compute on a miss.
func (m *ShapeMemo) listWorktrees(path string, compute func() ([]Worktree, error)) ([]Worktree, error) {
	if m == nil {
		return compute()
	}
	m.mu.Lock()
	r, ok := m.worktrees[path]
	m.mu.Unlock()
	if ok {
		return r.list, r.err
	}
	list, err := compute()
	m.mu.Lock()
	m.worktrees[path] = worktreeResult{list: list, err: err}
	m.mu.Unlock()
	return list, err
}
