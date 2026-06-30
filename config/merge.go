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
	for i := 1; i < len(layers); i++ {
		layer := layers[i]
		for _, f := range integrationsSkillsFindings(layer.path, layer.cfg.Integrations, layer.md) {
			merged.recordFinding(f)
		}
		mergeConfigOverlay(&merged, layer.cfg, layer.md)
	}

	*userCfg = merged
	return nil
}

func mergeConfigOverlay(dst, src *Config, md toml.MetaData) {
	if src == nil {
		return
	}
	if md.IsDefined("includes") {
		dst.Includes = append([]string(nil), src.Includes...)
	}
	if md.IsDefined("projects") {
		dst.Projects = append([]ProjectEntry(nil), src.Projects...)
	}
	if md.IsDefined("commands") {
		dst.Commands = append([]UserDefinedCommand(nil), src.Commands...)
	}
	if md.IsDefined("exclude_current_session") {
		dst.ExcludeCurrentSession = src.ExcludeCurrentSession
	}
	if md.IsDefined("exclude_current_dir") {
		dst.ExcludeCurrentDir = src.ExcludeCurrentDir
	}
	if md.IsDefined("disambiguation_strategy") {
		dst.DisambiguationStrategy = src.DisambiguationStrategy
	}
	if md.IsDefined("quick_access_modifier") {
		dst.QuickAccessModifier = src.QuickAccessModifier
	}
	if md.IsDefined("worktree") {
		dst.Worktree = cloneWorktreeConfig(src.Worktree)
	}
	if md.IsDefined("project") {
		dst.Project = cloneProjectConfig(src.Project)
	}
	if md.IsDefined("select") {
		dst.Select = cloneProjectConfig(src.Select)
	}
	if md.IsDefined("pane_monitoring") {
		dst.PaneMonitoring = clonePaneMonitoringConfig(src.PaneMonitoring)
	}
	if md.IsDefined("dashboard") {
		dst.Dashboard = cloneDashboardConfig(src.Dashboard)
	}
	if md.IsDefined("workload") {
		dst.Task = cloneTaskConfig(src.Task)
	}
	if md.IsDefined("effort") {
		dst.Effort = cloneEffortMap(src.Effort)
	}
	if md.IsDefined("workbenches") {
		dst.Workbenches = cloneSessionTemplates(src.Workbenches)
	}
	if md.IsDefined("session_templates") {
		dst.SessionTemplates = cloneSessionTemplates(src.SessionTemplates)
	}
	if md.IsDefined("queue") {
		dst.Queue = cloneQueueConfig(src.Queue)
	}
	if md.IsDefined("updates") {
		dst.Updates = cloneUpdatesConfig(src.Updates)
	}
	if md.IsDefined("integrations") {
		dst.Integrations = mergeIntegrationsConfig(dst.Integrations, src.Integrations, md)
	}
	if md.IsDefined("repo") {
		if src.Repo != nil {
			if dst.Repo == nil {
				dst.Repo = make(map[string]RepoOverrideConfig, len(src.Repo))
			}
			for key, block := range src.Repo {
				dst.Repo[key] = block
			}
		}
	}
}

func mergeIntegrationsConfig(base, overlay *IntegrationsConfig, md toml.MetaData) *IntegrationsConfig {
	if overlay == nil {
		return base
	}
	result := cloneIntegrationsConfig(base)
	if result == nil {
		result = &IntegrationsConfig{}
	}
	if md.IsDefined("integrations", "skills") {
		result.Skills = append([]string(nil), overlay.Skills...)
	}
	if md.IsDefined("integrations", "skills_prefix") {
		if overlay.SkillsPrefix == nil {
			result.SkillsPrefix = nil
		} else {
			v := *overlay.SkillsPrefix
			result.SkillsPrefix = &v
		}
	}
	return result
}

func cloneIntegrationsConfig(src *IntegrationsConfig) *IntegrationsConfig {
	if src == nil {
		return nil
	}
	var prefix *string
	if src.SkillsPrefix != nil {
		v := *src.SkillsPrefix
		prefix = &v
	}
	return &IntegrationsConfig{
		Skills:       append([]string(nil), src.Skills...),
		SkillsPrefix: prefix,
	}
}

func cloneSessionTemplates(src []SessionTemplate) []SessionTemplate {
	if src == nil {
		return nil
	}
	out := make([]SessionTemplate, len(src))
	for i, tmpl := range src {
		out[i] = SessionTemplate{
			Name:    tmpl.Name,
			Windows: make([]SessionTemplateWindow, len(tmpl.Windows)),
		}
		for j, window := range tmpl.Windows {
			out[i].Windows[j].Name = window.Name
			if window.Layout != nil {
				layout := *window.Layout
				out[i].Windows[j].Layout = &layout
			}
		}
	}
	return out
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

func cloneWorktreeConfig(src *WorktreeConfig) *WorktreeConfig {
	if src == nil {
		return nil
	}
	return &WorktreeConfig{
		Commands:                      append([]UserDefinedCommand(nil), src.Commands...),
		UnreadNotificationsEnabled:    src.UnreadNotificationsEnabled,
		AttentionNotificationsEnabled: src.AttentionNotificationsEnabled,
	}
}

func cloneProjectConfig(src *ProjectConfig) *ProjectConfig {
	if src == nil {
		return nil
	}
	return &ProjectConfig{
		Commands:                      append([]UserDefinedCommand(nil), src.Commands...),
		UnreadNotificationsEnabled:    src.UnreadNotificationsEnabled,
		AttentionNotificationsEnabled: src.AttentionNotificationsEnabled,
	}
}

func clonePaneMonitoringConfig(src *PaneMonitoringConfig) *PaneMonitoringConfig {
	if src == nil {
		return nil
	}
	dst := *src
	dst.IgnoreStatusFrom = append([]string(nil), src.IgnoreStatusFrom...)
	dst.TopicAgents = append(TopicSteps(nil), src.TopicAgents...)
	return &dst
}

func cloneDashboardConfig(src *DashboardConfig) *DashboardConfig {
	if src == nil {
		return nil
	}
	dst := *src
	dst.SortCriteria = append([]string(nil), src.SortCriteria...)
	return &dst
}

func cloneQueueConfig(src *QueueConfig) *QueueConfig {
	if src == nil {
		return nil
	}
	return &QueueConfig{
		PollInterval:         src.PollInterval,
		AgentQuotaRetryAfter: src.AgentQuotaRetryAfter,
		CrashRetryDelays:     append([]string(nil), src.CrashRetryDelays...),
	}
}

func cloneUpdatesConfig(src *UpdatesConfig) *UpdatesConfig {
	if src == nil {
		return nil
	}
	dst := *src
	return &dst
}

func cloneEffortMap(src map[string]EffortConfig) map[string]EffortConfig {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]EffortConfig, len(src))
	for agent, ladder := range src {
		dst[agent] = cloneEffortConfig(ladder)
	}
	return dst
}
