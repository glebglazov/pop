package config

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"

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

// applyConfigLayerMerge resolves effective config as embedded defaults, then
// config.runtime.toml (when present), then the user file — later layers override
// earlier ones field-by-field (ADR 0065).
func applyConfigLayerMerge(d *Deps, userCfg *Config, userPath string, userMD toml.MetaData) error {
	defaults, err := loadEmbeddedDefaults()
	if err != nil {
		return err
	}

	layers := []configLayer{{path: "<embedded defaults>", cfg: defaults, md: toml.MetaData{}}}

	runtimePath := DefaultRuntimeConfigPathWith(d)
	if layer, err := decodeConfigLayer(d, runtimePath); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("loading runtime config %q: %w", runtimePath, err)
		}
	} else {
		layers = append(layers, *layer)
	}

	layers = append(layers, configLayer{path: userPath, cfg: userCfg, md: userMD})

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
	return nil
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

