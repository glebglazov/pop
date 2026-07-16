package config

import (
	"strings"

	"github.com/BurntSushi/toml"
)

// EffectiveTOML renders pop's effective configuration as TOML: the global
// config with its includes merged in and every [repo."<path>"] key
// canonicalized to an absolute realpath. It is the value counterpart to the
// keys schema — what is actually in effect, not what may be set (ADR-0114).
func EffectiveTOML(path string) (string, error) {
	return EffectiveTOMLWith(defaultDeps, path)
}

// EffectiveTOMLWith is the injectable variant of EffectiveTOML.
func EffectiveTOMLWith(d *Deps, path string) (string, error) {
	cfg, err := LoadWith(d, path)
	if err != nil {
		return "", err
	}
	return renderEffectiveTOML(d, cfg)
}

// renderEffectiveTOML serializes an already-loaded config back to TOML as an
// effective mirror. Includes are dropped because the loader has already merged
// them in — re-listing them would invite a redundant second resolution — and
// every repo-scope key is canonicalized (~ expanded, symlinks resolved) so the
// emitted [repo."<path>"] keys are absolute realpaths.
func renderEffectiveTOML(d *Deps, cfg *Config) (string, error) {
	out := *cfg
	out.Includes = nil
	out.Repo = canonicalizeRepoKeys(d, cfg.Repo)

	var b strings.Builder
	if err := toml.NewEncoder(&b).Encode(&out); err != nil {
		return "", err
	}
	return b.String(), nil
}

// canonicalizeRepoKeys rebuilds a [repo] block map with every key resolved to
// its absolute realpath. Keys that collapse to the same path after
// canonicalization coalesce (last wins), matching how resolution treats them
// as one repository identity.
func canonicalizeRepoKeys(d *Deps, repo map[string]RepoOverrideConfig) map[string]RepoOverrideConfig {
	if repo == nil {
		return nil
	}
	out := make(map[string]RepoOverrideConfig, len(repo))
	for key, block := range repo {
		out[canonicalPath(d, key)] = block
	}
	return out
}
