package config

// PersistRepoTrunk records checkoutPath as the Trunk worktree for its repository
// in config.runtime.toml (ADR-0150). The user's config.toml is never written.
func PersistRepoTrunk(checkoutPath string) error {
	return PersistRepoTrunkWith(DefaultDeps(), "", checkoutPath)
}

// PersistRepoTrunkWith is the injectable variant.
func PersistRepoTrunkWith(d *Deps, _configPath, checkoutPath string) error {
	if d == nil {
		d = defaultDeps
	}
	return SetRuntimeRepoTrunkWith(d, canonicalPath(d, checkoutPath))
}
