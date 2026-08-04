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

// DeclaredTrunkPathsWith reads the two path-keyed trunk declarations — a
// hand-authored [repo."<path>"] trunk = true block and runtime layer 5
// (config.runtime.toml) — and hands back the paths they name.
//
// This is the fork-free half of binding.ResolveTrunkPathWith. That resolver
// answers "which checkout is *this* repository's trunk", which needs a repo key
// per candidate and so a git fork per candidate. A caller that already holds the
// checkout and only wants to know whether it was *declared* the trunk needs
// neither: a declaration is keyed by the path itself, so the answer is a set
// membership test. That is what keeps the project picker's zero-git-call
// invariant (ADR-0110) intact while it honours the declaration.
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
	if c != nil {
		for rawKey, block := range c.Repo {
			if block.Trunk != nil && *block.Trunk {
				add(rawKey)
			}
		}
	}
	// A missing runtime file is the common case and reads as no declarations,
	// which is why the error is dropped rather than surfaced: a trunk that cannot
	// be read is a trunk that is not declared, and every caller degrades the same
	// way it does for a repo with no declaration at all.
	if paths, err := RuntimeRepoTrunkPathsWith(d); err == nil {
		for _, rawKey := range paths {
			add(rawKey)
		}
	}
	sort.Strings(out)
	return out
}
