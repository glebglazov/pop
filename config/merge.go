package config

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/glebglazov/pop/debug"
)

//go:embed defaults.toml
var embeddedDefaultsTOML string

var validIntegrationSkillAliases = map[string]bool{
	IntegrationSkillPane:  true,
	IntegrationSkillTasks: true,
}

// DefaultRuntimeConfigPath returns the integration runtime config path.
func DefaultRuntimeConfigPath() string {
	return DefaultRuntimeConfigPathWith(defaultDeps)
}

// DefaultRuntimeConfigPathWith returns config.runtime.toml under the pop data dir.
func DefaultRuntimeConfigPathWith(d *Deps) string {
	return filepath.Join(dataDirWith(d), "config.runtime.toml")
}

// DefaultOverrideConfigPath returns the override config path.
func DefaultOverrideConfigPath() string {
	return DefaultOverrideConfigPathWith(defaultDeps)
}

// DefaultOverrideConfigPathWith returns config.override.toml under the pop data dir.
func DefaultOverrideConfigPathWith(d *Deps) string {
	return filepath.Join(dataDirWith(d), "config.override.toml")
}

func dataDirWith(d *Deps) string {
	if xdgData := d.FS.Getenv("XDG_DATA_HOME"); xdgData != "" {
		return filepath.Join(xdgData, "pop")
	}
	home, err := d.FS.UserHomeDir()
	if err != nil {
		debug.Error("dataDirWith: UserHomeDir: %v", err)
	}
	return filepath.Join(home, ".local", "share", "pop")
}

func loadEmbeddedDefaults() (*Config, error) {
	var cfg Config
	if _, err := toml.Decode(embeddedDefaultsTOML, &cfg); err != nil {
		return nil, fmt.Errorf("embedded defaults: %w", err)
	}
	return &cfg, nil
}

type configLayer struct {
	path string
	cfg  *Config
	md   toml.MetaData
}

func decodeConfigLayer(d *Deps, path string) (*configLayer, error) {
	data, err := d.FS.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	md, err := toml.Decode(string(data), &cfg)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &configLayer{path: path, cfg: &cfg, md: md}, nil
}

// applyConfigLayerMerge resolves effective config from four layers, lowest rank
// first: embedded defaults, config.runtime.toml, the user's hand-authored
// config.toml, then config.override.toml. A later layer overrides an earlier one
// field-by-field (ADR-0065, ADR-0202).
//
// Two of those files are written by pop itself, and they sit on opposite sides of
// the hand-authored file on purpose. What separates them is not that pop wrote
// both but what each one says (ADR-0212 decision 5): one records what happened,
// the other states what a human wants.
//
//	config.runtime.toml   BELOW config.toml — the gap-filler. It records what
//	                      pop's own surfaces happened to pick (a Preferred
//	                      workbench, a Trunk checkout, an integrate skills list),
//	                      so a declaration at the same scope must beat it. That low
//	                      rank is load-bearing, not incidental: preferred_workbench's
//	                      three-valued explicit-none logic exists precisely
//	                      because a runtime entry cannot outrank a declaration
//	                      above it (see ResolvePreferredWorkbench).
//	config.override.toml  ABOVE config.toml — the override layer. It records whole
//	                      keys a human deliberately overrode through pop's own
//	                      editor, so it has to beat the very file being
//	                      overridden; a layer that lost to config.toml would be
//	                      inert for everyone who has configured anything
//	                      (ADR-0202 decision 1).
//
// Only the override file's global half takes part in this merge. Its
// [repo."<identity>"] blocks are the repository-scoped half of the same layer,
// laid over what the repo-scope ladder resolves for a checkout in hand
// (override_layer.go, reposcope.go) — which this merge, holding no checkout,
// could not do.
//
// It returns the override layer's MetaData, which the caller needs to keep
// hand-authored include files from winning over an override.
func applyConfigLayerMerge(d *Deps, userCfg *Config, userPath string, userMD toml.MetaData) (toml.MetaData, error) {
	var overrideMD toml.MetaData

	defaults, err := loadEmbeddedDefaults()
	if err != nil {
		return overrideMD, err
	}

	layers := []configLayer{{path: "<embedded defaults>", cfg: defaults, md: toml.MetaData{}}}

	runtimePath := DefaultRuntimeConfigPathWith(d)
	if layer, err := decodeConfigLayer(d, runtimePath); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return overrideMD, fmt.Errorf("loading runtime config %q: %w", runtimePath, err)
		}
	} else {
		layers = append(layers, *layer)
	}

	layers = append(layers, configLayer{path: userPath, cfg: userCfg, md: userMD})

	overridePath := DefaultOverrideConfigPathWith(d)
	if layer, err := decodeConfigLayer(d, overridePath); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return overrideMD, fmt.Errorf("loading override config %q: %w", overridePath, err)
		}
	} else {
		// The override file's [repo."<identity>"] blocks are the layer's
		// repository-scoped half and are applied over the resolved value by the
		// repo-scope resolvers (ADR-0212 decision 2). Merging them here would file
		// them in the same map as the user's own [repo."<path>"] declarations,
		// where they would read as one of them — a rung of the very ladder they are
		// there to outrank.
		layer.cfg.Repo = nil
		layers = append(layers, *layer)
		overrideMD = layer.md
	}

	merged := *defaults
	policy := overlayPolicy()
	for i := 1; i < len(layers); i++ {
		layer := layers[i]
		for _, f := range integrationsSkillsFindings(layer.path, layer.cfg.Integrations, layer.md) {
			merged.recordFinding(f)
		}
		mergeWalk(&merged, layer.cfg, layer.md, policy)
	}

	*userCfg = merged
	return overrideMD, nil
}

// claimOverrideKeys marks every key config.override.toml defines as already
// claimed for the first-wins include walk, so a hand-authored include file can
// never win over the layer that outranks it. Ancestors are claimed too because
// the include walk tests a whole-value field at the field's own path: an include
// that redefines all of [work.verify] would otherwise wipe an agent list the
// override set inside it.
func claimOverrideKeys(policy *mergePolicy, overrideMD toml.MetaData) {
	for _, key := range overrideMD.Keys() {
		// The per-repository blocks take no part in the global merge, so an
		// include's own [repo."<path>"] declarations are none of their business.
		if len(key) > 0 && key[0] == overrideRepoSection {
			continue
		}
		for i := 1; i <= len(key); i++ {
			policy.claim(strings.Join([]string(key[:i]), "."))
		}
	}
}

func integrationsSkillsFindings(path string, integrations *IntegrationsConfig, md toml.MetaData) []Finding {
	if integrations == nil || !md.IsDefined("integrations", "skills") {
		return nil
	}
	var findings []Finding
	for i, alias := range integrations.Skills {
		if validIntegrationSkillAliases[alias] {
			continue
		}
		findings = append(findings, Finding{
			Path: fmt.Sprintf("integrations.skills[%d]", i),
			Message: fmt.Sprintf(
				"%s: [integrations] skills[%d]: unknown integration skill alias %q; valid aliases: pane, tasks",
				path, i, alias,
			),
		})
	}
	return findings
}

