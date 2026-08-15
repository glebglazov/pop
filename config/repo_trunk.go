package config

import "sort"

// This file is how a trunk is named and read back outside the resolution ladder:
// `--trunk <path>` states one, and the trunk resolver asks what was stated.
//
// Naming a trunk is a human stating intent, so it lands in the override config
// layer rather than in a gap-filler pop wrote for itself (ADR-0212 decision 5),
// filed under the Repository identity of the checkout named — the repository has
// one fork base, and every worktree of it must read the same one. Neither entry
// point reads a committed .pop/config.toml: the in-tree anchors are found through
// the trunk, so a trunk resolved through them could never resolve at all
// (ADR-0150's self-reference guard).

// PersistRepoTrunk states checkoutPath as the Trunk worktree of its repository.
// The user's config.toml is never written.
func PersistRepoTrunk(checkoutPath string) error {
	return PersistRepoTrunkWith(DefaultDeps(), checkoutPath)
}

// PersistRepoTrunkWith is the injectable variant. The path is canonicalized
// before it is stored, so what a reader compares against a checkout is the same
// shape whatever the operator typed.
func PersistRepoTrunkWith(d *Deps, checkoutPath string) error {
	if d == nil {
		d = defaultDeps
	}
	trunk := canonicalPath(d, checkoutPath)
	_, err := SetRepoOverrideValueWith(d, trunk, "trunk", trunk)
	return err
}

// OverrideTrunkPaths returns every checkout the override layer states as a Trunk
// worktree, over all its per-repository blocks. It hands back paths rather than
// an answer for one repository because the caller that needs it — the trunk
// resolver — holds git and can say which of them is a checkout of the repository
// in hand, which is a sharper question than matching the block's key.
func OverrideTrunkPaths() ([]string, error) {
	return OverrideTrunkPathsWith(defaultDeps)
}

// OverrideTrunkPathsWith is the injectable variant. Paths come back canonicalized
// and sorted, so a map's iteration order never reaches the caller.
func OverrideTrunkPathsWith(d *Deps) ([]string, error) {
	if d == nil {
		d = defaultDeps
	}
	layer, err := loadOverrideLayer(d)
	if err != nil {
		return nil, err
	}
	var paths []string
	for key, block := range layer.scoped.Repo {
		// A block is filed under a repository identity, so that key is the only
		// thing a folded legacy boolean in this file could name.
		if path, ok := block.Trunk.Resolve(key); ok {
			paths = append(paths, canonicalPath(d, path))
		}
	}
	sort.Strings(paths)
	return paths, nil
}
