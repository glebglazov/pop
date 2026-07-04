package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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
	// Trunk resolves the Trunk worktree checkout for a given checkout, used by
	// the Preferred workbench inheritance layer (ADR-0078): a child worktree
	// with no entry of its own inherits the Trunk worktree's runtime entry.
	// Returns (path, true) for a real trunk anchor and ("", false) when there
	// is none (e.g. an unconfigured bare repo — that step is simply skipped).
	// config cannot import tasks/binding, so callers with git access inject
	// this; a nil Trunk disables the inheritance layer.
	Trunk func(checkoutPath string) (path string, ok bool)
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
	Key     string `toml:"key" desc:"Key binding that triggers this command (e.g. \"ctrl-l\")."`
	Label   string `toml:"label" desc:"Display label shown in the picker hint bar."`
	Command string `toml:"command" desc:"Shell command to execute."`
	Exit    bool   `toml:"exit" desc:"Exit the picker after running the command."`
}

// PaneMonitoringConfig holds pane monitoring configuration
type PaneMonitoringConfig struct {
	DismissUnreadInActivePane bool `toml:"dismiss_unread_in_active_pane" desc:"Auto-clear unread status when its pane is the active one."`
	// Deprecated: use DismissUnreadInActivePane. The old key is read for
	// backwards compat; a warning is emitted when it is present.
	DismissAttentionInActivePane bool     `toml:"dismiss_attention_in_active_pane" desc:"Deprecated: use dismiss_unread_in_active_pane."`
	IgnoreStatusFrom             []string `toml:"ignore_status_from" desc:"Status sources to ignore (array of source names)."`
	TCPServer                    bool     `toml:"tcp_server" desc:"Bind a TCP listener for daemon IPC instead of direct state writes."`
	// Addr pins the monitor daemon's TCP address (host:port). Empty means the
	// address is derived from the data dir (ADR 0021). A pinned addr is shared
	// across any data dirs using this config, so only pin it for single-instance
	// setups (e.g. exposing a fixed port to containers).
	Addr string `toml:"addr" desc:"Pin the monitor daemon's TCP address (host:port); empty derives it from the data dir."`
	// TopicAgents is the ordered list of typed Topic derivation steps (ADR 0068).
	// Each entry is a truncate step (local prompt truncation → seed) or an agent
	// step (a curated agent-CLI Topic recipe → final). A bare string is sugar for
	// { type = "agent", command = "<string>" }. Each step carries a set_if guard
	// checked against @pop_topic_kind. Unset/nil defaults to a single truncate
	// step; an explicit empty array disables derivation. pop links no model SDK
	// and holds no keys — auth lives in the CLIs.
	TopicAgents TopicSteps `toml:"topic_agents" desc:"Ordered Topic-derivation steps (truncate/agent recipes)."`
	// TopicWords bounds the word count of a derived Topic after it is normalized
	// into a kebab slug (ADR 0057). Zero/unset means the default
	// (DefaultTopicWords); see PaneMonitoringTopicWords.
	TopicWords int `toml:"topic_words" desc:"Max words in a derived Topic slug (0 = default)."`
	// TopicDerivationTimeout bounds, in seconds, how long each topic_agents recipe
	// may run before pop kills it and falls through to the next recipe (then to
	// prompt truncation). Large local models (e.g. a multi-GB ollama model that
	// must cold-load) need more than the default; see PaneMonitoringTopicDerivationTimeout.
	// Zero/unset means the default (DefaultTopicDerivationTimeoutSeconds).
	TopicDerivationTimeout int `toml:"topic_derivation_timeout" desc:"Per-recipe topic-derivation timeout in seconds (0 = default)."`
}

// DefaultTopicWords is the word cap applied to a derived Topic slug when
// [pane_monitoring] topic_words is unset.
const DefaultTopicWords = 5

// DefaultTopicDerivationTimeoutSeconds is the per-recipe timeout applied to a
// topic-derivation recipe when [pane_monitoring] topic_derivation_timeout is unset.
// 30s gives a multi-GB local model room to cold-load before pop falls through.
const DefaultTopicDerivationTimeoutSeconds = 30

// DashboardConfig holds dashboard-specific configuration
type DashboardConfig struct {
	CurrentPaneAlwaysUnderCursor bool     `toml:"current_pane_always_under_cursor" desc:"Deprecated: place the current pane under the cursor (use cursor_position)."`
	CursorPosition               string   `toml:"cursor_position" desc:"Initial cursor strategy (current_registered|current_any|first_active)."`
	SortCriteria                 []string `toml:"sort_criteria" desc:"Dashboard sort order (array of status|pane_last_active_at|session_last_visit_at|alphabetical)."`
	ZoomOnSwitch                 *bool    `toml:"zoom_on_switch" desc:"Zoom the target pane when switching to it."`
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
	Commands                   []UserDefinedCommand `toml:"commands" desc:"User-defined commands for the worktree picker."`
	UnreadNotificationsEnabled bool                 `toml:"unread_notifications_enabled" desc:"Enable unread-status notifications in worktree mode."`
	// Deprecated: use UnreadNotificationsEnabled. The old key is read for
	// backwards compat; a warning is emitted when it is present.
	AttentionNotificationsEnabled bool `toml:"attention_notifications_enabled" desc:"Deprecated: use unread_notifications_enabled."`
}

// ProjectConfig holds project-picker-specific configuration
type ProjectConfig struct {
	Commands                   []UserDefinedCommand `toml:"commands" desc:"User-defined commands for the project picker."`
	UnreadNotificationsEnabled bool                 `toml:"unread_notifications_enabled" desc:"Enable unread-status notifications in project mode."`
	// Deprecated: use UnreadNotificationsEnabled. The old key is read for
	// backwards compat; a warning is emitted when it is present.
	AttentionNotificationsEnabled bool `toml:"attention_notifications_enabled" desc:"Deprecated: use unread_notifications_enabled."`
}

// Integration skill alias values for optional integration components.
const (
	IntegrationSkillTasks = "tasks"
	IntegrationSkillPane  = "pane"
)

// DefaultIntegrationSkills is the embedded pop default for [integrations] skills.
var DefaultIntegrationSkills = []string{IntegrationSkillTasks, IntegrationSkillPane}

// DefaultSkillsPrefix is the prefix applied to every embedded skill's installed
// name when [integrations] skills_prefix is absent. With it, the installed name
// of an embedded skill is `pop-<base>` — byte-identical to pop's original
// behaviour (ADR 0063).
const DefaultSkillsPrefix = "pop-"

// IntegrationsConfig holds global integration preferences (ADR 0065).
type IntegrationsConfig struct {
	Skills       []string `toml:"skills" desc:"Embedded skills to install (array of skill aliases)."`
	SkillsPrefix *string  `toml:"skills_prefix" desc:"Prefix for installed skill names (default \"pop-\"; empty = bare names)."`
}

// ResolveSkillsPrefix returns the configured skill-name prefix. An absent
// [integrations] section or skills_prefix key resolves to DefaultSkillsPrefix
// (`pop-`); an explicit empty string resolves to "" (bare base names). The
// receiver may be nil.
func (c *Config) ResolveSkillsPrefix() string {
	if c == nil || c.Integrations == nil || c.Integrations.SkillsPrefix == nil {
		return DefaultSkillsPrefix
	}
	return *c.Integrations.SkillsPrefix
}

// UpdatesConfig holds update-check / Update-notice configuration.
type UpdatesConfig struct {
	// NoticeEnabled gates both the picker Update notice and the daily
	// background Update check. A nil pointer (absent section or key) defaults
	// to enabled; an explicit false disables both so pop makes zero automatic
	// network calls (CONTEXT.md "Update check", "Update notice").
	NoticeEnabled *bool `toml:"notice_enabled" desc:"Enable the update notice and daily background update check (default true)."`
}

// TasksConfig holds task-execution configuration under the [tasks] TOML table.
type TasksConfig struct {
	// Implement holds the ordered worker fallback list for `pop tasks implement`.
	Implement *ImplementConfig `toml:"implement" desc:"Implement sub-command settings ([tasks.implement] table)."`
	// Presets is the per-preset settings map (e.g. output mode). Keyed by agent
	// preset name. Renamed from [workload.agents] so "agents" no longer means
	// both an ordered list and a settings map.
	Presets map[string]TaskAgentConfig `toml:"presets" desc:"Per-agent preset settings ([tasks.presets.<name>] tables)."`
	// Git holds commit-time git overrides for Pop's own commits.
	Git *TaskGitConfig `toml:"git" desc:"Commit-time git overrides for Pop's commits ([tasks.git] table)."`
	// Verify holds Agent-verification settings (ADR-0086).
	Verify *VerifyConfig `toml:"verify" desc:"Agent-verification settings ([tasks.verify] table)."`
}

// ImplementConfig holds settings for the `pop tasks implement` sub-command.
type ImplementConfig struct {
	// Agents is the ordered in-process fallback list used by
	// `pop tasks implement` for unpinned tasks when --agent is absent.
	Agents []string `toml:"agents" desc:"Ordered fallback agent list for unpinned tasks."`
}

// VerifyConfig holds Agent-verification settings (ADR-0086). It is the
// master, off-by-default gate for the feature: only when Enabled does status
// derivation gate Done on a SHA-keyed Verify verdict. Agents and Effort steer
// which agent renders that verdict and at what model strength.
type VerifyConfig struct {
	// Enabled is the master opt-in switch. Absent/false ⇒ the Verifier never
	// runs and status derives from the manifest alone, exactly as before this
	// feature (ADR-0086/0087).
	Enabled bool `toml:"enabled" desc:"Enable Agent verification as a Done gate (default false)."`
	// Agents is the ordered fallback list of agent presets the Verifier walks,
	// mirroring [tasks.implement].agents: it falls through to the next agent on
	// a quota pause or a missing binary. An empty list falls back to
	// [tasks.implement].agents (and, failing that, the built-in default agent).
	Agents []string `toml:"agents" desc:"Ordered fallback agent list for the Verifier (falls back to [tasks.implement].agents when omitted)."`
	// Effort selects the Verifier's model-strength tier (light, standard, or
	// heavy). Absent ⇒ heavy — verification runs at the strongest tier by default.
	Effort string `toml:"effort" desc:"Verifier model-strength tier: light, standard, or heavy (default heavy)."`
	// MaxRemediationDepth bounds the verify→remediate→re-verify loop (ADR-0086):
	// a FIXABLE verdict spawns a Remediation task only while the set is under this
	// many cycles, after which it parks at VERIFY-FAILED. A nil pointer ⇒ the
	// built-in default; a value ≤ 0 disables remediation (a FIXABLE verdict parks
	// immediately).
	MaxRemediationDepth *int `toml:"max_remediation_depth" desc:"Max verify→remediate cycles before parking at VERIFY-FAILED (default 3)."`
}

// WorkloadConfig is the deprecated [workload] table, the predecessor of
// [tasks] (ADR-0092). Old configs using [workload] still load and behave
// identically to their [tasks.*] equivalent; a load-time deprecation warning
// names the replacement. The structural mapping is:
//
//	[workload] default_agents  → [tasks.implement].agents
//	[workload.verify]          → [tasks.verify]
//	[workload.git]             → [tasks.git]
//	[workload.agents.<name>]   → [tasks.presets.<name>]
type WorkloadConfig struct {
	DefaultAgents []string                               `toml:"default_agents" desc:"Deprecated: use [tasks.implement].agents."`
	Verify        *WorkloadVerifyConfig                  `toml:"verify" desc:"Deprecated: use [tasks.verify]."`
	Git           *TaskGitConfig                         `toml:"git" desc:"Deprecated: use [tasks.git]."`
	Agents        map[string]WorkloadAgentConfig         `toml:"agents" desc:"Deprecated: use [tasks.presets]."`
}

// WorkloadVerifyConfig is the deprecated [workload.verify] table. Fields match
// the old shape; MaxRetries is the pre-rename name for MaxRemediationDepth.
type WorkloadVerifyConfig struct {
	Enabled    bool     `toml:"enabled" desc:"Deprecated: use [tasks.verify].enabled."`
	Agents     []string `toml:"agents" desc:"Deprecated: use [tasks.verify].agents."`
	Effort     string   `toml:"effort" desc:"Deprecated: use [tasks.verify].effort."`
	MaxRetries int      `toml:"max_retries" desc:"Deprecated: use [tasks.verify].max_remediation_depth."`
}

// WorkloadAgentConfig is the deprecated [workload.agents.<name>] table,
// renamed to [tasks.presets.<name>] in ADR-0092.
type WorkloadAgentConfig struct {
	Output string `toml:"output" desc:"Deprecated: use [tasks.presets.<name>].output."`
}

// TaskAgentConfig holds configuration for one task agent preset.
type TaskAgentConfig struct {
	Output string `toml:"output" desc:"Output mode for this agent preset."`
}

// TaskGitConfig holds commit-time git configuration applied to Pop's own
// commits during a task drain (e.g. disabling GPG signing so an unattended
// queue drain never hangs on a 1Password presence prompt).
type TaskGitConfig struct {
	// CommitConfigOverrides is a list of git `-c`-style `key=value` strings
	// (e.g. "commit.gpgsign=false") prepended as `-c key=value` pairs to Pop's
	// commit invocations. Absent/empty ⇒ no overrides. Validation is lazy: see
	// Config.ResolveCommitConfigOverrides.
	CommitConfigOverrides []string `toml:"commit_config_overrides" desc:"git -c key=value overrides prepended to Pop's commits (array)."`
}

// WorkbenchOptions holds [workbench] table options.
type WorkbenchOptions struct {
	// PickOnCreate gates the picker create-path Workbench prompt (ADR-0075).
	// When true, selecting a project with no live session and ≥1 resolved
	// Workbench shows a quick-search list to pick a Workbench (or "no workbench")
	// before the session is created. Default false ⇒ the project picker behaves
	// exactly as today (no prompt).
	PickOnCreate bool `toml:"pick_on_create" desc:"Prompt to pick a Workbench when creating a session with no live one."`

	// Order fixes the display sequence of the interactive Workbench lists (the
	// create prompt and the Preferred-workbench picker). Tokens are the literal
	// on-screen labels: Workbench names plus the special options "<empty>" and
	// "<reset>". Named tokens front-load in the listed order; everything unnamed
	// follows in default order ("<empty>", Workbenches in resolution order,
	// "<reset>"). A token that resolves to nothing is ignored. Global-only.
	Order []string `toml:"order" desc:"Fixed display order of Workbench-list tokens (array of on-screen labels)."`
}

// Workbench is a named blueprint for an ordered list of tmux windows,
// each with a named pane tree. Split trees and multi-window templates are now
// supported; a template with invalid window names is excluded at load time.
type Workbench struct {
	Name string `toml:"name" desc:"Workbench name (referenced by preferred_workbench)."`
	// BeforeApply is an ordered list of shell commands run for one-time
	// side effects (repo setup: pull, decrypt, mkdir) before any window of
	// this Workbench is realized, with cwd = the session directory (ADR-0075).
	// They run on every apply, including a reapply over a live session — the
	// caller owns idempotency. This is side-effecting commands only, not
	// shell-environment propagation: exported vars would not reach sibling panes.
	BeforeApply []string          `toml:"before_apply" desc:"Shell commands run once before realizing windows (array)."`
	Windows     []WorkbenchWindow `toml:"windows" desc:"Ordered tmux windows ([[workbenches.windows]] tables)."`
}

type WorkbenchWindow struct {
	Name   string             `toml:"name" desc:"Window name."`
	Layout *WorkbenchPaneSpec `toml:"layout" desc:"Root pane layout for the window ([workbenches.windows.layout] table)."`
}

type WorkbenchPaneSpec struct {
	Name    string `toml:"name" desc:"Pane name."`
	Command string `toml:"command" desc:"Shell command to run in this leaf pane."`
	// Children is "rows" (stacked top-to-bottom) or "columns" (side-by-side). Only
	// meaningful when Panes is non-empty (making this a container node).
	Children string `toml:"children" desc:"Split direction for child panes (rows|columns); container nodes only."`
	// Panes holds child pane specs. When non-empty, this node is a container
	// and Command is ignored. When empty, this is a leaf node.
	Panes []WorkbenchPaneSpec `toml:"panes" desc:"Child pane specs; non-empty makes this a container node."`
	// Weight is the relative size within siblings. Defaults to 1 when omitted.
	Weight int `toml:"weight" desc:"Relative size within siblings (default 1)."`
	// Cwd is the working directory for this pane and its descendants.
	// Relative paths are resolved against the session directory; ~ and
	// absolute paths are accepted. Empty means inherit the parent cwd,
	// defaulting to the session directory at the root.
	Cwd string `toml:"cwd" desc:"Working directory for this pane and its descendants."`
	// Focus requests that this pane be the focused pane after the template
	// is applied. Only meaningful on leaf panes. If multiple panes request
	// focus, the first one wins and a warning is emitted.
	Focus bool `toml:"focus" desc:"Focus this pane after the template is applied (leaf panes only)."`
}

// EffortModel is one entry in an effort ladder. Reasoning is optional because
// not every agent has a reasoning-effort mechanism.
type EffortModel struct {
	Model     string `toml:"model" desc:"Model identifier for this ladder entry."`
	Reasoning string `toml:"reasoning" desc:"Reasoning-effort level (optional; agent-dependent)."`
}

// EffortConfig holds the model/reasoning ladder for one agent preset. Each
// tier is an ordered, user-owned fallback list; current resolution uses the
// head entry.
type EffortConfig struct {
	Heavy    []EffortModel `toml:"heavy" desc:"Heavy-tier model/reasoning ladder (array)."`
	Standard []EffortModel `toml:"standard" desc:"Standard-tier model/reasoning ladder (array)."`
	Light    []EffortModel `toml:"light" desc:"Light-tier model/reasoning ladder (array)."`
}

// ResolveCommitConfigOverrides validates the [tasks.git]
// commit_config_overrides entries and returns them as `key=value` strings ready
// to be prepended as `-c key=value` pairs to Pop's commit invocations. Each
// entry must split into a non-empty key on the first `=` (an empty value is
// legal git, e.g. "user.signingkey=").
//
// Validation is deliberately lazy — this is called only from the task drain
// path, never at global config load — so a typo never breaks the picker or
// dashboard. A malformed entry is a hard error: callers must fail the drain
// rather than silently proceed (proceeding could re-trigger the very signing
// hang this feature exists to prevent). The receiver may be nil (no [tasks]
// or [tasks.git] section), in which case no overrides apply.
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
	// PollInterval is the supervisor's scan cadence. Empty ⇒ DefaultQueuePollInterval.
	PollInterval string `toml:"poll_interval" desc:"Supervisor scan cadence as a duration string (e.g. \"60s\")."`
	// AgentQuotaRetryAfter is the global cooldown applied after an agent reports
	// a quota exit, before it re-enters rotation. Empty ⇒ DefaultQueueQuotaRetryAfter.
	AgentQuotaRetryAfter string `toml:"agent_quota_retry_after" desc:"Cooldown after an agent quota exit, as a duration string."`
	// CrashRetryDelays is the ordered backoff schedule for crash retries; its
	// length is the park threshold. Empty ⇒ DefaultQueueCrashRetryDelays.
	CrashRetryDelays []string `toml:"crash_retry_delays" desc:"Crash-retry backoff schedule (array of duration strings); length = park threshold."`
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
// applied and durations parsed.
type ResolvedQueueConfig struct {
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
	Path         string `toml:"path" desc:"Exact path or glob pattern to a project directory."`
	DisplayDepth int    `toml:"display_depth" desc:"Trailing path segments to show in the picker name (0 = default 1)."`

	// displayDepthInvalid records that the configured display_depth had the
	// wrong type (e.g. a string) so the value could not be decoded. Per ADR 0054
	// a non-essential bad value must not abort the load: UnmarshalTOML keeps the
	// rest of the entry, sets this flag, and GetDisplayDepth surfaces it as a
	// finding while falling back to the default depth.
	displayDepthInvalid bool
}

// UnmarshalTOML tolerantly decodes a single project entry. A wrong-typed
// display_depth (the only non-essential field) is recorded as invalid rather
// than aborting the whole config decode — BurntSushi stops at the first type
// error otherwise, dropping every later entry too (ADR 0054). A non-table entry
// or a non-string path is still an error: the projects list is essential, so a
// malformed entry is fatal to the command that consumes it.
func (p *ProjectEntry) UnmarshalTOML(v interface{}) error {
	m, ok := v.(map[string]interface{})
	if !ok {
		return fmt.Errorf("project entry must be a table, got %T", v)
	}
	if raw, present := m["path"]; present {
		s, ok := raw.(string)
		if !ok {
			return fmt.Errorf("project entry path must be a string, got %T", raw)
		}
		p.Path = s
	}
	if raw, present := m["display_depth"]; present {
		switch n := raw.(type) {
		case int64:
			p.DisplayDepth = int(n)
		case int:
			p.DisplayDepth = n
		default:
			p.displayDepthInvalid = true
		}
	}
	return nil
}

// GetDisplayDepth returns the effective display depth and an error iff the
// configured display_depth was the wrong type. Per ADR 0054 the caller decides
// severity: this value is non-essential, so the project dashboard ignores the
// error and uses the returned default (1). The error carries a Finding so the
// problem still surfaces in the warning banner.
func (p ProjectEntry) GetDisplayDepth() (int, error) {
	if p.displayDepthInvalid {
		return 1, Finding{
			Path:    "projects[].display_depth",
			Message: fmt.Sprintf("projects entry %q has a non-integer display_depth; using default depth 1", p.Path),
		}
	}
	if p.DisplayDepth <= 0 {
		return 1, nil
	}
	return p.DisplayDepth, nil
}

// Finding is a single config validation problem, keyed to the config path of
// the offending key (e.g. "effort.opencode.extreme") and carrying a
// human-readable, file-qualified message. Per ADR 0054 findings are collected
// on the loaded Config rather than thrown: a command that never consumes the
// offending key still renders and surfaces the finding only as a non-blocking
// warning, while a command that does consume it can treat the matching getter's
// error as fatal. Finding implements error so a value getter can return it
// directly.
type Finding struct {
	// Path is the dotted config path of the offending key.
	Path string
	// Message is a human-readable, file-qualified description of the problem.
	Message string
}

// Error makes Finding usable as the error returned by a value getter.
func (f Finding) Error() string { return f.Message }

type Config struct {
	Includes              []string             `toml:"includes" desc:"Additional config files to merge in (paths, later wins)."`
	Projects              []ProjectEntry       `toml:"projects" desc:"Directories or globs offered in the project picker."`
	Commands              []UserDefinedCommand `toml:"commands" desc:"User-defined commands surfaced in the picker."`
	ExcludeCurrentSession bool                 `toml:"exclude_current_session" desc:"Hide the current tmux session from the picker."`
	// Deprecated: use ExcludeCurrentSession. TODO: remove after v1.0.
	ExcludeCurrentDir      bool            `toml:"exclude_current_dir" desc:"Deprecated: use exclude_current_session."`
	DisambiguationStrategy string          `toml:"disambiguation_strategy" desc:"How to shorten duplicate display names (first_unique_segment|full_path)."`
	QuickAccessModifier    string          `toml:"quick_access_modifier" desc:"Modifier for quick-access hotkeys (alt|ctrl|disabled)."`
	Worktree               *WorktreeConfig `toml:"worktree" desc:"Worktree dashboard behavior ([worktree] table)."`
	Project                *ProjectConfig  `toml:"project" desc:"Project dashboard behavior ([project] table)."`
	// Deprecated: use Project. TODO: remove at next major release.
	Select         *ProjectConfig        `toml:"select" desc:"Deprecated: use [project]."`
	PaneMonitoring *PaneMonitoringConfig `toml:"pane_monitoring" desc:"Pane attention/status monitoring daemon settings ([pane_monitoring] table)."`
	Dashboard      *DashboardConfig      `toml:"dashboard" desc:"Shared dashboard and cursor behavior ([dashboard] table)."`
	Task   *TasksConfig            `toml:"tasks" desc:"Task-set execution defaults ([tasks] table)."`
	// Deprecated: use Task. The [workload] table was renamed to [tasks] in
	// ADR-0092. Old configs still load and warn; the alias is structural
	// (not 1:1). Removal is gated in CLEANUP.md.
	Workload *WorkloadConfig `toml:"workload" desc:"Deprecated: use [tasks] (ADR-0092)."`
	Effort map[string]EffortConfig `toml:"effort" desc:"Per-agent reasoning-effort ladders ([effort.<agent>] tables)."`
	// Workbenches is the canonical TOML key for session blueprints.
	Workbenches []Workbench `toml:"workbenches" desc:"Global session blueprints (templates)."`
	// WorkbenchOpts holds the [workbench] options table (empty for now).
	WorkbenchOpts *WorkbenchOptions   `toml:"workbench" desc:"Workbench options ([workbench] table)."`
	Queue         *QueueConfig        `toml:"queue" desc:"Queue supervisor settings ([queue] table)."`
	Updates       *UpdatesConfig      `toml:"updates" desc:"Auto-update behavior ([updates] table)."`
	Integrations  *IntegrationsConfig `toml:"integrations" desc:"AI-agent integration settings ([integrations] table)."`
	// Repo holds [repo."<path>"] override blocks keyed by any checkout path.
	// The key is canonicalized (~ expanded, symlinks resolved) at resolution
	// time; any worktree path or bare dir of the same repo resolves to the
	// same repository identity.
	Repo map[string]RepoOverrideConfig `toml:"repo" desc:"Per-repo override blocks keyed by any checkout path ([repo.\"<path>\"] tables)."`

	// Findings holds semantic config problems collected at load time (ADR 0054).
	// Each is keyed to its config path; callers consult them through value
	// getters (e.g. EffortFor) and decide severity per their capability. They
	// are also mirrored into Warnings so a command that never consumes the
	// offending key still surfaces the problem in the picker's warning banner.
	Findings []Finding `toml:"-"`

	Warnings []string `toml:"-"` // non-serialized warnings from config loading
}

// recordFinding appends a finding and mirrors its message into Warnings, so a
// command that never consumes the offending key still surfaces it in the
// non-blocking picker banner (ADR 0054).
func (c *Config) recordFinding(f Finding) {
	c.Findings = append(c.Findings, f)
	c.Warnings = append(c.Warnings, f.Message)
}

// blockingFindingFor returns the first finding whose config path lies under the
// given top-level section (an exact match or a "<section>." prefix), or nil. A
// value getter for that section returns this as its error so the caller decides
// whether the problem is fatal to its capability (ADR 0054).
func (c *Config) blockingFindingFor(section string) error {
	if c == nil {
		return nil
	}
	for i := range c.Findings {
		p := c.Findings[i].Path
		if p == section || strings.HasPrefix(p, section+".") {
			return c.Findings[i]
		}
	}
	return nil
}

// EffortFor returns the configured effort ladder for an agent preset. The error
// is non-nil iff a blocking effort finding exists (an invalid [effort] tier or
// entry key); per ADR 0054 the caller decides severity — fatal if it consumes
// effort, otherwise fall back to defaults. When no effort finding exists the
// error is nil and the returned EffortConfig is the agent's ladder (the zero
// value if the agent is unconfigured).
func (c *Config) EffortFor(agent string) (EffortConfig, error) {
	if err := c.blockingFindingFor("effort"); err != nil {
		return EffortConfig{}, err
	}
	if c == nil || c.Effort == nil {
		return EffortConfig{}, nil
	}
	return c.Effort[agent], nil
}

// ProjectEntries returns the configured project list and an error iff a
// blocking finding lands on the projects section's essentials. Per ADR 0054 the
// projects list is essential to the project dashboard, so the call site treats
// this error as fatal — there is nothing to switch to without it. A
// non-essential per-entry finding (e.g. a bad display_depth, keyed under
// "projects[]...") is deliberately not matched here, so it never makes the list
// fatal.
func (c *Config) ProjectEntries() ([]ProjectEntry, error) {
	if c == nil {
		return nil, nil
	}
	if err := c.blockingFindingFor("projects"); err != nil {
		return c.Projects, err
	}
	return c.Projects, nil
}

// RepoScopeConfig is the single shared repo-scope key set (ADR-0083). Every key
// here is accepted at BOTH repo-scope loci: the committed repo-root .pop.toml
// and the user's central global [repo."<path>"] override block. Adding a
// repo-scope key here makes both surfaces accept it without touching two structs.
// trunk is the sole exception — it is per-checkout machine topology, never valid
// in committed .pop.toml — so it lives on the individual structs, not here.
type RepoScopeConfig struct {
	// Workbenches are repo-scope session blueprints (canonical key).
	Workbenches []Workbench `toml:"workbenches" desc:"Repo-scope session blueprints (templates)."`
	// PreferredWorkbench names the repo-default Workbench that auto-applies when
	// a session is born for any checkout of this repo (ADR-0078). It is keyed by
	// repository identity, not the exact checkout path, so it is a coarse default
	// shared by every worktree of the repo. Readable from committed .pop.toml as
	// well as the global override; the override wins for the same key (ADR-0083).
	PreferredWorkbench string `toml:"preferred_workbench" desc:"Repo-default Workbench that auto-applies to new sessions of this repo."`
}

// RepoConfig is the repo-root .pop.toml surface. It is deliberately separate
// from Config: global config.toml registers projects, while .pop.toml only
// describes behavior for an already-registered project. It carries the shared
// repo-scope key set plus a non-decoded Trunk slot (populated only by resolution
// from a global override, never parsed from .pop.toml).
type RepoConfig struct {
	RepoScopeConfig
	// Trunk marks a specific checkout as the Trunk worktree — the repository's
	// fork base for managed worktrees. Meaningful only in a [repo."<path>"]
	// global override block keyed to that checkout; a bare repo must declare
	// trunk = true to enable managed-worktree provisioning. Repo-local .pop.toml
	// cannot name a machine-specific trunk, so this is never decoded (toml:"-").
	Trunk bool `toml:"-"`

	// Findings holds non-fatal scope-legality problems collected while loading
	// this .pop.toml (ADR-0054, ADR-0083): top-level keys that are not repo-scope
	// keys (global/machine-only settings, or the [repo]-only trunk) are ignored
	// but surfaced here so a command can render them in the picker warning banner.
	Findings []Finding `toml:"-"`
}

// RepoOverrideConfig is the shape of a [repo."<path>"] block in global
// config.toml. It accepts the shared repo-scope key set plus the [repo]-only
// trunk key; global-only settings (project registry, daemon knobs, etc.) are not.
type RepoOverrideConfig struct {
	RepoScopeConfig
	// Trunk is meaningful only for the specific checkout path that keys this
	// block; it is not propagated to other worktrees of the same repo.
	Trunk *bool `toml:"trunk" desc:"Mark this exact checkout as the repo's Trunk (fork base for managed worktrees)."`
}

// LoadRepoConfig reads repo-root .pop.toml. A missing file is not an error and
// resolves to the zero config. Malformed TOML is returned to the caller so it
// can be reported while degrading behavior to defaults.
func LoadRepoConfig(repoRoot string) (RepoConfig, error) {
	return LoadRepoConfigWith(defaultDeps, repoRoot)
}

// LoadRepoConfigWith reads repo-root .pop.toml using injected dependencies.
func LoadRepoConfigWith(d *Deps, repoRoot string) (RepoConfig, error) {
	path := filepath.Join(repoRoot, ".pop.toml")
	data, err := d.FS.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return RepoConfig{}, nil
		}
		return RepoConfig{}, err
	}
	var cfg RepoConfig
	md, err := toml.Decode(string(data), &cfg)
	if err != nil {
		return RepoConfig{}, fmt.Errorf("%s: %w", path, err)
	}
	if err := validateRepoConfigMetadata(path, md); err != nil {
		return RepoConfig{}, err
	}
	// Scope-legality (ADR-0083): only shared repo-scope keys are honored in
	// .pop.toml. Global/machine-only keys (and the [repo]-only trunk) are ignored
	// but surfaced as non-fatal findings so the rest of the file still loads.
	cfg.Findings = popTOMLScopeFindings(path, md)
	return cfg, nil
}

// canonicalPath expands ~ and resolves symlinks, returning a clean absolute path.
func canonicalPath(d *Deps, path string) string {
	p := expandHomeWith(d, path)
	if r, err := d.FS.EvalSymlinks(p); err == nil {
		p = r
	}
	return filepath.Clean(p)
}

// repoIdentity resolves path to its repository identity using filesystem checks
// only (no git commands). For pop-style bare repos (directories containing a
// .bare/ subdir), the identity is the bare repo root. For all other paths the
// identity is the canonicalized path itself. Two worktrees of the same bare
// repo therefore share the same identity.
func repoIdentity(d *Deps, path string) string {
	canon := canonicalPath(d, path)
	current := canon
	for {
		if info, err := d.FS.Stat(filepath.Join(current, ".bare")); err == nil && info.IsDir() {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return canon
}

// ResolveRepoConfig returns the effective RepoConfig for checkoutPath by merging:
//
//	global [repo."<path>"] override → .pop.toml → built-in default (false for all bools)
//
// Fields are merged individually; a nil pointer in the override means "not set"
// and the next layer wins. trunk exists only in global override blocks and is
// applied only when the override's key path exactly matches checkoutPath
// (per-checkout semantics).
//
// A missing .pop.toml is not an error. A malformed .pop.toml degrades to the
// zero config (the error is returned so callers may surface a warning).
func (c *Config) ResolveRepoConfig(d *Deps, checkoutPath string) (RepoConfig, error) {
	// A renamed execution key (queue_base/execution_base → trunk) is recorded at
	// load as a blocking "repo" finding rather than aborting Load(). This getter
	// is the execution-config consumption point, so it surfaces that finding as
	// its error (ADR 0054): consuming commands treat it as fatal, the migration
	// tripwire stays loud, while a command that never resolves repo config (the
	// project dashboard) is unaffected. Checked before touching .pop.toml so the
	// fatal config-global finding always wins over a per-checkout problem.
	if err := c.blockingFindingFor("repo"); err != nil {
		return RepoConfig{}, err
	}
	canon := canonicalPath(d, checkoutPath)
	identity := repoIdentity(d, checkoutPath)

	// Find the matching global override block, if any.
	var override *RepoOverrideConfig
	var executionBaseApplies bool
	if c != nil {
		for rawKey, block := range c.Repo {
			keyCanon := canonicalPath(d, rawKey)
			keyIdentity := repoIdentity(d, rawKey)
			if keyIdentity != identity {
				continue
			}
			b := block
			override = &b
			executionBaseApplies = (keyCanon == canon)
		}
	}

	// Load .pop.toml from the repo identity root (may be zero config).
	popTOML, popErr := LoadRepoConfigWith(d, identity)

	// Merge: start with .pop.toml, then layer global override on top. For any
	// shared repo-scope key the personal [repo."<path>"] value beats the repo's
	// committed .pop.toml (ADR-0083 repo-scope ordering). trunk is per-checkout,
	// applied only when the override's key path exactly matches checkoutPath.
	result := popTOML
	if override != nil {
		if override.PreferredWorkbench != "" {
			result.PreferredWorkbench = override.PreferredWorkbench
		}
		if override.Trunk != nil && executionBaseApplies {
			result.Trunk = *override.Trunk
		}
	}

	return result, popErr
}

// ResolveWorkbenchesWith returns the union of Workbenches from all three homes
// (global config, .pop.toml, and [repo."<path>"]), resolved with
// most-specific-wins precedence: [repo."<path>"] > .pop.toml > global library.
// Name collisions emit warnings. A bare repo's .pop.toml templates are visible
// from all its worktrees via Repository identity.
func (c *Config) ResolveWorkbenchesWith(d *Deps, checkoutPath string) ([]Workbench, []string) {
	// Start with global templates (lowest precedence, already validated at Load)
	var result []Workbench
	seen := make(map[string]string) // name -> source for collision warnings
	var warnings []string

	if c != nil {
		for _, tmpl := range c.Workbenches {
			result = append(result, tmpl)
			seen[tmpl.Name] = "global config"
		}
	}

	// Load .pop.toml from repo identity root (medium precedence)
	identity := repoIdentity(d, checkoutPath)
	popTOML, _ := LoadRepoConfigWith(d, identity)
	for _, tmpl := range popTOML.Workbenches {
		if source, exists := seen[tmpl.Name]; exists {
			warnings = append(warnings, fmt.Sprintf(
				"session template %q defined in both %s and .pop.toml; using .pop.toml",
				tmpl.Name, source,
			))
			// Remove the lower-precedence version
			for i := len(result) - 1; i >= 0; i-- {
				if result[i].Name == tmpl.Name {
					result = append(result[:i], result[i+1:]...)
					break
				}
			}
		}
		result = append(result, tmpl)
		seen[tmpl.Name] = ".pop.toml"
	}

	// Find matching [repo."<path>"] override (highest precedence)
	if c != nil {
		for rawKey, block := range c.Repo {
			keyCanon := canonicalPath(d, rawKey)
			keyIdentity := repoIdentity(d, rawKey)
			// Match by identity (repo-level) not exact path (worktree-level)
			if keyIdentity != identity {
				continue
			}
			for _, tmpl := range block.Workbenches {
				if source, exists := seen[tmpl.Name]; exists {
					warnings = append(warnings, fmt.Sprintf(
						"session template %q defined in both %s and [repo.%q]; using [repo.%q]",
						tmpl.Name, source, keyCanon, keyCanon,
					))
					// Remove the lower-precedence version
					for i := len(result) - 1; i >= 0; i-- {
						if result[i].Name == tmpl.Name {
							result = append(result[:i], result[i+1:]...)
							break
						}
					}
				}
				result = append(result, tmpl)
				seen[tmpl.Name] = fmt.Sprintf("[repo.%q]", keyCanon)
			}
			break // Only one override block can match per identity
		}
	}

	return result, warnings
}

// TaskAgentOutput returns the configured output mode for one agent preset.
// Defaults to "auto"; validation is owned by the task executor.
func (c *Config) TaskAgentOutput(agent string) string {
	if c == nil || c.Task == nil {
		return "auto"
	}
	agentConfig, ok := c.Task.Presets[agent]
	if !ok || agentConfig.Output == "" {
		return "auto"
	}
	return agentConfig.Output
}

// IntegrationsSkills returns the merged [integrations] skills list. The error
// is non-nil iff a blocking integrations finding exists (an unknown skill
// alias in any config layer); per ADR 0054 the caller decides severity.
func (c *Config) IntegrationsSkills() ([]string, error) {
	if err := c.blockingFindingFor("integrations"); err != nil {
		return nil, err
	}
	if c == nil || c.Integrations == nil || len(c.Integrations.Skills) == 0 {
		return append([]string(nil), DefaultIntegrationSkills...), nil
	}
	return append([]string(nil), c.Integrations.Skills...), nil
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

// WorkbenchPickOnCreate reports whether the picker create-path should prompt for
// a Workbench when creating a new session. Defaults to false (no prompt); only an
// explicit [workbench] pick_on_create = true enables it (ADR-0075). The receiver
// may be nil.
func (c *Config) WorkbenchPickOnCreate() bool {
	if c == nil || c.WorkbenchOpts == nil {
		return false
	}
	return c.WorkbenchOpts.PickOnCreate
}

// WorkbenchOrder returns the configured [workbench] order tokens (the fixed
// display sequence for the interactive Workbench lists), or nil when unset. The
// receiver may be nil.
func (c *Config) WorkbenchOrder() []string {
	if c == nil || c.WorkbenchOpts == nil {
		return nil
	}
	return c.WorkbenchOpts.Order
}

// ResolvePreferredWorkbench returns the name of the Workbench that should
// auto-apply when a session is born for checkoutPath, or "" for none, plus any
// non-fatal warnings the caller should surface. Resolution follows the
// user-first precedence law (ADR-0083): hand-authored config beats
// runtime-generated config at any scope, and the user's central config.toml
// beats the repo's in-tree .pop.toml. Highest → lowest, the layers that carry
// this key:
//
//	1  config.toml [repo."<path>"]        user central · repo-specific
//	   config.toml (global keys)          n/a for this key (no global home)
//	3  ./.pop.toml                        repo in-tree, this worktree
//	4  <trunk>/.pop.toml (→ id-root)      repo in-tree, inherited from the Trunk
//	5  config.runtime.toml[<wt-path>]     runtime, this worktree (ctrl+w)
//	6  config.runtime.toml[<trunk-path>]  runtime, inherited from the Trunk
//	   → none
//
// Everything hand-authored (1–4) beats everything runtime (5–6): a repo default
// or committed .pop.toml therefore wins over a worktree runtime entry (the
// reverse of the shipped scope-first ordering), and runtime is now a gap-filler
// applying only where nothing hand-authored sets the key. The in-tree .pop.toml
// is read at two anchors — this worktree (layer 3) and the Trunk worktree, the
// trunk read falling back to the Repository identity root for a bare repo (layer
// 4) — with presence deciding which supplies the value: a worktree with its own
// .pop.toml overrides the inherited trunk one, and a worktree without inherits
// trunk's. Layer 4 reuses ADR-0078's Deps.Trunk resolver and its this-is-trunk
// read-once guard (skipped when the inherited anchor is this very checkout, so a
// stale name never double-warns).
//
// The runtime layers stay three-valued (ADR-0078): an explicit-none entry
// (empty string) short-circuits to flat/prompt here — but only within the
// runtime tier, since it can no longer override a hand-authored value above it;
// a named entry uses that name; an absent entry falls through. The trunk runtime
// layer is dynamic (the child reflects trunk's current choice, never a value
// snapshotted at create) and is skipped when the repo has no trunk anchor or the
// resolver is not wired.
//
// A stored name that does not resolve to a real Workbench for this checkout is
// skipped with a non-fatal warning (ADR-0054 style) at each layer and resolution
// continues down the chain — a broken preference never blocks getting into a
// session and never silently vanishes. The receiver may be nil.
func (c *Config) ResolvePreferredWorkbench(d *Deps, checkoutPath string) (string, []string) {
	if c == nil {
		return "", nil
	}

	var warnings []string
	// resolves reports whether name is a real Workbench for this checkout,
	// resolving the template set lazily (and once) so an unset chain does no work.
	var workbenches []Workbench
	resolved := false
	resolves := func(name string) bool {
		if !resolved {
			workbenches, _ = c.ResolveWorkbenchesWith(d, checkoutPath)
			resolved = true
		}
		for _, tmpl := range workbenches {
			if tmpl.Name == name {
				return true
			}
		}
		return false
	}
	staleWarn := func(name string) string {
		return fmt.Sprintf(
			"preferred workbench %q does not resolve to a Workbench for %s; ignoring",
			name, checkoutPath,
		)
	}
	// consider applies one name-only layer's value (empty means "unset, fall
	// through"): use the name if it resolves, else warn and continue down the
	// chain. Returns (result, done). The runtime layers handle their own
	// explicit-none short-circuit and use consider only for the resolve-or-warn.
	consider := func(name string) (string, bool) {
		if name == "" {
			return "", false
		}
		if resolves(name) {
			return name, true
		}
		warnings = append(warnings, staleWarn(name))
		return "", false
	}

	// Layer 1: config.toml [repo."<path>"] — hand-authored, central, repo-specific.
	if name, done := consider(c.repoPreferredWorkbench(d, checkoutPath)); done {
		return name, warnings
	}

	// Layer 2: config.toml global keys — preferred_workbench has no universal
	// global home, so this position never supplies it.

	// Layer 3: ./.pop.toml — hand-authored, repo in-tree, this worktree.
	if name, done := consider(c.popTOMLPreferredWorkbench(d, checkoutPath)); done {
		return name, warnings
	}

	// Layer 4: <trunk>/.pop.toml — hand-authored, repo in-tree, inherited from the
	// Trunk worktree (falling back to the Repository identity root for a bare
	// repo). Skipped when the inherited anchor is this very checkout (read-once
	// guard: Layer 3 already read it, and re-reading would double-warn a stale name).
	if anchor := c.inheritedRepoConfigAnchor(d, checkoutPath); anchor != "" &&
		canonicalPath(d, anchor) != canonicalPath(d, checkoutPath) {
		if name, done := consider(c.popTOMLPreferredWorkbench(d, anchor)); done {
			return name, warnings
		}
	}

	// Layer 5: config.runtime.toml[<wt-path>] — runtime, this worktree (ctrl+w).
	if name, present, err := RuntimePreferredWorkbenchWith(d, checkoutPath); err != nil {
		debug.Error("config: read runtime preferred workbench for %s: %v", checkoutPath, err)
	} else if present {
		if name == "" {
			// Explicit none: flat/prompt here, short-circuiting the trunk runtime
			// layer below (but not the hand-authored layers above, already passed).
			return "", warnings
		}
		if resolved, done := consider(name); done {
			return resolved, warnings
		}
	}

	// Layer 6: config.runtime.toml[<trunk-path>] — runtime, inherited (ADR-0078).
	// A worktree with no entry of its own inherits trunk's current choice,
	// resolved dynamically at open — re-pointing trunk follows through to
	// un-overridden children. Skipped when there is no trunk anchor (an
	// unconfigured bare repo) or when this checkout *is* the trunk (Layer 5
	// already read its own entry, so re-reading would double-warn on a stale name).
	if d.Trunk != nil {
		if trunkPath, ok := d.Trunk(checkoutPath); ok && trunkPath != "" &&
			canonicalPath(d, trunkPath) != canonicalPath(d, checkoutPath) {
			if name, present, err := RuntimePreferredWorkbenchWith(d, trunkPath); err != nil {
				debug.Error("config: read trunk preferred workbench for %s: %v", trunkPath, err)
			} else if present {
				if name == "" {
					// Trunk opts out: flat/prompt here.
					return "", warnings
				}
				if resolved, done := consider(name); done {
					return resolved, warnings
				}
			}
		}
	}

	// None.
	return "", warnings
}

// popTOMLPreferredWorkbench reads preferred_workbench from the committed .pop.toml
// at dir, or "" when the file is absent, the key is unset, or the file is
// malformed (the read error degrades to none, logged for debugging — a broken
// in-tree file must not block getting into a session).
func (c *Config) popTOMLPreferredWorkbench(d *Deps, dir string) string {
	cfg, err := LoadRepoConfigWith(d, dir)
	if err != nil {
		debug.Error("config: read .pop.toml preferred workbench at %s: %v", dir, err)
		return ""
	}
	return cfg.PreferredWorkbench
}

// inheritedRepoConfigAnchor returns the checkout whose committed .pop.toml
// supplies the inherited (layer 4) repo-scope value for checkoutPath: the Trunk
// worktree when the resolver reports one, otherwise the Repository identity root
// — where a bare repo's shared .pop.toml lives (ADR-0083). A nil resolver or a
// non-bare repo with no trunk yields the identity root, which for a non-bare
// checkout is the checkout itself (Layer 4's read-once guard then skips it).
func (c *Config) inheritedRepoConfigAnchor(d *Deps, checkoutPath string) string {
	if d != nil && d.Trunk != nil {
		if trunkPath, ok := d.Trunk(checkoutPath); ok && trunkPath != "" {
			return trunkPath
		}
	}
	return repoIdentity(d, checkoutPath)
}

// repoPreferredWorkbench returns the preferred_workbench declared on the global
// [repo."<path>"] block whose key shares checkoutPath's repository identity, or
// "" when none. Unlike trunk (which is per-checkout), this default is keyed by
// identity so every worktree of the repo shares it.
func (c *Config) repoPreferredWorkbench(d *Deps, checkoutPath string) string {
	if c == nil {
		return ""
	}
	identity := repoIdentity(d, checkoutPath)
	for rawKey, block := range c.Repo {
		if repoIdentity(d, rawKey) != identity {
			continue
		}
		if block.PreferredWorkbench != "" {
			return block.PreferredWorkbench
		}
	}
	return ""
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

// PaneMonitoringTopicSteps returns the ordered Topic derivation pipeline.
// When topic_agents is unset, a single truncate / set_if="empty" step is
// returned (today's truncation behaviour). An explicit empty array yields no
// steps. See PaneMonitoringConfig.TopicAgents for the step vocabulary.
func (c *Config) PaneMonitoringTopicSteps() TopicSteps {
	if c.PaneMonitoring == nil || c.PaneMonitoring.TopicAgents == nil {
		return DefaultTopicSteps()
	}
	return c.PaneMonitoring.TopicAgents
}

// PaneMonitoringTopicWords returns the word cap applied to a derived Topic slug,
// defaulting to DefaultTopicWords when unset or non-positive.
func (c *Config) PaneMonitoringTopicWords() int {
	if c.PaneMonitoring == nil || c.PaneMonitoring.TopicWords < 1 {
		return DefaultTopicWords
	}
	return c.PaneMonitoring.TopicWords
}

// PaneMonitoringTopicDerivationTimeout returns the per-recipe topic-derivation
// timeout, defaulting to DefaultTopicDerivationTimeoutSeconds when unset or
// non-positive.
func (c *Config) PaneMonitoringTopicDerivationTimeout() time.Duration {
	secs := DefaultTopicDerivationTimeoutSeconds
	if c.PaneMonitoring != nil && c.PaneMonitoring.TopicDerivationTimeout > 0 {
		secs = c.PaneMonitoring.TopicDerivationTimeout
	}
	return time.Duration(secs) * time.Second
}

// DashboardZoomOnSwitch reports whether selecting a pane from the dashboard
// maximizes (zooms) it within its window. Defaults to true; set
// [dashboard] zoom_on_switch = false to focus the pane in place, preserving
// the window's split layout (e.g. nvim above, agent below).
func (c *Config) DashboardZoomOnSwitch() bool {
	if c == nil || c.Dashboard == nil || c.Dashboard.ZoomOnSwitch == nil {
		return true
	}
	return *c.Dashboard.ZoomOnSwitch
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
	md, err := toml.DecodeFile(path, &cfg)
	if err != nil {
		return nil, err
	}
	if err := applyConfigLayerMerge(d, &cfg, path, md); err != nil {
		return nil, err
	}
	// Migrate deprecated [workload] → [tasks] (ADR-0092). This runs after
	// layer merge so it sees the merged workload/tasks state.
	for _, f := range workloadMigrationFindings(&cfg, path) {
		cfg.recordFinding(f)
	}
	for _, f := range effortConfigFindings(path, md) {
		cfg.recordFinding(f)
	}
	for _, f := range projectEntryFindings(path, cfg.Projects) {
		cfg.recordFinding(f)
	}
	if cfg.Workbenches != nil {
		tmplFindings, validTemplates := workbenchFindings(path, cfg.Workbenches)
		for _, f := range tmplFindings {
			cfg.recordFinding(f)
		}
		cfg.Workbenches = validTemplates
	}
	for _, f := range repoRenameFindings(path, md) {
		cfg.recordFinding(f)
	}
	for _, f := range repoBlockWarnings(path, md) {
		cfg.recordFinding(f)
	}
	for _, f := range queueAgentsWarnings(path, md) {
		cfg.recordFinding(f)
	}

	selectSectionUsed := cfg.Select != nil
	if selectSectionUsed {
		cfg.recordFinding(Finding{Path: "deprecated.select", Message: "[select] is deprecated; rename to [project]"})
		if cfg.Project == nil {
			cfg.Project = cfg.Select
		}
	}

	// Deprecation findings for the needs_attention → unread rename.
	if cfg.PaneMonitoring != nil && cfg.PaneMonitoring.DismissAttentionInActivePane {
		cfg.recordFinding(Finding{
			Path:    "deprecated.pane_monitoring.dismiss_attention_in_active_pane",
			Message: "[pane_monitoring] dismiss_attention_in_active_pane is deprecated; rename to dismiss_unread_in_active_pane",
		})
	}
	if pc := cfg.projectConfig(); pc != nil && pc.AttentionNotificationsEnabled {
		section := "[project]"
		if selectSectionUsed && cfg.Select == pc {
			section = "[select]"
		}
		cfg.recordFinding(Finding{
			Path:    "deprecated.attention_notifications_enabled",
			Message: section + " attention_notifications_enabled is deprecated; rename to unread_notifications_enabled",
		})
	}
	if cfg.Select != nil && cfg.Select != cfg.Project && cfg.Select.AttentionNotificationsEnabled {
		cfg.recordFinding(Finding{
			Path:    "deprecated.attention_notifications_enabled",
			Message: "[select] attention_notifications_enabled is deprecated; rename to unread_notifications_enabled",
		})
	}
	if cfg.Worktree != nil && cfg.Worktree.AttentionNotificationsEnabled {
		cfg.recordFinding(Finding{
			Path:    "deprecated.worktree.attention_notifications_enabled",
			Message: "[worktree] attention_notifications_enabled is deprecated; rename to unread_notifications_enabled",
		})
	}

	configDir := filepath.Dir(path)
	for _, include := range cfg.Includes {
		expanded := expandHomeWith(d, include)
		if !filepath.IsAbs(expanded) {
			expanded = filepath.Join(configDir, expanded)
		}

		var included Config
		includedMD, err := toml.DecodeFile(expanded, &included)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				cfg.Warnings = append(cfg.Warnings, fmt.Sprintf("include file %q not found, skipping", include))
				continue
			}
			return nil, fmt.Errorf("loading include %q: %w", include, err)
		}
		for _, f := range effortConfigFindings(expanded, includedMD) {
			cfg.recordFinding(f)
		}
		for _, f := range projectEntryFindings(expanded, included.Projects) {
			cfg.recordFinding(f)
		}
		for _, f := range repoRenameFindings(expanded, includedMD) {
			cfg.recordFinding(f)
		}
		for _, f := range repoBlockWarnings(expanded, includedMD) {
			cfg.recordFinding(f)
		}
		// Migrate deprecated [workload] → [tasks] in included file (ADR-0092)
		for _, f := range workloadMigrationFindings(&included, expanded) {
			cfg.recordFinding(f)
		}
		cfg.Warnings = append(cfg.Warnings, includeFileWarnings(expanded, &included, d)...)

		cfg.Projects = append(cfg.Projects, included.Projects...)
		mergeIncludedTask(&cfg, included.Task, expanded)
		mergeIncludedEffort(&cfg, included.Effort, expanded)

		if included.Workbenches != nil {
			tmplFindings, validTemplates := workbenchFindings(expanded, included.Workbenches)
			for _, f := range tmplFindings {
				cfg.recordFinding(f)
			}
			cfg.Workbenches = append(cfg.Workbenches, validTemplates...)
		}

		for key, block := range included.Repo {
			if _, exists := cfg.Repo[key]; exists {
				cfg.Warnings = append(cfg.Warnings, fmt.Sprintf(
					"%s: [repo.%q] skipped, key already defined (first definition wins)",
					expanded, key,
				))
				continue
			}
			if cfg.Repo == nil {
				cfg.Repo = make(map[string]RepoOverrideConfig)
			}
			cfg.Repo[key] = block
		}
	}

	return &cfg, nil
}

// workbenchFindings validates Workbenches at load time. A template with a
// missing or duplicate window name is recorded as a non-fatal finding and
// excluded from the returned slice; the rest of the config still loads.
func workbenchFindings(path string, templates []Workbench) ([]Finding, []Workbench) {
	if templates == nil {
		return nil, nil
	}
	var findings []Finding
	valid := make([]Workbench, 0, len(templates))

	for i, tmpl := range templates {
		if tmpl.Name == "" {
			findings = append(findings, Finding{
				Path:    fmt.Sprintf("workbenches[%d]", i),
				Message: fmt.Sprintf("%s: workbenches[%d] has no name; excluding", path, i),
			})
			continue
		}

		names := make(map[string]bool)
		invalid := false
		for j, w := range tmpl.Windows {
			if w.Name == "" {
				findings = append(findings, Finding{
					Path: fmt.Sprintf("workbenches[%d].windows[%d].name", i, j),
					Message: fmt.Sprintf(
						"%s: workbench %q window[%d] is missing a name; excluding template",
						path, tmpl.Name, j,
					),
				})
				invalid = true
				break
			}
			if names[w.Name] {
				findings = append(findings, Finding{
					Path: fmt.Sprintf("workbenches[%d].windows[%d].name", i, j),
					Message: fmt.Sprintf(
						"%s: workbench %q has duplicate window name %q; excluding template",
						path, tmpl.Name, w.Name,
					),
				})
				invalid = true
				break
			}
			names[w.Name] = true

			// A duplicate pane name within one window makes that Workbench
			// reapply-unsafe (ADR-0075): merge matches live panes by name, so
			// two leaves sharing a name cannot be told apart. This is a
			// non-fatal finding — the template still loads and applies; it just
			// loses its reapply guarantee for that window.
			for _, dup := range duplicatePaneSpecNames(w.Layout) {
				findings = append(findings, Finding{
					Path: fmt.Sprintf("workbenches[%d].windows[%d]", i, j),
					Message: fmt.Sprintf(
						"%s: workbench %q window %q has duplicate pane name %q; reapply-unsafe",
						path, tmpl.Name, w.Name, dup,
					),
				})
			}
		}

		if !invalid {
			valid = append(valid, tmpl)
		}
	}

	return findings, valid
}

// duplicatePaneSpecNames returns the leaf pane-spec names that appear more than
// once anywhere in a window's layout tree, in first-duplicate order. Unnamed
// leaves are anonymous (ADR-0075 B1) and never collide.
func duplicatePaneSpecNames(layout *WorkbenchPaneSpec) []string {
	if layout == nil {
		return nil
	}
	seen := make(map[string]bool)
	flagged := make(map[string]bool)
	var dups []string
	var walk func(p *WorkbenchPaneSpec)
	walk = func(p *WorkbenchPaneSpec) {
		if len(p.Panes) == 0 {
			if p.Name == "" {
				return
			}
			if seen[p.Name] && !flagged[p.Name] {
				flagged[p.Name] = true
				dups = append(dups, p.Name)
			}
			seen[p.Name] = true
			return
		}
		for i := range p.Panes {
			walk(&p.Panes[i])
		}
	}
	walk(layout)
	return dups
}

// projectEntryFindings collects a finding for every project entry whose
// display_depth had the wrong type. Per ADR 0054 these are non-essential: they
// are keyed under "projects[].display_depth" (deliberately not the "projects"
// section, so the essential ProjectEntries getter stays non-fatal) and only
// surface as a warning banner while the entry still resolves at the default
// depth. The file path is prepended so the banner names the offending file.
func projectEntryFindings(path string, entries []ProjectEntry) []Finding {
	var findings []Finding
	for i := range entries {
		if _, err := entries[i].GetDisplayDepth(); err != nil {
			f, ok := err.(Finding)
			if !ok {
				continue
			}
			f.Message = fmt.Sprintf("%s: %s", path, f.Message)
			findings = append(findings, f)
		}
	}
	return findings
}

// effortConfigFindings inspects decoded metadata for semantic problems in the
// [effort] section — an unknown tier, or an unknown key inside a valid tier —
// and returns them as findings keyed to the offending config path. Per ADR 0054
// these are collected, not thrown: a stale [effort] key must not abort a command
// (e.g. the project dashboard) that never reads effort. A command that consumes
// effort surfaces the finding as the error from Config.EffortFor.
func effortConfigFindings(path string, md toml.MetaData) []Finding {
	validTiers := map[string]bool{"heavy": true, "standard": true, "light": true}
	validEntryKeys := map[string]bool{"model": true, "reasoning": true}
	var findings []Finding
	// An array-of-tables value (e.g. heavy = [{ ... }]) surfaces as several
	// nested undecoded keys sharing a prefix, so dedupe by the finding path to
	// report each offending tier / entry key exactly once.
	seen := make(map[string]bool)
	add := func(f Finding) {
		if seen[f.Path] {
			return
		}
		seen[f.Path] = true
		findings = append(findings, f)
	}
	for _, key := range md.Undecoded() {
		if len(key) >= 3 && key[0] == "effort" && !validTiers[key[2]] {
			add(Finding{
				Path:    fmt.Sprintf("effort.%s.%s", key[1], key[2]),
				Message: fmt.Sprintf("%s: [effort.%s] unknown tier %q; valid tiers: heavy, standard, light", path, key[1], key[2]),
			})
		}
	}
	for _, key := range md.Undecoded() {
		if len(key) >= 4 && key[0] == "effort" && validTiers[key[2]] && !validEntryKeys[key[3]] {
			add(Finding{
				Path:    fmt.Sprintf("effort.%s.%s.%s", key[1], key[2], key[3]),
				Message: fmt.Sprintf("%s: [effort.%s] tier %q entry has unknown key %q; valid entry keys: model, reasoning", path, key[1], key[2], key[3]),
			})
		}
	}
	return findings
}

// repoRenameFindings inspects decoded metadata for the deliberate migration
// tripwires in repo/execution config — keys that were renamed or removed
// (worktree_ready, execution_base→trunk, queue_base→trunk, misplaced trunk).
// Per ADR 0054 these are returned as findings keyed to the "repo" section, not
// thrown: a stale execution key must not abort a command (e.g. the project
// dashboard) that never reads execution config. A command that consumes it
// surfaces the finding as the error from Config.ResolveRepoConfig, so the
// tripwire stays loud but confined to the execution/queue commands.
func repoRenameFindings(path string, md toml.MetaData) []Finding {
	var findings []Finding
	add := func(msg string) {
		findings = append(findings, Finding{Path: "repo", Message: msg})
	}
	for _, key := range md.Undecoded() {
		// .pop.toml-level / top-level (len==1) renames
		if len(key) == 1 {
			switch key[0] {
			case "worktree_ready":
				add(fmt.Sprintf("%s: worktree_ready was removed; use trunk = true in a global [repo.%q] block to name the Trunk worktree", path, "<path>"))
			case "execution_base":
				add(fmt.Sprintf("%s: execution_base was renamed to trunk; use trunk = true in a global [repo.%q] block", path, "<path>"))
			case "queue_base":
				add(fmt.Sprintf("%s: queue_base was renamed to trunk; use trunk = true in a global [repo.%q] block", path, "<path>"))
			}
		}
		// [repo."<path>"] block renames (len>=3)
		if len(key) >= 3 && key[0] == "repo" {
			switch key[2] {
			case "worktree_ready":
				add(fmt.Sprintf("%s: [repo.%q] worktree_ready was removed; there is no replacement", path, key[1]))
			case "execution_base":
				add(fmt.Sprintf("%s: [repo.%q] execution_base was renamed to trunk", path, key[1]))
			case "queue_base":
				add(fmt.Sprintf("%s: [repo.%q] queue_base was renamed to trunk", path, key[1]))
			}
		}
	}
	return findings
}

// validateRepoConfigMetadata keeps the repo-local .pop.toml path hard-failing on
// the same migration tripwires (LoadRepoConfig has no Config to carry findings
// on, and a stale key there still surfaces fatally via ResolveRepoConfig's
// returned error). It returns a plain error — deliberately NOT a Finding — so a
// caller iterating checkouts (the queue's representative resolver) can tell a
// fatal config-global migration finding apart from a per-checkout .pop.toml
// problem it should degrade past.
func validateRepoConfigMetadata(path string, md toml.MetaData) error {
	if findings := repoRenameFindings(path, md); len(findings) > 0 {
		return errors.New(findings[0].Message)
	}
	return nil
}

// workloadMigrationFindings detects the deprecated [workload] table and
// migrates its fields into the canonical [tasks] structure (ADR-0092). When
// both are present, [tasks] wins per-key; [workload] fills gaps and emits a
// deprecation warning naming the replacement. The mapping is structural:
//
//	[workload] default_agents  → [tasks.implement].agents
//	[workload.verify]          → [tasks.verify]
//	[workload.git]             → [tasks.git]
//	[workload.agents.<name>]   → [tasks.presets.<name>]
//
// Returns findings for each aliased key present; the caller records them.
func workloadMigrationFindings(cfg *Config, path string) []Finding {
	if cfg.Workload == nil {
		return nil
	}

	// Emit deprecation warnings for the deprecated [workload] table and each
	// aliased sub-key present. Each warning names the [tasks.*] replacement.
	var findings []Finding
	findings = append(findings, Finding{
		Path:    "deprecated.workload",
		Message: fmt.Sprintf("%s: [workload] is deprecated; rename to [tasks]", path),
	})
	if len(cfg.Workload.DefaultAgents) > 0 {
		findings = append(findings, Finding{
			Path:    "deprecated.workload.default_agents",
			Message: fmt.Sprintf("%s: [workload] default_agents is deprecated; rename to [tasks.implement].agents", path),
		})
	}
	if cfg.Workload.Verify != nil {
		findings = append(findings, Finding{
			Path:    "deprecated.workload.verify",
			Message: fmt.Sprintf("%s: [workload.verify] is deprecated; rename to [tasks.verify]", path),
		})
	}
	if cfg.Workload.Git != nil {
		findings = append(findings, Finding{
			Path:    "deprecated.workload.git",
			Message: fmt.Sprintf("%s: [workload.git] is deprecated; rename to [tasks.git]", path),
		})
	}
	if len(cfg.Workload.Agents) > 0 {
		findings = append(findings, Finding{
			Path:    "deprecated.workload.agents",
			Message: fmt.Sprintf("%s: [workload.agents] is deprecated; rename to [tasks.presets]", path),
		})
	}

	// Migrate [workload] → [tasks], honoring per-key precedence: [tasks] wins
	// when both set the same field; [workload] fills gaps.
	if cfg.Task == nil {
		cfg.Task = &TasksConfig{}
	}

	// [workload] default_agents → [tasks.implement].agents
	if len(cfg.Workload.DefaultAgents) > 0 {
		if cfg.Task.Implement == nil {
			cfg.Task.Implement = &ImplementConfig{
				Agents: append([]string(nil), cfg.Workload.DefaultAgents...),
			}
		} else if len(cfg.Task.Implement.Agents) == 0 {
			cfg.Task.Implement.Agents = append([]string(nil), cfg.Workload.DefaultAgents...)
		}
		// else: [tasks.implement].agents already set, [tasks] wins
	}

	// [workload.verify] → [tasks.verify]
	if cfg.Workload.Verify != nil {
		if cfg.Task.Verify == nil {
			cfg.Task.Verify = cloneWorkloadVerifyAsVerify(cfg.Workload.Verify)
		} else {
			// Merge per-field: [tasks.verify] wins when set
			wv := cfg.Workload.Verify
			tv := cfg.Task.Verify
			if !tv.Enabled && wv.Enabled {
				tv.Enabled = true
			}
			if len(tv.Agents) == 0 && len(wv.Agents) > 0 {
				tv.Agents = append([]string(nil), wv.Agents...)
			}
			if tv.Effort == "" && wv.Effort != "" {
				tv.Effort = wv.Effort
			}
			if tv.MaxRemediationDepth == nil && wv.MaxRetries > 0 {
				v := wv.MaxRetries
				tv.MaxRemediationDepth = &v
			}
		}
	}

	// [workload.git] → [tasks.git]
	if cfg.Workload.Git != nil {
		if cfg.Task.Git == nil {
			cfg.Task.Git = cloneTaskGitConfig(cfg.Workload.Git)
		} else if len(cfg.Task.Git.CommitConfigOverrides) == 0 &&
			len(cfg.Workload.Git.CommitConfigOverrides) > 0 {
			cfg.Task.Git.CommitConfigOverrides = append([]string(nil), cfg.Workload.Git.CommitConfigOverrides...)
		}
	}

	// [workload.agents.<name>] → [tasks.presets.<name>]
	if len(cfg.Workload.Agents) > 0 {
		if cfg.Task.Presets == nil {
			cfg.Task.Presets = make(map[string]TaskAgentConfig, len(cfg.Workload.Agents))
		}
		for name, wac := range cfg.Workload.Agents {
			if _, exists := cfg.Task.Presets[name]; !exists {
				cfg.Task.Presets[name] = TaskAgentConfig{Output: wac.Output}
			}
		}
	}

	return findings
}

// cloneWorkloadVerifyAsVerify converts a deprecated WorkloadVerifyConfig into
// the canonical VerifyConfig, mapping MaxRetries → MaxRemediationDepth.
func cloneWorkloadVerifyAsVerify(src *WorkloadVerifyConfig) *VerifyConfig {
	if src == nil {
		return nil
	}
	dst := &VerifyConfig{
		Enabled: src.Enabled,
		Agents:  append([]string(nil), src.Agents...),
		Effort:  src.Effort,
	}
	if src.MaxRetries > 0 {
		v := src.MaxRetries
		dst.MaxRemediationDepth = &v
	}
	return dst
}

// cloneWorkloadConfig deep-copies a WorkloadConfig for layer merge.
func cloneWorkloadConfig(src *WorkloadConfig) *WorkloadConfig {
	if src == nil {
		return nil
	}
	dst := &WorkloadConfig{
		DefaultAgents: append([]string(nil), src.DefaultAgents...),
	}
	if src.Verify != nil {
		dst.Verify = &WorkloadVerifyConfig{
			Enabled:    src.Verify.Enabled,
			Agents:     append([]string(nil), src.Verify.Agents...),
			Effort:     src.Verify.Effort,
			MaxRetries: src.Verify.MaxRetries,
		}
	}
	if src.Git != nil {
		dst.Git = cloneTaskGitConfig(src.Git)
	}
	if len(src.Agents) > 0 {
		dst.Agents = make(map[string]WorkloadAgentConfig, len(src.Agents))
		for name, wac := range src.Agents {
			dst.Agents[name] = wac
		}
	}
	return dst
}

// queueAgentsWarnings returns a load-time finding when a config file still
// sets the deleted [queue].agents key. Agent selection is owned by
// [tasks.implement].agents; the old key is ignored (fail-soft).
func queueAgentsWarnings(path string, md toml.MetaData) []Finding {
	for _, key := range md.Undecoded() {
		if len(key) == 2 && key[0] == "queue" && key[1] == "agents" {
			return []Finding{{
				Path: "deprecated.queue.agents",
				Message: fmt.Sprintf(
					"%s: [queue] agents is ignored; configure agent fallback under [tasks.implement].agents",
					path,
				),
			}}
		}
	}
	return nil
}

// repoScopeLegalKeys returns the set of TOML keys that are legal at repo scope,
// derived by reflection from the shared RepoScopeConfig schema (ADR-0083). It is
// the single source of truth for both repo-scope loci — the committed .pop.toml
// and the global [repo."<path>"] override — so adding a repo-scope key to that
// struct makes both surfaces accept it with no change to validation code. trunk
// is deliberately absent (it is [repo]-only, not shared).
func repoScopeLegalKeys() map[string]bool {
	legal := make(map[string]bool)
	t := reflect.TypeOf(RepoScopeConfig{})
	for i := 0; i < t.NumField(); i++ {
		name := strings.Split(t.Field(i).Tag.Get("toml"), ",")[0]
		if name != "" && name != "-" {
			legal[name] = true
		}
	}
	return legal
}

// popTOMLScopeFindings enforces scope-legality for a committed .pop.toml
// (ADR-0083, ADR-0054): only shared repo-scope keys (repoScopeLegalKeys) are
// honored there. Any global/machine-only top-level key (projects, queue,
// pane_monitoring, dashboard/daemon knobs, …) and the [repo]-only trunk key are
// ignored but surfaced as non-fatal findings, so the rest of the file still
// loads. The legal set is generated from the shared schema, not a second
// hand-maintained whitelist. Renamed-key migration tripwires (worktree_ready,
// execution_base, queue_base) stay fatal and are handled by
// validateRepoConfigMetadata before this runs.
func popTOMLScopeFindings(path string, md toml.MetaData) []Finding {
	legal := repoScopeLegalKeys()
	var findings []Finding
	seen := make(map[string]bool)
	for _, key := range md.Undecoded() {
		if len(key) == 0 {
			continue
		}
		name := key[0] // top-level key; nested global tables share one key[0]
		if legal[name] || seen[name] {
			continue
		}
		seen[name] = true
		msg := fmt.Sprintf("%s: %q is not valid in .pop.toml and was ignored (only repo-scope keys are accepted)", path, name)
		if name == "trunk" {
			msg = fmt.Sprintf("%s: trunk is only valid in a global [repo.%q] override block and was ignored", path, "<path>")
		}
		findings = append(findings, Finding{Path: "repo_scope.unknown_key", Message: msg})
	}
	return findings
}

// repoBlockWarnings returns load-time findings for unknown keys inside
// [repo."<path>"] blocks. Only the shared repo-scope key set (plus the
// [repo]-only trunk) is valid there; any other key is silently degraded but
// surfaced as a finding. The valid set is derived from the shared schema
// (repoScopeLegalKeys), so it stays in sync with .pop.toml scope-legality.
func repoBlockWarnings(path string, md toml.MetaData) []Finding {
	validRepoKeys := repoScopeLegalKeys()
	validRepoKeys["trunk"] = true // [repo]-only machine topology, not shared
	var findings []Finding
	seen := make(map[string]bool)
	for _, key := range md.Undecoded() {
		if len(key) < 3 || key[0] != "repo" {
			continue
		}
		// key[1] = block path, key[2] = unknown field name
		fieldName := key[2]
		if validRepoKeys[fieldName] {
			continue
		}
		uniq := key[1] + "\x00" + fieldName
		if seen[uniq] {
			continue
		}
		seen[uniq] = true
		findings = append(findings, Finding{
			Path: "config.unknown_repo_key",
			Message: fmt.Sprintf(
				"%s: [repo.%q] unknown key %q ignored (only trunk, workbenches, and preferred_workbench are accepted)",
				path, key[1], fieldName,
			),
		})
	}
	return findings
}

// includeFileWarnings returns load-time warnings for non-whitelisted top-level
// keys and nested includes in an included file. Includes carry a fixed whitelist:
// `projects`, `workbenches`, `[tasks]`, `[effort.<agent>]`, and `[repo."<path>"]`.
func includeFileWarnings(path string, cfg *Config, d *Deps) []string {
	var warnings []string

	// Check for nested includes (not allowed)
	if len(cfg.Includes) > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"%s: includes field ignored (nested includes not supported, one level only)",
			path,
		))
	}

	// Detect all top-level keys actually present in the include file by parsing
	// into a generic map. This catches both struct fields and undecoded keys.
	data, err := d.FS.ReadFile(path)
	if err != nil {
		return warnings
	}

	var rawInclude map[string]interface{}
	if _, err := toml.Decode(string(data), &rawInclude); err != nil {
		return warnings
	}

	// Whitelisted top-level keys
	whitelisted := map[string]bool{
		"projects":    true,
		"workbenches": true,
		"repo":        true,
		"tasks":       true,
		"workload":    true, // deprecated alias for tasks (ADR-0092)
		"effort":      true,
		"includes":    true, // mentioned in includes, so we track it for warning above
	}

	// Check for non-whitelisted keys
	seen := make(map[string]bool)
	for key := range rawInclude {
		if !whitelisted[key] && !seen[key] {
			seen[key] = true
			warnings = append(warnings, fmt.Sprintf(
				"%s: %q ignored (includes only support projects, workbenches, repo, tasks, and effort blocks)",
				path, key,
			))
		}
	}

	// Emit deprecation warning if deprecated [workload] key is present
	if _, hasWorkload := rawInclude["workload"]; hasWorkload {
		warnings = append(warnings, fmt.Sprintf(
			"%s: [workload] is deprecated; rename to [tasks]",
			path,
		))
	}

	return warnings
}

func mergeIncludedTask(cfg *Config, included *TasksConfig, path string) {
	if included == nil {
		return
	}
	if cfg.Task == nil {
		cfg.Task = cloneTaskConfig(included)
		return
	}
	// Merge Implement sub-table (agents list)
	if included.Implement != nil && len(included.Implement.Agents) > 0 {
		if cfg.Task.Implement == nil {
			cfg.Task.Implement = &ImplementConfig{}
		}
		if len(cfg.Task.Implement.Agents) > 0 {
			cfg.Warnings = append(cfg.Warnings, fmt.Sprintf(
				"%s: [tasks.implement].agents skipped, already defined (first definition wins)",
				path,
			))
		} else {
			cfg.Task.Implement.Agents = append([]string(nil), included.Implement.Agents...)
		}
	}
	// Merge Presets map
	for preset, block := range included.Presets {
		if cfg.Task.Presets == nil {
			cfg.Task.Presets = make(map[string]TaskAgentConfig)
		}
		if _, exists := cfg.Task.Presets[preset]; exists {
			cfg.Warnings = append(cfg.Warnings, fmt.Sprintf(
				"%s: [tasks.presets.%s] skipped, already defined (first definition wins)",
				path, preset,
			))
			continue
		}
		cfg.Task.Presets[preset] = block
	}
	if included.Git != nil {
		if cfg.Task.Git != nil {
			cfg.Warnings = append(cfg.Warnings, fmt.Sprintf(
				"%s: [tasks.git] skipped, already defined (first definition wins)",
				path,
			))
		} else {
			cfg.Task.Git = cloneTaskGitConfig(included.Git)
		}
	}
	if included.Verify != nil {
		if cfg.Task.Verify != nil {
			cfg.Warnings = append(cfg.Warnings, fmt.Sprintf(
				"%s: [tasks.verify] skipped, already defined (first definition wins)",
				path,
			))
		} else {
			v := *included.Verify
			cfg.Task.Verify = &v
		}
	}
}

func mergeIncludedEffort(cfg *Config, included map[string]EffortConfig, path string) {
	if len(included) == 0 {
		return
	}
	if cfg.Effort == nil {
		cfg.Effort = make(map[string]EffortConfig, len(included))
	}
	for agent, ladder := range included {
		if _, exists := cfg.Effort[agent]; exists {
			cfg.Warnings = append(cfg.Warnings, fmt.Sprintf(
				"%s: [effort.%s] skipped, already defined (first definition wins)",
				path, agent,
			))
			continue
		}
		cfg.Effort[agent] = cloneEffortConfig(ladder)
	}
}

func cloneTaskConfig(src *TasksConfig) *TasksConfig {
	if src == nil {
		return nil
	}
	dst := &TasksConfig{}
	if src.Implement != nil {
		dst.Implement = &ImplementConfig{
			Agents: append([]string(nil), src.Implement.Agents...),
		}
	}
	if len(src.Presets) > 0 {
		dst.Presets = make(map[string]TaskAgentConfig, len(src.Presets))
		for preset, block := range src.Presets {
			dst.Presets[preset] = block
		}
	}
	if src.Git != nil {
		dst.Git = cloneTaskGitConfig(src.Git)
	}
	if src.Verify != nil {
		v := *src.Verify
		dst.Verify = &v
	}
	return dst
}

func cloneTaskGitConfig(src *TaskGitConfig) *TaskGitConfig {
	if src == nil {
		return nil
	}
	return &TaskGitConfig{
		CommitConfigOverrides: append([]string(nil), src.CommitConfigOverrides...),
	}
}

func cloneEffortConfig(src EffortConfig) EffortConfig {
	return EffortConfig{
		Heavy:    cloneEffortModels(src.Heavy),
		Standard: cloneEffortModels(src.Standard),
		Light:    cloneEffortModels(src.Light),
	}
}

func cloneEffortModels(src []EffortModel) []EffortModel {
	if len(src) == 0 {
		return nil
	}
	return append([]EffortModel(nil), src...)
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
		// display_depth is non-essential (ADR 0054): a wrong-typed value falls
		// back to the default here while the entry still resolves. The finding
		// was already recorded at load time, so it surfaces in the banner.
		displayDepth, _ := entry.GetDisplayDepth()

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
				// A malformed glob degrades to a warning rather than aborting:
				// other entries still resolve, and the picker renders what it
				// can while naming the bad pattern in the banner (ADR 0054).
				c.recordFinding(Finding{
					Path:    "projects[].path",
					Message: fmt.Sprintf("project path %q is not a valid glob pattern (%v); skipping", entry.Path, err),
				})
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
