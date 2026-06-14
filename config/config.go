package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/bmatcuk/doublestar/v4"
	"github.com/glebglazov/pop/debug"
	"github.com/glebglazov/pop/internal/deps"
)

// Deps holds external dependencies for the config package
type Deps struct {
	FS deps.FileSystem
}

// DefaultDeps returns dependencies using real implementations
func DefaultDeps() *Deps {
	return &Deps{
		FS: deps.NewRealFileSystem(),
	}
}

var defaultDeps = DefaultDeps()

// UserDefinedCommand defines a custom keybinding for a picker
type UserDefinedCommand struct {
	Key     string `toml:"key"`     // Key binding (e.g., "ctrl-l")
	Label   string `toml:"label"`   // Display label for hints
	Command string `toml:"command"` // Shell command to execute
	Exit    bool   `toml:"exit"`    // Whether to exit picker after execution
}

// PaneMonitoringConfig holds pane monitoring configuration
type PaneMonitoringConfig struct {
	DismissUnreadInActivePane bool `toml:"dismiss_unread_in_active_pane"`
	// Deprecated: use DismissUnreadInActivePane. The old key is read for
	// backwards compat; a warning is emitted when it is present.
	DismissAttentionInActivePane bool     `toml:"dismiss_attention_in_active_pane"`
	IgnoreStatusFrom             []string `toml:"ignore_status_from"`
	TCPServer                    bool     `toml:"tcp_server"`
	// Addr pins the monitor daemon's TCP address (host:port). Empty means the
	// address is derived from the data dir (ADR 0021). A pinned addr is shared
	// across any data dirs using this config, so only pin it for single-instance
	// setups (e.g. exposing a fixed port to containers).
	Addr string `toml:"addr"`
	// TopicCommand, when set, is the shell command `set-topic --derive` pipes a
	// normalized JSON payload to on stdin; its stdout becomes the pane's Topic.
	// Empty means topics fall back to built-in prompt truncation (ADR 0024).
	TopicCommand string `toml:"topic_command"`
}

// DashboardConfig holds dashboard-specific configuration
type DashboardConfig struct {
	CurrentPaneAlwaysUnderCursor bool     `toml:"current_pane_always_under_cursor"`
	CursorPosition               string   `toml:"cursor_position"`
	SortCriteria                 []string `toml:"sort_criteria"`
}

// Valid dashboard cursor position strategies.
const (
	DashboardCursorCurrentRegistered = "current_registered"
	DashboardCursorCurrentAny        = "current_any"
	DashboardCursorFirstActive       = "first_active"
)

// Valid sort criteria for the dashboard.
const (
	SortByStatus             = "status"
	SortByPaneLastActiveAt   = "pane_last_active_at"
	SortBySessionLastVisitAt = "session_last_visit_at"
	SortByAlphabetical       = "alphabetical"

	// Deprecated: use SortByPaneLastActiveAt. Kept for backward compat with
	// existing config files that reference "pane_last_visit_at".
	SortByPaneLastVisitAt = "pane_last_visit_at"
)

// DefaultSortCriteria is the default sort order for the dashboard
var DefaultSortCriteria = []string{SortByStatus, SortByPaneLastActiveAt, SortByAlphabetical}

// WorktreeConfig holds worktree-specific configuration
type WorktreeConfig struct {
	Commands                   []UserDefinedCommand `toml:"commands"`
	UnreadNotificationsEnabled bool                 `toml:"unread_notifications_enabled"`
	// Deprecated: use UnreadNotificationsEnabled. The old key is read for
	// backwards compat; a warning is emitted when it is present.
	AttentionNotificationsEnabled bool `toml:"attention_notifications_enabled"`
}

// ProjectConfig holds project-picker-specific configuration
type ProjectConfig struct {
	Commands                   []UserDefinedCommand `toml:"commands"`
	UnreadNotificationsEnabled bool                 `toml:"unread_notifications_enabled"`
	// Deprecated: use UnreadNotificationsEnabled. The old key is read for
	// backwards compat; a warning is emitted when it is present.
	AttentionNotificationsEnabled bool `toml:"attention_notifications_enabled"`
}

// UpdatesConfig holds update-check / Update-notice configuration.
type UpdatesConfig struct {
	// NoticeEnabled gates both the picker Update notice and the daily
	// background Update check. A nil pointer (absent section or key) defaults
	// to enabled; an explicit false disables both so pop makes zero automatic
	// network calls (CONTEXT.md "Update check", "Update notice").
	NoticeEnabled *bool `toml:"notice_enabled"`
}

// TaskConfig holds task-execution configuration.
type TaskConfig struct {
	Agents map[string]TaskAgentConfig `toml:"agents"`
	// Git holds commit-time git overrides for Pop's own commits. The TOML
	// sub-table is `[workload.git]` because the parent key stays "workload"
	// for backward compatibility (see Config.Task). A nil pointer means the
	// section is absent ⇒ no overrides; Pop's commits behave exactly as today.
	Git *TaskGitConfig `toml:"git"`
}

// TaskAgentConfig holds configuration for one task agent preset.
type TaskAgentConfig struct {
	Output string `toml:"output"`
}

// TaskGitConfig holds commit-time git configuration applied to Pop's own
// commits during a task drain (e.g. disabling GPG signing so an unattended
// queue drain never hangs on a 1Password presence prompt).
type TaskGitConfig struct {
	// CommitConfigOverrides is a list of git `-c`-style `key=value` strings
	// (e.g. "commit.gpgsign=false") prepended as `-c key=value` pairs to Pop's
	// commit invocations. Absent/empty ⇒ no overrides. Validation is lazy: see
	// Config.ResolveCommitConfigOverrides.
	CommitConfigOverrides []string `toml:"commit_config_overrides"`
}

// ResolveCommitConfigOverrides validates the [workload.git]
// commit_config_overrides entries and returns them as `key=value` strings ready
// to be prepended as `-c key=value` pairs to Pop's commit invocations. Each
// entry must split into a non-empty key on the first `=` (an empty value is
// legal git, e.g. "user.signingkey=").
//
// Validation is deliberately lazy — this is called only from the task drain
// path, never at global config load — so a typo never breaks the picker or
// dashboard. A malformed entry is a hard error: callers must fail the drain
// rather than silently proceed (proceeding could re-trigger the very signing
// hang this feature exists to prevent). The receiver may be nil (no [workload]
// or [workload.git] section), in which case no overrides apply.
func (c *Config) ResolveCommitConfigOverrides() ([]string, error) {
	if c == nil || c.Task == nil || c.Task.Git == nil {
		return nil, nil
	}
	raw := c.Task.Git.CommitConfigOverrides
	if len(raw) == 0 {
		return nil, nil
	}
	overrides := make([]string, 0, len(raw))
	for i, entry := range raw {
		key, _, found := strings.Cut(entry, "=")
		if !found || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("[tasks.git] commit_config_overrides[%d]: %q must be in key=value form with a non-empty key", i, entry)
		}
		overrides = append(overrides, entry)
	}
	return overrides, nil
}

// QueueConfig holds `pop queue` supervisor configuration. Durations are stored
// as standard duration strings (e.g. "60s", "1h") and parsed by ResolveQueue.
type QueueConfig struct {
	// Agents is an ordered fallback pool of Agent-preset-shaped strings (same
	// grammar as --agent). Validation against recognized presets is owned by
	// the command layer (the config package stays free of the tasks package).
	// Empty/unset ⇒ a single agent = implement's default, with no fallback.
	Agents []string `toml:"agents"`
	// PollInterval is the supervisor's scan cadence. Empty ⇒ DefaultQueuePollInterval.
	PollInterval string `toml:"poll_interval"`
	// AgentQuotaRetryAfter is the global cooldown applied after an agent reports
	// a quota exit, before it re-enters rotation. Empty ⇒ DefaultQueueQuotaRetryAfter.
	AgentQuotaRetryAfter string `toml:"agent_quota_retry_after"`
	// CrashRetryDelays is the ordered backoff schedule for crash retries; its
	// length is the park threshold. Empty ⇒ DefaultQueueCrashRetryDelays.
	CrashRetryDelays []string `toml:"crash_retry_delays"`
}

// Queue default values applied when the [queue] section or individual fields
// are omitted.
const (
	DefaultQueuePollInterval    = 60 * time.Second
	DefaultQueueQuotaRetryAfter = time.Hour
)

// DefaultQueueCrashRetryDelays is the default crash-retry backoff schedule.
var DefaultQueueCrashRetryDelays = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute}

// ResolvedQueueConfig holds the parsed queue configuration with defaults
// applied and durations parsed. Agents stay as raw preset-shaped strings;
// preset validation is performed in the command layer.
type ResolvedQueueConfig struct {
	Agents               []string
	PollInterval         time.Duration
	AgentQuotaRetryAfter time.Duration
	CrashRetryDelays     []time.Duration
}

// ResolveQueue parses the [queue] section, applying defaults for omitted
// fields and validating that every duration string is well-formed. A bad
// duration is a config error. The receiver may be nil (no [queue] section), in
// which case all defaults apply.
func (c *Config) ResolveQueue() (ResolvedQueueConfig, error) {
	resolved := ResolvedQueueConfig{
		PollInterval:         DefaultQueuePollInterval,
		AgentQuotaRetryAfter: DefaultQueueQuotaRetryAfter,
		CrashRetryDelays:     append([]time.Duration(nil), DefaultQueueCrashRetryDelays...),
	}

	var q *QueueConfig
	if c != nil {
		q = c.Queue
	}
	if q == nil {
		return resolved, nil
	}

	if len(q.Agents) > 0 {
		resolved.Agents = append([]string(nil), q.Agents...)
	}

	if strings.TrimSpace(q.PollInterval) != "" {
		d, err := time.ParseDuration(q.PollInterval)
		if err != nil {
			return ResolvedQueueConfig{}, fmt.Errorf("[queue] poll_interval: %w", err)
		}
		resolved.PollInterval = d
	}

	if strings.TrimSpace(q.AgentQuotaRetryAfter) != "" {
		d, err := time.ParseDuration(q.AgentQuotaRetryAfter)
		if err != nil {
			return ResolvedQueueConfig{}, fmt.Errorf("[queue] agent_quota_retry_after: %w", err)
		}
		resolved.AgentQuotaRetryAfter = d
	}

	if q.CrashRetryDelays != nil {
		delays := make([]time.Duration, 0, len(q.CrashRetryDelays))
		for i, raw := range q.CrashRetryDelays {
			d, err := time.ParseDuration(raw)
			if err != nil {
				return ResolvedQueueConfig{}, fmt.Errorf("[queue] crash_retry_delays[%d]: %w", i, err)
			}
			delays = append(delays, d)
		}
		resolved.CrashRetryDelays = delays
	}

	return resolved, nil
}

// ProjectEntry represents a project configuration entry.
type ProjectEntry struct {
	Path         string `toml:"path"`
	DisplayDepth int    `toml:"display_depth"` // number of path segments to show in display name; 0 means use default (1)
}

// GetDisplayDepth returns the effective display depth.
// Returns 1 if not explicitly set (DisplayDepth == 0).
func (p ProjectEntry) GetDisplayDepth() int {
	if p.DisplayDepth <= 0 {
		return 1
	}
	return p.DisplayDepth
}

type Config struct {
	Includes              []string             `toml:"includes"`
	Projects              []ProjectEntry       `toml:"projects"`
	Commands              []UserDefinedCommand `toml:"commands"`
	ExcludeCurrentSession bool                 `toml:"exclude_current_session"`
	// Deprecated: use ExcludeCurrentSession. TODO: remove after v1.0.
	ExcludeCurrentDir      bool            `toml:"exclude_current_dir"`
	DisambiguationStrategy string          `toml:"disambiguation_strategy"`
	QuickAccessModifier    string          `toml:"quick_access_modifier"`
	Worktree               *WorktreeConfig `toml:"worktree"`
	Project                *ProjectConfig  `toml:"project"`
	// Deprecated: use Project. TODO: remove at next major release.
	Select         *ProjectConfig        `toml:"select"`
	PaneMonitoring *PaneMonitoringConfig `toml:"pane_monitoring"`
	Dashboard      *DashboardConfig      `toml:"dashboard"`
	// The TOML key stays "workload" for backward compatibility with existing
	// user config files; the rename is internal only.
	Task    *TaskConfig    `toml:"workload"`
	Queue   *QueueConfig   `toml:"queue"`
	Updates *UpdatesConfig `toml:"updates"`

	Warnings []string `toml:"-"` // non-serialized warnings from config loading
}

// TaskAgentOutput returns the configured output mode for one agent preset.
// Defaults to "auto"; validation is owned by the task executor.
func (c *Config) TaskAgentOutput(agent string) string {
	if c == nil || c.Task == nil {
		return "auto"
	}
	agentConfig, ok := c.Task.Agents[agent]
	if !ok || agentConfig.Output == "" {
		return "auto"
	}
	return agentConfig.Output
}

// UpdateNoticeEnabled reports whether the picker Update notice and the daily
// background Update check are enabled. Defaults to true; only an explicit
// [updates] notice_enabled = false disables them (CONTEXT.md "Update check").
// Doctor's live check is user-initiated and not gated by this flag.
func (c *Config) UpdateNoticeEnabled() bool {
	if c == nil || c.Updates == nil || c.Updates.NoticeEnabled == nil {
		return true
	}
	return *c.Updates.NoticeEnabled
}

// ExpandedPath represents a resolved project path with display metadata
type ExpandedPath struct {
	Path         string
	DisplayDepth int  // number of path segments to show in display name
	Explicit     bool // true if the path was listed explicitly (not from a glob)
}

// ShouldExcludeCurrentSession returns true if the current session should be
// excluded from the picker. Supports both the new and deprecated config keys.
func (c *Config) ShouldExcludeCurrentSession() bool {
	return c.ExcludeCurrentSession || c.ExcludeCurrentDir
}

// GetDisambiguationStrategy returns the configured disambiguation strategy.
// Defaults to "first_unique_segment" when not set or invalid.
func (c *Config) GetDisambiguationStrategy() string {
	if c.DisambiguationStrategy == "full_path" {
		return "full_path"
	}
	return "first_unique_segment"
}

// GetQuickAccessModifier returns the configured quick access modifier.
// Defaults to "alt" when not set or invalid.
func (c *Config) GetQuickAccessModifier() string {
	switch c.QuickAccessModifier {
	case "alt", "ctrl", "disabled":
		return c.QuickAccessModifier
	default:
		return "alt"
	}
}

// DismissUnreadInActivePane returns whether unread status should be
// automatically downgraded to clear when the pane is currently active.
// Supports both the new and deprecated config keys.
// Defaults to false.
func (c *Config) DismissUnreadInActivePane() bool {
	if c.PaneMonitoring == nil {
		return false
	}
	return c.PaneMonitoring.DismissUnreadInActivePane || c.PaneMonitoring.DismissAttentionInActivePane
}

// ShouldIgnoreStatusFrom returns whether set-status calls from the given source
// should be ignored.
func (c *Config) ShouldIgnoreStatusFrom(source string) bool {
	if c.PaneMonitoring == nil {
		return false
	}
	for _, s := range c.PaneMonitoring.IgnoreStatusFrom {
		if s == source {
			return true
		}
	}
	return false
}

// CurrentPaneAlwaysUnderCursor returns whether the dashboard should place the
// current tmux pane under the cursor. Defaults to false.
func (c *Config) CurrentPaneAlwaysUnderCursor() bool {
	if c.Dashboard == nil {
		return false
	}
	return c.Dashboard.CurrentPaneAlwaysUnderCursor
}

// DashboardCursorPosition returns the configured initial cursor strategy.
// Defaults to current_registered. The deprecated current_pane_always_under_cursor
// boolean maps to current_any only when cursor_position is not set.
func (c *Config) DashboardCursorPosition() string {
	if c.Dashboard == nil {
		return DashboardCursorCurrentRegistered
	}
	switch c.Dashboard.CursorPosition {
	case DashboardCursorCurrentRegistered, DashboardCursorCurrentAny, DashboardCursorFirstActive:
		return c.Dashboard.CursorPosition
	case "":
		if c.Dashboard.CurrentPaneAlwaysUnderCursor {
			return DashboardCursorCurrentAny
		}
	}
	return DashboardCursorCurrentRegistered
}

// PaneMonitoringTCPServer returns whether the monitor daemon should bind a TCP
// listener for IPC. When false, `pane set-status` writes state directly
// instead of dialing the daemon. Defaults to false.
func (c *Config) PaneMonitoringTCPServer() bool {
	if c.PaneMonitoring == nil {
		return false
	}
	return c.PaneMonitoring.TCPServer
}

// PaneMonitoringAddr returns the pinned monitor daemon address, or "" when
// none is configured (in which case the address is derived from the data dir).
func (c *Config) PaneMonitoringAddr() string {
	if c.PaneMonitoring == nil {
		return ""
	}
	return c.PaneMonitoring.Addr
}

// PaneMonitoringTopicCommand returns the configured topic-derivation command,
// or "" when none is set (in which case topics fall back to prompt truncation).
func (c *Config) PaneMonitoringTopicCommand() string {
	if c.PaneMonitoring == nil {
		return ""
	}
	return c.PaneMonitoring.TopicCommand
}

// DashboardSortCriteria returns the configured sort criteria for the dashboard.
// Defaults to [status, pane_last_active_at, alphabetical].
func (c *Config) DashboardSortCriteria() []string {
	if c.Dashboard == nil || len(c.Dashboard.SortCriteria) == 0 {
		return DefaultSortCriteria
	}
	return c.Dashboard.SortCriteria
}

func (c *Config) projectConfig() *ProjectConfig {
	if c.Project != nil {
		return c.Project
	}
	return c.Select
}

// UnreadNotificationsEnabled returns whether unread notifications are
// enabled for the given mode ("project" or "worktree"). "select" is accepted
// as a deprecated alias for "project". Supports both the new and deprecated
// config keys. Defaults to false.
func (c *Config) UnreadNotificationsEnabled(mode string) bool {
	switch mode {
	case "project", "select":
		pc := c.projectConfig()
		if pc == nil {
			return false
		}
		return pc.UnreadNotificationsEnabled || pc.AttentionNotificationsEnabled
	case "worktree":
		if c.Worktree == nil {
			return false
		}
		return c.Worktree.UnreadNotificationsEnabled || c.Worktree.AttentionNotificationsEnabled
	default:
		return false
	}
}

// CommandsForMode returns the effective custom commands for the given mode
// ("project" or "worktree"). "select" is accepted as a deprecated alias for
// "project". Section-specific commands override global ones matched by key.
func (c *Config) CommandsForMode(mode string) []UserDefinedCommand {
	byKey := make(map[string]UserDefinedCommand)
	for _, cmd := range c.Commands {
		byKey[cmd.Key] = cmd
	}

	var sectionCmds []UserDefinedCommand
	switch mode {
	case "project", "select":
		if pc := c.projectConfig(); pc != nil {
			sectionCmds = pc.Commands
		}
	case "worktree":
		if c.Worktree != nil {
			sectionCmds = c.Worktree.Commands
		}
	}
	for _, cmd := range sectionCmds {
		byKey[cmd.Key] = cmd
	}

	// Collect in stable order: global order first, then section-only additions
	var result []UserDefinedCommand
	seen := make(map[string]bool)
	for _, cmd := range c.Commands {
		result = append(result, byKey[cmd.Key])
		seen[cmd.Key] = true
	}
	for _, cmd := range sectionCmds {
		if !seen[cmd.Key] {
			result = append(result, cmd)
			seen[cmd.Key] = true
		}
	}
	return result
}

// DefaultConfigPath returns the default config file path
func DefaultConfigPath() string {
	return DefaultConfigPathWith(defaultDeps)
}

// DefaultConfigPathWith returns the default config file path using provided dependencies
func DefaultConfigPathWith(d *Deps) string {
	if xdgConfig := d.FS.Getenv("XDG_CONFIG_HOME"); xdgConfig != "" {
		return filepath.Join(xdgConfig, "pop", "config.toml")
	}
	home, err := d.FS.UserHomeDir()
	if err != nil {
		debug.Error("DefaultConfigPath: UserHomeDir: %v", err)
	}
	return filepath.Join(home, ".config", "pop", "config.toml")
}

// Load reads the config file from the given path
func Load(path string) (*Config, error) {
	return LoadWith(defaultDeps, path)
}

// LoadWith reads the config file using provided dependencies for ~ expansion
func LoadWith(d *Deps, path string) (*Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, err
	}

	selectSectionUsed := cfg.Select != nil
	if selectSectionUsed {
		cfg.Warnings = append(cfg.Warnings, "[select] is deprecated; rename to [project]")
		if cfg.Project == nil {
			cfg.Project = cfg.Select
		}
	}

	// Deprecation warnings for the needs_attention → unread rename.
	if cfg.PaneMonitoring != nil && cfg.PaneMonitoring.DismissAttentionInActivePane {
		cfg.Warnings = append(cfg.Warnings, "[pane_monitoring] dismiss_attention_in_active_pane is deprecated; rename to dismiss_unread_in_active_pane")
	}
	if pc := cfg.projectConfig(); pc != nil && pc.AttentionNotificationsEnabled {
		section := "[project]"
		if selectSectionUsed && cfg.Select == pc {
			section = "[select]"
		}
		cfg.Warnings = append(cfg.Warnings, section+" attention_notifications_enabled is deprecated; rename to unread_notifications_enabled")
	}
	if cfg.Select != nil && cfg.Select != cfg.Project && cfg.Select.AttentionNotificationsEnabled {
		cfg.Warnings = append(cfg.Warnings, "[select] attention_notifications_enabled is deprecated; rename to unread_notifications_enabled")
	}
	if cfg.Worktree != nil && cfg.Worktree.AttentionNotificationsEnabled {
		cfg.Warnings = append(cfg.Warnings, "[worktree] attention_notifications_enabled is deprecated; rename to unread_notifications_enabled")
	}

	configDir := filepath.Dir(path)
	for _, include := range cfg.Includes {
		expanded := expandHomeWith(d, include)
		if !filepath.IsAbs(expanded) {
			expanded = filepath.Join(configDir, expanded)
		}

		var included Config
		if _, err := toml.DecodeFile(expanded, &included); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				cfg.Warnings = append(cfg.Warnings, fmt.Sprintf("include file %q not found, skipping", include))
				continue
			}
			return nil, fmt.Errorf("loading include %q: %w", include, err)
		}

		cfg.Projects = append(cfg.Projects, included.Projects...)
	}

	return &cfg, nil
}

// ExpandProjects resolves all project paths from the config
// Supports exact paths and glob patterns like ~/Dev/*/*
func (c *Config) ExpandProjects() ([]ExpandedPath, error) {
	return c.ExpandProjectsWith(defaultDeps)
}

// ExpandProjectsWith resolves all project paths using provided dependencies
func (c *Config) ExpandProjectsWith(d *Deps) ([]ExpandedPath, error) {
	cachePath := DefaultCachePathWith(d)
	cache := loadGlobCache(d, cachePath)
	cacheModified := false

	var projects []ExpandedPath
	seen := make(map[string]bool)

	addProject := func(path string, displayDepth int, explicit bool) {
		if !seen[path] && isDirectoryWith(d, path) {
			seen[path] = true
			projects = append(projects, ExpandedPath{Path: path, DisplayDepth: displayDepth, Explicit: explicit})
		}
	}

	for _, entry := range c.Projects {
		expanded := expandHomeWith(d, entry.Path)
		displayDepth := entry.GetDisplayDepth()

		// Check if it's a glob pattern (only single * allowed, not **)
		if strings.Contains(expanded, "**") {
			continue // Skip recursive glob patterns
		}
		if strings.Contains(expanded, "*") {
			matches, updated, err := expandGlobCached(d, expanded, cache)
			if updated {
				cacheModified = true
			}
			if err != nil {
				continue // Skip invalid patterns
			}
			for _, match := range matches {
				addProject(match, displayDepth, false)
			}
		} else {
			// Exact path - resolve symlinks
			resolved := expanded
			if r, err := d.FS.EvalSymlinks(expanded); err == nil {
				resolved = r
			}
			addProject(resolved, displayDepth, true)
		}
	}

	if cacheModified {
		saveGlobCache(d, cachePath, cache)
	}

	return removeSubsumedPaths(projects), nil
}

// removeSubsumedPaths filters out paths that are strict parents of other paths
// in the set. This implements "more specific wins" — if both /a/b and /a/b/c
// are in the list, /a/b is removed. Works transitively.
// Explicitly listed paths (not from globs) are never subsumed.
func removeSubsumedPaths(paths []ExpandedPath) []ExpandedPath {
	subsumed := make(map[string]bool)
	for _, p := range paths {
		if p.Explicit {
			continue
		}
		for _, q := range paths {
			if p.Path != q.Path && strings.HasPrefix(q.Path, p.Path+"/") {
				subsumed[p.Path] = true
				break
			}
		}
	}

	var result []ExpandedPath
	for _, p := range paths {
		if !subsumed[p.Path] {
			result = append(result, p)
		}
	}
	return result
}

// expandHomeWith replaces ~ with the user's home directory
func expandHomeWith(d *Deps, path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := d.FS.UserHomeDir()
		if err != nil {
			debug.Error("expandHome: UserHomeDir: %v", err)
		}
		return filepath.Join(home, path[2:])
	}
	return path
}

// expandGlobWithBase expands a glob pattern and returns both the matches
// and the resolved base path (after symlink resolution).
func expandGlobWithBase(d *Deps, pattern string) ([]string, string, error) {
	base, pat := doublestar.SplitPattern(pattern)

	// Resolve symlinks in the base path once (e.g., ~/Dev -> /private/Dev)
	resolvedBase := base
	if r, err := d.FS.EvalSymlinks(base); err == nil {
		resolvedBase = r
	}

	fsys := d.FS.DirFS(base)
	matches, err := doublestar.Glob(fsys, pat, doublestar.WithNoHidden())
	if err != nil {
		return nil, "", err
	}

	// Convert to absolute paths using the resolved base
	var results []string
	for _, match := range matches {
		results = append(results, filepath.Join(resolvedBase, match))
	}
	return results, resolvedBase, nil
}

func isDirectoryWith(d *Deps, path string) bool {
	info, err := d.FS.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}
