package config

import (
	"path/filepath"
	"sort"
)

// DeclaredTrunkPaths returns every checkout path declared the Trunk worktree of
// its repository, home-expanded and cleaned. See DeclaredTrunkPathsWith.
func (c *Config) DeclaredTrunkPaths() []string {
	return c.DeclaredTrunkPathsWith(defaultDeps)
}

// DeclaredTrunkPathsWith reads every source that can state a trunk without the
// trunk being known first — a [repo."<path>"] block of the global config.toml and
// the override layer's per-repository entries — and hands back the checkouts they
// name.
//
// This is the fork-free half of binding.ResolveTrunkPathWith. That resolver
// answers "which checkout is *this* repository's trunk", which needs a repo key
// per candidate and so a git fork per candidate. A caller that already holds the
// checkout and only wants to know whether it was *declared* the trunk needs
// neither: every declaration names a path, so the answer is a set membership
// test. That is what keeps the project picker's zero-git-call invariant
// (ADR-0110) intact while it honours the declaration.
//
// Paths are returned as written after home expansion, with no symlink
// resolution: resolving costs an lstat per path component, and the caller that
// needs it can resolve both sides itself once a plain comparison has missed.
// The order is sorted so a map's iteration order never reaches the caller.
func (c *Config) DeclaredTrunkPathsWith(d *Deps) []string {
	if d == nil {
		d = defaultDeps
	}
	seen := make(map[string]bool)
	var out []string
	add := func(raw string) {
		if raw == "" {
			return
		}
		path := filepath.Clean(expandHomeWith(d, raw))
		if seen[path] {
			return
		}
		seen[path] = true
		out = append(out, path)
	}
	// A block states a path, and a retired boolean states the block's own key
	// (ADR-0212 decision 3), so both spellings arrive here as the checkout meant.
	// A trunk pop recorded for itself is no longer a source of its own: it folded
	// into the override layer's block for its repository (decision 5), which is
	// read below.
	if c != nil {
		for rawKey, block := range c.Repo {
			if path, ok := block.Trunk.Resolve(rawKey); ok {
				add(path)
			}
		}
	}
	// A missing or unreadable override layer is the common case and reads as no
	// declarations: a trunk that cannot be read is a trunk that is not declared,
	// and every caller degrades the same way it does for a repo with no
	// declaration at all. That is why the error is dropped.
	if layer, err := loadOverrideLayer(d); err == nil {
		for key, block := range layer.scoped.Repo {
			if path, ok := block.Trunk.Resolve(key); ok {
				add(path)
			}
		}
	}
	sort.Strings(out)
	return out
}
