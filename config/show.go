package config

import (
	"encoding/json"
	"strings"

	"github.com/BurntSushi/toml"
)

// ResolvedTrunk is the current repo's effective Trunk worktree as surfaced by
// pop config show: the checkout that serves as Trunk — a bare repo's
// config-declared trunk = "<path>" worktree (ADR-0212 decision 3), or a
// non-bare repo's git-derived main worktree (which no config file names) —
// together with whether the underlying repository is bare. Resolving it needs
// git, so the caller (cmd) wires pop's own trunk resolver; config only renders
// the answer. A nil *ResolvedTrunk means the command ran outside any git repo,
// so the section is omitted.
type ResolvedTrunk struct {
	// Path is the Trunk worktree checkout; rendered as an absolute realpath.
	// Empty when no trunk is resolvable (e.g. a bare repo with no trunk
	// override), in which case only Bare is emitted.
	Path string
	// Bare reports whether the underlying repository is bare.
	Bare bool
	// Checkout is the checkout the command ran in — the anchor the repo-scope
	// keys of the current-repo section resolve from. It is the caller's own
	// working directory, not the Trunk: scope-first resolution answers per
	// checkout, and a worktree may declare something its Trunk does not. Empty
	// leaves the repo-scope keys out of the section.
	Checkout string
}

// CurrentTrunkFunc resolves the current repo's effective Trunk worktree from the
// merged config (which supplies any trunk = "<path>" override). It returns a nil
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
// emitted [repo."<path>"] keys are absolute realpaths, with the override layer's
// per-repository entries laid over them. When trunk is non-nil the current
// repo's answers are appended as a [current_repo] table.
func renderEffectiveTOML(d *Deps, cfg *Config, trunk *ResolvedTrunk) (string, error) {
	out := *cfg
	out.Includes = nil
	repo, err := effectiveRepoBlocks(d, cfg)
	if err != nil {
		return "", err
	}
	out.Repo = repo

	var b strings.Builder
	if err := toml.NewEncoder(&b).Encode(&out); err != nil {
		return "", err
	}
	if trunk != nil {
		section, err := encodeCurrentRepo(d, cfg, trunk)
		if err != nil {
			return "", err
		}
		b.WriteString("\n")
		b.WriteString(section)
	}
	return b.String(), nil
}

// currentRepoTOML is the [current_repo] table body: the resolved effective Trunk
// worktree (absolute realpath, omitted when none is resolvable), whether the
// underlying repository is bare, and the repo-scope keys as they resolve at the
// checkout the command ran in.
type currentRepoTOML struct {
	Trunk              string `toml:"trunk,omitempty"`
	Bare               bool   `toml:"bare"`
	PreferredWorkbench string `toml:"preferred_workbench,omitempty"`
	// TurnCap is a pointer so "this repository declares no bound" is an absent
	// key rather than a printed zero, which would read as a cap of no turns.
	TurnCap *int `toml:"turn_cap,omitempty"`
}

// encodeCurrentRepo renders what pop resolves for the current repository as a
// standalone [current_repo] TOML table. The trunk path is canonicalized the same
// way repo keys are (~ expanded, symlinks resolved) so it is emitted as an
// absolute realpath.
//
// The repo-scope keys are resolved from the checkout rather than read off the
// global config, which is what makes the mirror report the scope-first answer
// (ADR-0212 decision 1): a value a team committed to .pop/config.toml lives in no
// file this command prints above, yet it is what is in force here — so a reader
// standing in that repository sees the committed value, not the global one it now
// outranks. Resolution warnings are dropped: the mirror states effective values
// and never provenance, and a stale name is already surfaced as a load finding.
func encodeCurrentRepo(d *Deps, cfg *Config, trunk *ResolvedTrunk) (string, error) {
	section := struct {
		CurrentRepo currentRepoTOML `toml:"current_repo"`
	}{
		CurrentRepo: currentRepoTOML{Bare: trunk.Bare},
	}
	if p := strings.TrimSpace(trunk.Path); p != "" {
		section.CurrentRepo.Trunk = canonicalPath(d, p)
	}
	if checkout := strings.TrimSpace(trunk.Checkout); checkout != "" {
		section.CurrentRepo.PreferredWorkbench, _ = cfg.ResolvePreferredWorkbench(d, checkout)
		if repoCfg, err := cfg.ResolveRepoConfig(d, checkout); err == nil && repoCfg.TurnCap > 0 {
			bound := repoCfg.TurnCap
			section.CurrentRepo.TurnCap = &bound
		}
	}

	var b strings.Builder
	if err := toml.NewEncoder(&b).Encode(&section); err != nil {
		return "", err
	}
	return b.String(), nil
}

// effectiveRepoBlocks renders the [repo."<path>"] blocks as they are actually in
// force: the user's own blocks, canonicalized, with the override layer's
// per-repository entries laid over the block of the same repository (ADR-0212
// decision 2). Without the overlay the mirror would print a value an override
// has already replaced — and "what is actually in effect" is the whole reason
// this command exists. An override for a repository the user declares no block
// for is emitted under the Repository identity it is filed against, which is the
// only path the layer knows for it.
func effectiveRepoBlocks(d *Deps, cfg *Config) (map[string]RepoOverrideConfig, error) {
	blocks := canonicalizeRepoKeys(d, cfg.Repo)
	// A trunk is printed as the path it names, whichever spelling stated it: a
	// block still holding the retired boolean is folded to the checkout it marked
	// (ADR-0212 decision 3), so the mirror reads back in the one live form.
	for key, block := range blocks {
		if path, ok := block.Trunk.Resolve(key); ok {
			folded := TrunkPath(canonicalPath(d, path))
			block.Trunk = &folded
			blocks[key] = block
		}
	}
	layer, err := loadOverrideLayer(d)
	if err != nil {
		return nil, err
	}
	for key, override := range layer.scoped.Repo {
		identity := repoIdentity(d, key)
		target := identity
		for declared := range blocks {
			if repoIdentity(d, declared) == identity {
				target = declared
				break
			}
		}
		merged := blocks[target]
		overlayRepoBlock(&merged, override)
		if blocks == nil {
			blocks = map[string]RepoOverrideConfig{}
		}
		blocks[target] = merged
	}
	return blocks, nil
}

// overlayRepoBlock lays one [repo] block over another: the shared repo-scope
// keys through the same walker that resolves them, and the two [repo]-only keys
// by presence, a nil pointer meaning the block states nothing about that key.
func overlayRepoBlock(base *RepoOverrideConfig, over RepoOverrideConfig) {
	scope := over.RepoScopeConfig
	mergeWalk(&base.RepoScopeConfig, &scope, repoScopeMetadata(scope), repoScopePolicy())
	if over.Trunk != nil {
		base.Trunk = over.Trunk
	}
	if over.TurnCap != nil {
		base.TurnCap = over.TurnCap
	}
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
