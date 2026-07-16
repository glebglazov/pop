package config

import (
	"encoding/json"
	"strings"

	"github.com/BurntSushi/toml"
)

// ResolvedTrunk is the current repo's effective Trunk worktree as surfaced by
// pop config show: the checkout that serves as Trunk — a bare repo's
// config-declared trunk = true worktree, or a non-bare repo's git-derived main
// worktree (which no config file names) — together with whether the underlying
// repository is bare. Resolving it needs git, so the caller (cmd) wires pop's
// own trunk resolver; config only renders the answer. A nil *ResolvedTrunk means
// the command ran outside any git repo, so the section is omitted.
type ResolvedTrunk struct {
	// Path is the Trunk worktree checkout; rendered as an absolute realpath.
	// Empty when no trunk is resolvable (e.g. a bare repo with no trunk = true
	// override), in which case only Bare is emitted.
	Path string
	// Bare reports whether the underlying repository is bare.
	Bare bool
}

// CurrentTrunkFunc resolves the current repo's effective Trunk worktree from the
// merged config (which supplies any trunk = true override). It returns a nil
// *ResolvedTrunk when run outside any git repo, so the current-repo section is
// omitted. cmd wires pop's real resolver; config never imports the trunk
// resolver, keeping this package free of git and the task-binding store.
type CurrentTrunkFunc func(cfg *Config) (*ResolvedTrunk, error)

// EffectiveTOML renders pop's effective configuration as TOML: the global
// config with its includes merged in and every [repo."<path>"] key
// canonicalized to an absolute realpath. When trunk is non-nil, the current
// repo's resolved Trunk worktree is appended as a [current_repo] table. It is
// the value counterpart to the keys schema — what is actually in effect, not
// what may be set (ADR-0114).
func EffectiveTOML(path string, trunk CurrentTrunkFunc) (string, error) {
	return EffectiveTOMLWith(defaultDeps, path, trunk)
}

// EffectiveTOMLWith is the injectable variant of EffectiveTOML.
func EffectiveTOMLWith(d *Deps, path string, trunk CurrentTrunkFunc) (string, error) {
	cfg, err := LoadWith(d, path)
	if err != nil {
		return "", err
	}
	var rt *ResolvedTrunk
	if trunk != nil {
		rt, err = trunk(cfg)
		if err != nil {
			return "", err
		}
	}
	return renderEffectiveTOML(d, cfg, rt)
}

// EffectiveJSON renders the same effective-config mirror as EffectiveTOML —
// merged global config, canonicalized [repo."<path>"] keys, and the current
// repo's resolved trunk/bare — but as JSON, for machine consumers. It is built
// by re-decoding the rendered TOML into a generic value and re-encoding that
// as JSON, so the JSON keys and nesting always match the TOML form exactly
// (e.g. current_repo.trunk / current_repo.bare) with no separate struct to
// drift out of sync. The motivating consumer is the to-tasks-here-and-now
// guard, which needs the resolved trunk without shell TOML-parsing.
func EffectiveJSON(path string, trunk CurrentTrunkFunc) (string, error) {
	return EffectiveJSONWith(defaultDeps, path, trunk)
}

// EffectiveJSONWith is the injectable variant of EffectiveJSON.
func EffectiveJSONWith(d *Deps, path string, trunk CurrentTrunkFunc) (string, error) {
	cfg, err := LoadWith(d, path)
	if err != nil {
		return "", err
	}
	var rt *ResolvedTrunk
	if trunk != nil {
		rt, err = trunk(cfg)
		if err != nil {
			return "", err
		}
	}
	tomlOut, err := renderEffectiveTOML(d, cfg, rt)
	if err != nil {
		return "", err
	}

	var generic interface{}
	if _, err := toml.Decode(tomlOut, &generic); err != nil {
		return "", err
	}
	b, err := json.MarshalIndent(generic, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// renderEffectiveTOML serializes an already-loaded config back to TOML as an
// effective mirror. Includes are dropped because the loader has already merged
// them in — re-listing them would invite a redundant second resolution — and
// every repo-scope key is canonicalized (~ expanded, symlinks resolved) so the
// emitted [repo."<path>"] keys are absolute realpaths. When trunk is non-nil the
// current repo's resolved Trunk worktree is appended as a [current_repo] table.
func renderEffectiveTOML(d *Deps, cfg *Config, trunk *ResolvedTrunk) (string, error) {
	out := *cfg
	out.Includes = nil
	out.Repo = canonicalizeRepoKeys(d, cfg.Repo)

	var b strings.Builder
	if err := toml.NewEncoder(&b).Encode(&out); err != nil {
		return "", err
	}
	if trunk != nil {
		section, err := encodeCurrentRepo(d, trunk)
		if err != nil {
			return "", err
		}
		b.WriteString("\n")
		b.WriteString(section)
	}
	return b.String(), nil
}

// currentRepoTOML is the [current_repo] table body: the resolved effective Trunk
// worktree (absolute realpath, omitted when none is resolvable) and whether the
// underlying repository is bare.
type currentRepoTOML struct {
	Trunk string `toml:"trunk,omitempty"`
	Bare  bool   `toml:"bare"`
}

// encodeCurrentRepo renders the resolved trunk as a standalone [current_repo]
// TOML table. The trunk path is canonicalized the same way repo keys are (~
// expanded, symlinks resolved) so it is emitted as an absolute realpath.
func encodeCurrentRepo(d *Deps, trunk *ResolvedTrunk) (string, error) {
	section := struct {
		CurrentRepo currentRepoTOML `toml:"current_repo"`
	}{
		CurrentRepo: currentRepoTOML{Bare: trunk.Bare},
	}
	if p := strings.TrimSpace(trunk.Path); p != "" {
		section.CurrentRepo.Trunk = canonicalPath(d, p)
	}

	var b strings.Builder
	if err := toml.NewEncoder(&b).Encode(&section); err != nil {
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
