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

// DashboardConfig holds the deprecated [dashboard] table (ADR-0206): the
// monitor dashboard's cursor, sort and zoom settings now live in
// [monitor.dashboard] (MonitorDashboardConfig) and this table is read only as
// its alias, through monitorDashboardConfig().
type DashboardConfig struct {
	CurrentPaneAlwaysUnderCursor bool     `toml:"current_pane_always_under_cursor" desc:"Deprecated: place the current pane under the cursor (use cursor_position)."`
	CursorPosition               string   `toml:"cursor_position" desc:"Deprecated: use [monitor.dashboard]."`
	SortCriteria                 []string `toml:"sort_criteria" desc:"Deprecated: use [monitor.dashboard]."`
	ZoomOnSwitch                 *bool    `toml:"zoom_on_switch" desc:"Deprecated: use [monitor.dashboard]."`
}

// MonitorConfig holds the [monitor] table: the monitor/pane dashboard's own
// settings, one nested table per concern (house style, e.g. [work.dashboard]).
type MonitorConfig struct {
	// Dashboard holds the monitor dashboard's cursor, sort and zoom settings
	// (ADR-0206). Supersedes the root [dashboard] table kept above as its
	// deprecated alias.
	Dashboard *MonitorDashboardConfig `toml:"dashboard" desc:"Monitor dashboard cursor, sort and zoom behavior ([monitor.dashboard] table)."`
}

// MonitorDashboardConfig holds the monitor dashboard's cursor-position,
// sort-criteria and zoom-on-switch settings (ADR-0206). Read through
// Config.monitorDashboardConfig(), which aliases the deprecated [dashboard]
// table: present here, this table wins key-for-key over [dashboard].
type MonitorDashboardConfig struct {
	CursorPosition string   `toml:"cursor_position" desc:"Initial cursor strategy (current_registered|current_any|first_active)."`
	SortCriteria   []string `toml:"sort_criteria" desc:"Dashboard sort order (array of status|pane_last_active_at|session_last_visit_at|alphabetical)."`
	ZoomOnSwitch   *bool    `toml:"zoom_on_switch" desc:"Zoom the target pane when switching to it."`
	// KillPanePromptEnabled gates the y/N confirmation C-x asks before it
	// destroys a pane. It has no alias in the deprecated [dashboard] table: the
	// setting is newer than the move (ADR-0205/ADR-0206).
	KillPanePromptEnabled *bool `toml:"kill_pane_prompt_enabled" desc:"Ask y/N before C-x kills the cursored pane."`
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
	// WorktreeDisplay selects how the project dashboard arranges a repository's
	// worktree rows. Absent or "flat" is today's list; "nested" hangs a
	// project's live worktree sessions under it as a second level. Read through
	// Config.ProjectWorktreeDisplay, never off this field.
	WorktreeDisplay string `toml:"worktree_display" desc:"How the project dashboard arranges worktree rows (flat|nested)."`
	// SessionOrdering selects how the dashboard orders its rows. Absent or
	// "unified" is today's one recency timeline; "live-first" tiers the rows
	// with a live tmux session next to the prompt. Read through
	// Config.ProjectSessionOrdering, never off this field.
	SessionOrdering string `toml:"session_ordering" desc:"How the project dashboard orders its rows (unified|live-first)."`
}

// WorktreeDisplay is how the project dashboard arranges a repository's worktree
// rows: flat (every worktree as its own top-level row, under its full
// "<project>/<worktree>" name) or nested (a project's live worktree sessions
// hung under it). Nested is a preference and flat is the permanent default —
// this is not a migration.
type WorktreeDisplay string

const (
	// WorktreeDisplayFlat is the default: one top-level row per worktree.
	WorktreeDisplayFlat WorktreeDisplay = "flat"
	// WorktreeDisplayNested hangs a project's live worktree sessions under it.
	WorktreeDisplayNested WorktreeDisplay = "nested"
)

// SessionOrdering is how the project dashboard orders its rows: unified (one
// recency timeline, rows with a live session interleaved with session-less
// checkouts) or live-first (the rows with a live session tier next to the
// prompt, recency untouched within each tier). Live-first is a preference and
// unified is the permanent default — this is not a migration.
type SessionOrdering string

const (
	// SessionOrderingUnified is the default: one unified recency timeline.
	SessionOrderingUnified SessionOrdering = "unified"
	// SessionOrderingLiveFirst tiers rows with a live session next to the prompt.
	SessionOrderingLiveFirst SessionOrdering = "live-first"
)

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

// TmuxConfig holds global tmux-server addressing settings (ADR-0199).
// Global-scope only: excluded from the include whitelist (ADR-0037) and from
// repo-scope surfaces (ADR-0083). An empty Socket is meaningful — it means
// emit no -L flag, not "default".
type TmuxConfig struct {
	// Socket is the tmux server socket name every pop command addresses
	// (-L <name>). Empty/unset means no -L flag (tmux's own default server).
	Socket string `toml:"socket" desc:"Tmux server socket name (-L); empty addresses tmux's default server with no -L flag."`
	// Include is the path to a user-authored tmux config file that the Base
	// tmux config sources last (ADR-0199 decision 6). Pop never creates or
	// writes this file. Empty/unset means the documented default path.
	Include string `toml:"include" desc:"Path to a user-authored tmux config the Base tmux config sources last (default \"~/.config/pop/tmux.conf\"). Pop never writes this file."`
}

// DefaultTmuxIncludePath is the documented default for tmux.include — under
// pop's own config tree, not tmux's search paths, so a user running pop's
// Base tmux config can still keep personal bindings without suppressing -f.
const DefaultTmuxIncludePath = "~/.config/pop/tmux.conf"

// DefaultTaskMaxTries is the default started-attempt cap for implement and verify
// when neither config nor an explicit CLI flag names a value (ADR-0099).
const DefaultTaskMaxTries = 3

// DefaultTaskAttemptRetryDelays is the default inter-attempt wait schedule when
// a retrying Work group omits attempt_retry_delays (ADR-0099).
var DefaultTaskAttemptRetryDelays = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute}

// TasksConfig holds what is left of the retired [tasks] table (ADR-0194). Every
// kind-scoped key moved to its Work group under [work]; only [tasks.git]
// survives, as the single read-compat exception recorded in CLEANUP.md —
// [work.implement].git wins when both are set. Any other [tasks.*] table is
// reported at load by retiredTasksSectionFindings rather than read.
type TasksConfig struct {
	// Git holds commit-time git overrides for Pop's own commits, at its pre-cut
	// address.
	Git *TaskGitConfig `toml:"git" include:"replace" desc:"Deprecated: use [work.implement].git."`
}

// ImplementConfig holds the implement Work group ([work.implement], ADR-0194):
// the agents an unattended coding drain walks and the retry loop that governs
// them.
type ImplementConfig struct {
	// Agents is the ordered in-process fallback list used by
	// `pop tasks implement` for unpinned tasks when --agent is absent.
	Agents AgentEntries `toml:"agents" include:"replace" desc:"Ordered fallback agent list for unpinned tasks (strings or {display_name, cmd} tables)."`
	// MaxTries is the started-attempt cap for implement. Zero/unset ⇒
	// DefaultTaskMaxTries. An explicit --max-tries flag still wins.
	MaxTries *int `toml:"max_tries" include:"replace" desc:"Implement started-attempt cap (default 3)."`
	// AttemptRetryDelays is the ordered inter-attempt wait schedule for
	// implement retries. Omitted ⇒ DefaultTaskAttemptRetryDelays; an explicit
	// empty array ⇒ zero delay (instant retries).
	AttemptRetryDelays []string `toml:"attempt_retry_delays" include:"replace" desc:"Implement inter-attempt retry delay schedule (array of duration strings)."`
	// Git holds commit-time git overrides for the commits the drain makes. It
	// sits here, not at the [work] root, because the drain's commit path is the
	// only reader.
	Git *TaskGitConfig `toml:"git" include:"replace" desc:"Commit-time git overrides for Pop's commits ([work.implement.git] table)."`
	// IncludeImplementationConvention inlines the resolved `implementation`
	// convention into every implement prompt as a labelled block, so a builder
	// adheres upfront to the rules the Refiner later enforces (ADR-0246). It is
	// deliberately independent of [work.refine].enabled: adherence can be driven
	// before the pass is ever switched on. Absent/false ⇒ the implement prompt
	// is unchanged.
	IncludeImplementationConvention bool `toml:"include_implementation_convention" include:"replace" desc:"Inline the resolved implementation convention into every implement prompt (default false)."`
}

// AgentGroupConfig is a Work group whose only setting is its ordered agent
// list — a kind with no retry loop of its own ([work.routine],
// [work.attended]).
type AgentGroupConfig struct {
	// Agents is the ordered fallback list for this kind of work. When empty,
	// resolution falls through to the kind's documented fallback.
	Agents AgentEntries `toml:"agents" include:"replace" desc:"Ordered fallback agent list for this kind of work (strings or {display_name, cmd} tables)."`
}

// VerifyConfig holds Agent-verification settings (ADR-0086). It is the
// master, off-by-default gate for the feature: only when Enabled does status
// derivation gate Done on a SHA-keyed Verify verdict. Agents and Effort steer
// which agent renders that verdict and at what model strength.
type VerifyConfig struct {
	// Enabled is the master opt-in switch. Absent/false ⇒ the Verifier never
	// runs and status derives from the manifest alone, exactly as before this
	// feature (ADR-0086/0087).
	Enabled bool `toml:"enabled" include:"replace" desc:"Enable Agent verification as a Done gate (default false)."`
	// Agents is the ordered fallback list of agent presets the Verifier walks,
	// mirroring [work.implement].agents: it falls through to the next agent on
	// a quota pause or a missing binary. An absent list falls back to
	// [work.implement].agents (and, failing that, the built-in default agent);
	// an override of `agents = []` disables that fallback (agent_list.go).
	Agents AgentEntries `toml:"agents" include:"replace" desc:"Ordered fallback agent list for the Verifier, falling back to [work.implement].agents when omitted (strings or {display_name, cmd} tables)."`
	// Effort selects the Verifier's model-strength tier (light, standard, or
	// heavy). Absent ⇒ heavy — verification runs at the strongest tier by default.
	Effort string `toml:"effort" include:"replace" desc:"Verifier model-strength tier: light, standard, or heavy (default heavy)."`
	// MaxRemediationDepth bounds the verify→remediate→re-verify loop (ADR-0086):
	// a FIXABLE verdict spawns a Remediation task only while the set is under this
	// many cycles, after which it parks at VERIFY-FAILED. A nil pointer ⇒ the
	// built-in default; a value ≤ 0 disables remediation (a FIXABLE verdict parks
	// immediately).
	MaxRemediationDepth *int `toml:"max_remediation_depth" include:"replace" desc:"Max verify→remediate cycles before parking at VERIFY-FAILED (default 3)."`
	// MaxTries is the started-attempt cap for verify. Zero/unset ⇒
	// DefaultTaskMaxTries.
	MaxTries *int `toml:"max_tries" include:"replace" desc:"Verify started-attempt cap (default 3)."`
	// AttemptRetryDelays is the ordered inter-attempt wait schedule for verify
	// retries. Omitted ⇒ DefaultTaskAttemptRetryDelays; an explicit empty array
	// ⇒ zero delay (instant retries).
	AttemptRetryDelays []string `toml:"attempt_retry_delays" include:"replace" desc:"Verify inter-attempt retry delay schedule (array of duration strings)."`
}

// RefineConfig holds Refine settings (ADR-0252). It carries exactly three
// keys and no remediation depth, because a refine pass reaches no verdict and
// spawns no work: the whole output is a report a human may act on or ignore.
type RefineConfig struct {
	// Enabled is the master opt-in switch for automatic Refine. Absent/false ⇒
	// the drain never refines; `pop tasks refine <set>` still runs on demand,
	// the way a human asking for a second opinion always may.
	Enabled bool `toml:"enabled" include:"replace" desc:"Enable automatic Refine at AFK quiescence (default false)."`
	// Agents is the ordered fallback list of agent presets the Refiner walks,
	// mirroring [work.verify].agents: it falls through to the next agent on a
	// quota pause or a missing binary. An absent list falls back to
	// [work.implement].agents (and, failing that, the built-in default agent);
	// an override of `agents = []` disables that fallback (agent_list.go).
	Agents AgentEntries `toml:"agents" include:"replace" desc:"Ordered fallback agent list for the Refiner, falling back to [work.implement].agents when omitted (strings or {display_name, cmd} tables)."`
	// Effort selects the Refiner's model-strength tier (light, standard, or
	// heavy). Absent ⇒ heavy — judging naming, structure and idiom is the
	// strongest tier's work.
	Effort string `toml:"effort" include:"replace" desc:"Refiner model-strength tier: light, standard, or heavy (default heavy)."`
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
	PickOnCreate bool `toml:"pick_on_create" include:"replace" desc:"Prompt to pick a Workbench when creating a session with no live one."`

	// Order fixes the display sequence of the interactive Workbench lists (the
	// create prompt and the Preferred-workbench picker). Tokens are the literal
	// on-screen labels: Workbench names plus the special options "<empty>" and
	// "<reset>". Named tokens front-load in the listed order; everything unnamed
	// follows in default order ("<empty>", Workbenches in resolution order,
	// "<reset>"). A token that resolves to nothing is ignored. Settable from an
	// included file (first definition wins; the main config wins over includes).
	Order []string `toml:"order" include:"replace" desc:"Fixed display order of Workbench-list tokens (array of on-screen labels)."`
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

// SpendConfig holds Spend-lens settings (ADR-0218).
type SpendConfig struct {
	// ModelRates are per-model notional rates a human declares for models the
	// published Rate table cannot cover ([spend.model_rates."<model>"] tables).
	ModelRates map[string]SpendModelRate `toml:"model_rates" merge:"map" include:"map-first-wins" desc:"Per-model declared notional rates for the Spend lens ([spend.model_rates.\"<model>\"] tables)."`
}

// SpendModelRate is the four per-token rates a declared override may state.
// They mirror a Rate-table entry so the two are interchangeable at lookup
// (ADR-0218).
type SpendModelRate struct {
	Prompt     string `toml:"prompt" desc:"Per-token prompt/input rate (USD) for this model."`
	Completion string `toml:"completion" desc:"Per-token completion/output rate (USD) for this model."`
	CacheRead  string `toml:"cache_read,omitempty" desc:"Per-token cache-read rate (USD); optional."`
	CacheWrite string `toml:"cache_write,omitempty" desc:"Per-token cache-write rate (USD); optional."`
}

// AgentConfig holds the settings keyed by agent preset rather than by kind of
// work. Only the output mode is left: it decides how pop parses this agent's
// stream in any kind of run (ADR-0194). The attended argument and model keys the
// block used to carry were cut by ADR-0195 — a per-preset key can hold only one
// attended configuration per agent, so the whole invocation moved onto the
// [work.attended].agents entry.
type AgentConfig struct {
	// Output selects how pop reads this preset's stream. Empty ⇒ "auto"; "text"
	// suppresses the adapter's stream-JSON flags entirely, and with them usage,
	// cost, turn counts and Agent proceed verdict detection — the in-config
	// workaround when a vendor changes a stream shape and pop's parser breaks.
	Output string `toml:"output" include:"replace" desc:"Output mode for this agent preset (auto|text|json)."`
}

// AgentSettingsFor returns the [agents.<preset>] block for a preset name, or the
// zero block when the user declared none.
func (c *Config) AgentSettingsFor(preset string) AgentConfig {
	if c == nil || c.Agents == nil {
		return AgentConfig{}
	}
	return c.Agents[strings.TrimSpace(preset)]
}

// ResolveCommitConfigOverrides validates the [work.implement.git]
// commit_config_overrides entries and returns them as `key=value` strings ready
// to be prepended as `-c key=value` pairs to Pop's commit invocations. Each
// entry must split into a non-empty key on the first `=` (an empty value is
// legal git, e.g. "user.signingkey=").
//
// The pre-cut [tasks.git] address is still read — the single read-compat
// exception to ADR-0194's hard cut, kept because the key was added on request
// and its user should not lose it silently. The new address wins when both are
// set.
//
// Validation is deliberately lazy — this is called only from the task drain
// path, never at global config load — so a typo never breaks the picker or
// dashboard. A malformed entry is a hard error: callers must fail the drain
// rather than silently proceed (proceeding could re-trigger the very signing
// hang this feature exists to prevent). The receiver may be nil, in which case
// no overrides apply.
func (c *Config) ResolveCommitConfigOverrides() ([]string, error) {
	section, raw := "work.implement.git", []string(nil)
	if c != nil && c.Work != nil && c.Work.Implement != nil && c.Work.Implement.Git != nil {
		raw = c.Work.Implement.Git.CommitConfigOverrides
	}
	if len(raw) == 0 && c != nil && c.Task != nil && c.Task.Git != nil {
		section, raw = "tasks.git", c.Task.Git.CommitConfigOverrides
	}
	if len(raw) == 0 {
		return nil, nil
	}
	overrides := make([]string, 0, len(raw))
	for i, entry := range raw {
		key, _, found := strings.Cut(entry, "=")
		if !found || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("[%s] commit_config_overrides[%d]: %q must be in key=value form with a non-empty key", section, i, entry)
		}
		overrides = append(overrides, entry)
	}
	return overrides, nil
}

// WorkConfig holds the [work] table: one sub-table per kind of work, plus the
// supervisor's timing under [work.daemon]. Every agent list and every
// task-execution setting is kind-scoped here (ADR-0194) — the root itself holds
// no shared defaults, so each key belongs to exactly the thing that reads it.
//
// Every group merges field-by-field across config layers: the override layer
// carries one key at a time (ADR-0202 decision 2), so a whole-table replace
// would silently drop the hand-authored keys sitting beside the overridden one.
// Includes descend the same way, first-wins per key (ADR-0037): an include may
// set one key of a group the parent also configured without losing the parent's
// other keys, and without its own siblings being erased by the parent's table.
type WorkConfig struct {
	// Implement is the unattended coding drain's group.
	Implement *ImplementConfig `toml:"implement" merge:"fields" include:"fields" desc:"Implement Work group ([work.implement] table)."`
	// Verify is the Verifier's group (ADR-0086).
	Verify *VerifyConfig `toml:"verify" merge:"fields" include:"fields" desc:"Agent-verification settings ([work.verify] table)."`
	// Refine is the Refiner's group (ADR-0252).
	Refine *RefineConfig `toml:"refine" merge:"fields" include:"fields" desc:"Refine settings ([work.refine] table)."`
	// Routine is the recurring-Routine group. An absent list falls through to
	// [work.implement].agents — an override of `agents = []` does not
	// (agent_list.go); a Routine manifest's own agents still beats both.
	Routine *AgentGroupConfig `toml:"routine" merge:"fields" include:"fields" desc:"Routine Work group ([work.routine] table)."`
	// Attended is the group every human-facing session shares — gate assistance,
	// an Assist session, Map assist, map grilling, a Routine refinement session.
	Attended *AgentGroupConfig `toml:"attended" merge:"fields" include:"fields" desc:"Attended-session Work group ([work.attended] table)."`
	// Dashboard holds Work-read-surface settings (view presets). Distinct from
	// the root [dashboard] table, which configures the monitor/pane dashboard.
	Dashboard *WorkDashboardConfig `toml:"dashboard" include:"fields" desc:"Work read-surface settings ([work.dashboard] table)."`
	// Daemon is the supervisor's timing. It is includable like every sibling
	// group: a machine whose quota or load wants a different cadence is exactly
	// the machine-local case an include file exists to carry.
	Daemon *WorkDaemonConfig `toml:"daemon" merge:"fields" include:"fields" desc:"Work supervisor timing ([work.daemon] table)."`
}

// WorkDaemonConfig holds `pop work daemon` supervisor configuration. Durations
// are stored as standard duration strings (e.g. "60s", "1h") and parsed by
// ResolveWorkDaemon.
type WorkDaemonConfig struct {
	// PollInterval is the supervisor's scan cadence. Empty ⇒ DefaultWorkDaemonPollInterval.
	PollInterval string `toml:"poll_interval" include:"replace" desc:"Supervisor scan cadence as a duration string (e.g. \"60s\")."`
	// AgentQuotaRetryAfter is how long a preset cools down after a quota refusal
	// that named no window class — the *unclassed* ceiling only. A refusal that
	// names its window is dated from that window's own span instead, and one
	// that carries the provider's reset instant is dated from the instant
	// (ADR-0235). Empty ⇒ DefaultWorkDaemonQuotaRetryAfter.
	AgentQuotaRetryAfter string `toml:"agent_quota_retry_after" include:"replace" desc:"Ceiling for a quota refusal that named no window, as a duration string."`
	// CrashRetryDelays is the ordered backoff schedule for crash retries; its
	// length is the park threshold. Empty ⇒ DefaultWorkDaemonCrashRetryDelays.
	CrashRetryDelays []string `toml:"crash_retry_delays" include:"replace" desc:"Crash-retry backoff schedule (array of duration strings); length = park threshold."`
}

// Work-daemon default values applied when the [work.daemon] section or
// individual fields are omitted.
const (
	DefaultWorkDaemonPollInterval = 60 * time.Second
	// DefaultWorkDaemonQuotaRetryAfter is the shortest Quota window class span —
	// the five-hour session limit. A refusal naming no window is assumed to have
	// exhausted the shortest one, so pop waits the least any real window could
	// need. The former one hour was shorter than every window a subscription
	// actually has, so it guaranteed a second refusal, and each of those used to
	// re-date the deadline from its own later moment (ADR-0235).
	DefaultWorkDaemonQuotaRetryAfter = 5 * time.Hour
)

// DefaultWorkDaemonCrashRetryDelays is the default crash-retry backoff schedule.
var DefaultWorkDaemonCrashRetryDelays = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute}

// ResolvedWorkDaemonConfig holds the parsed supervisor configuration with
// defaults applied and durations parsed.
type ResolvedWorkDaemonConfig struct {
	PollInterval         time.Duration
	AgentQuotaRetryAfter time.Duration
	CrashRetryDelays     []time.Duration
}

// ResolveWorkDaemon parses the [work.daemon] section, applying defaults for
// omitted fields and validating that every duration string is well-formed. A bad
// duration is a config error. The receiver may be nil (no [work] section), in
// which case all defaults apply.
func (c *Config) ResolveWorkDaemon() (ResolvedWorkDaemonConfig, error) {
	resolved := ResolvedWorkDaemonConfig{
		PollInterval:         DefaultWorkDaemonPollInterval,
		AgentQuotaRetryAfter: DefaultWorkDaemonQuotaRetryAfter,
		CrashRetryDelays:     append([]time.Duration(nil), DefaultWorkDaemonCrashRetryDelays...),
	}

	var q *WorkDaemonConfig
	if c != nil && c.Work != nil {
		q = c.Work.Daemon
	}
	if q == nil {
		return resolved, nil
	}

	if strings.TrimSpace(q.PollInterval) != "" {
		d, err := time.ParseDuration(q.PollInterval)
		if err != nil {
			return ResolvedWorkDaemonConfig{}, fmt.Errorf("[work.daemon] poll_interval: %w", err)
		}
		resolved.PollInterval = d
	}

	if strings.TrimSpace(q.AgentQuotaRetryAfter) != "" {
		d, err := time.ParseDuration(q.AgentQuotaRetryAfter)
		if err != nil {
			return ResolvedWorkDaemonConfig{}, fmt.Errorf("[work.daemon] agent_quota_retry_after: %w", err)
		}
		resolved.AgentQuotaRetryAfter = d
	}

	if q.CrashRetryDelays != nil {
		delays := make([]time.Duration, 0, len(q.CrashRetryDelays))
		for i, raw := range q.CrashRetryDelays {
			d, err := time.ParseDuration(raw)
			if err != nil {
				return ResolvedWorkDaemonConfig{}, fmt.Errorf("[work.daemon] crash_retry_delays[%d]: %w", i, err)
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
	// Includes selects the files that merge into this one, so it decides where
	// config comes from rather than holding a value — and a layer laid over the
	// merge cannot decide what went into it. Hence the one exception, with Repo,
	// to every leaf being overridable (ADR-0212 decision 4).
	Includes              []string             `toml:"includes" override:"never" desc:"Additional config files to merge in (paths; parent and earlier includes win)."`
	Projects              []ProjectEntry       `toml:"projects" include:"append" desc:"Directories or globs offered in the project picker."`
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
	// Deprecated: use Monitor.Dashboard. TODO: remove at next major release.
	Dashboard *DashboardConfig `toml:"dashboard" desc:"Deprecated: use [monitor.dashboard]."`
	Monitor   *MonitorConfig   `toml:"monitor" desc:"Monitor dashboard settings ([monitor] table; [monitor.dashboard] holds cursor, sort and zoom behavior)."`
	// Task holds the retired [tasks] table's one honored key, [tasks.git]
	// (ADR-0194). Everything else moved to [work.<kind>].
	Task   *TasksConfig            `toml:"tasks" include:"fields" desc:"Deprecated: use [work] (only [tasks.git] is still read)."`
	Effort map[string]EffortConfig `toml:"effort" include:"map-first-wins" desc:"Per-agent reasoning-effort ladders ([effort.<agent>] tables)."`
	// Agents holds [agents.<preset>] blocks: the settings keyed by agent preset
	// rather than by kind of work. After ADR-0195 the only key left is the
	// preset's output mode (ADR-0194); attended invocation lives on the
	// [work.attended].agents entry.
	Agents map[string]AgentConfig `toml:"agents" include:"map-fields" desc:"Per-agent preset settings ([agents.<preset>] tables)."`
	// Workbenches is the canonical TOML key for session blueprints.
	Workbenches []Workbench `toml:"workbenches" include:"append" desc:"Global session blueprints (templates)."`
	// WorkbenchOpts holds the [workbench] options table (pick_on_create, order).
	WorkbenchOpts *WorkbenchOptions   `toml:"workbench" include:"fields" desc:"Workbench options ([workbench] table)."`
	Work          *WorkConfig         `toml:"work" merge:"fields" include:"fields" desc:"Work settings ([work] table; one sub-table per kind of work)."`
	Updates       *UpdatesConfig      `toml:"updates" desc:"Auto-update behavior ([updates] table)."`
	Integrations  *IntegrationsConfig `toml:"integrations" merge:"fields" desc:"AI-agent integration settings ([integrations] table)."`
	Spend         *SpendConfig        `toml:"spend" merge:"fields" include:"fields" desc:"Spend lens settings ([spend] table)."`
	// Tmux holds global tmux-server addressing and the Tmux config include
	// ([tmux] table). Global-scope only (ADR-0199): no include: tag, not on
	// the include whitelist, not repo-scope.
	Tmux *TmuxConfig `toml:"tmux" desc:"Tmux server addressing and config include ([tmux] table)."`
	// Repo holds [repo."<path>"] override blocks keyed by any checkout path.
	// The key is canonicalized (~ expanded, symlinks resolved) at resolution
	// time; any worktree path or bare dir of the same repo resolves to the
	// same repository identity.
	//
	// It is a scope selector spelled as a table, not a table of values: it says
	// which repository the keys beneath it are stated for. Its leaves are
	// ordinary repository-scope keys, overridden through the override layer's own
	// per-repository blocks, so the node itself has nothing to override
	// (ADR-0212 decisions 3 and 4).
	Repo map[string]RepoOverrideConfig `toml:"repo" merge:"map" include:"map-first-wins" override:"never" desc:"Per-repo override blocks keyed by any checkout path ([repo.\"<path>\"] tables)."`

	// Findings holds semantic config problems collected at load time (ADR 0054).
	// Each is keyed to its config path; callers consult them through value
	// getters (e.g. EffortFor) and decide severity per their capability. They
	// are also mirrored into Warnings so a command that never consumes the
	// offending key still surfaces the problem in the picker's warning banner.
	Findings []Finding `toml:"-"`

	Warnings []string `toml:"-"` // non-serialized warnings from config loading

	// EmptyAgentOverrides names the Work-group agent lists config.override.toml
	// states as an explicit empty list (agent_list.go). It rides on the merged
	// config because the merge keeps a key's value and drops the layer that
	// wrote it, and for these keys the layer is the whole difference between "no
	// list of its own" and "no agents at all" (ADR-0202 decision 6).
	EmptyAgentOverrides []string `toml:"-"`
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
// here is accepted at BOTH repo-scope loci: the committed repo-root .pop/config.toml
// and the user's central global [repo."<path>"] override block. Adding a
// repo-scope key here makes both surfaces accept it without touching two structs.
// trunk is the sole exception — it is machine topology, an absolute path on this
// machine and never valid in committed .pop/config.toml — so it lives on the
// individual structs, not here.
type RepoScopeConfig struct {
	// Workbenches are repo-scope session blueprints (canonical key). The walker
	// unions them by name across the repo-scope ladder (ADR-0122), so a
	// higher-precedence source overrides a same-named blueprint from a lower one.
	Workbenches []Workbench `toml:"workbenches" merge:"list-by-key=name" desc:"Repo-scope session blueprints (templates)."`
	// PreferredWorkbench names the repo-default Workbench that auto-applies when
	// a session is born for any checkout of this repo (ADR-0078). It is keyed by
	// repository identity, not the exact checkout path, so it is a coarse default
	// shared by every worktree of the repo. Readable from committed .pop/config.toml as
	// well as from a [repo."<path>"] block, which — being keyed to one checkout —
	// is the more specific declaration and wins for the same key (ADR-0212).
	PreferredWorkbench string `toml:"preferred_workbench" desc:"Repo-default Workbench that auto-applies to new sessions of this repo."`
}

// RepoConfig is the repo-root .pop/config.toml surface. It is deliberately separate
// from Config: global config.toml registers projects, while .pop/config.toml only
// describes behavior for an already-registered project. It carries the shared
// repo-scope key set plus a non-decoded Trunk slot (populated only by resolution
// from a global override, never parsed from .pop/config.toml).
type RepoConfig struct {
	RepoScopeConfig
	// Trunk is the resolved path of this repository's Trunk worktree — its fork
	// base for managed worktrees — empty when the repository declares none. It is
	// stated at repository scope, in a [repo."<path>"] block of the global
	// config.toml or in the override layer's entry for the repository (ADR-0212
	// decision 3), and answers the same for every worktree of that repository.
	// A committed .pop/config.toml cannot name a machine-specific path, and a trunk
	// that had to be known before it could be read would never resolve, so this is
	// never decoded (toml:"-") and only ever populated by resolution.
	Trunk string `toml:"-"`

	// TurnCap is the resolved Turn cap for this checkout's repository: how many
	// Turns one implementation attempt may spend (ADR-0190). Zero means the
	// repository declares none and an attempt runs unbounded, as before. Like
	// trunk it is [repo."<path>"]-only — bounding a repository's drains must not
	// require committing a pop artifact into it (ADR-0191) — so it is never
	// decoded from .pop/config.toml (toml:"-") and is populated only by
	// resolution.
	TurnCap int `toml:"-"`

	// Findings holds non-fatal scope-legality problems collected while loading
	// this .pop/config.toml (ADR-0054, ADR-0083): top-level keys that are not repo-scope
	// keys (global/machine-only settings, or the [repo]-only trunk) are ignored
	// but surfaced here so a command can render them in the picker warning banner.
	Findings []Finding `toml:"-"`
}

// RepoOverrideConfig is the shape of a [repo."<path>"] block in global
// config.toml. It accepts the shared repo-scope key set plus the [repo]-only
// trunk and turn_cap keys; global-only settings (project registry, daemon knobs,
// etc.) are not.
type RepoOverrideConfig struct {
	RepoScopeConfig
	// Trunk names the checkout that is the repository's Trunk worktree. Like
	// turn_cap it describes the whole repository, so the block is matched by
	// repository identity and every worktree reads the one answer (ADR-0212
	// decision 3); the retired boolean spelling folds to the block's own key.
	Trunk *TrunkPath `toml:"trunk" desc:"Path of the checkout that is this repo's Trunk (fork base for managed worktrees)."`
	// TurnCap bounds how many Turns one implementation attempt in this repository
	// may spend (ADR-0190). Unlike trunk it is matched by repository identity, not
	// by the exact checkout that keys the block, because a bound describes the
	// repository rather than one worktree of it (ADR-0191).
	TurnCap *int `toml:"turn_cap" desc:"Max Turns one implementation attempt in this repo may spend (claude only; other presets cannot be told)."`
}

// LoadRepoConfig reads repo-root .pop/config.toml. A missing file is not an error and
// resolves to the zero config. Malformed TOML is returned to the caller so it
// can be reported while degrading behavior to defaults.
func LoadRepoConfig(repoRoot string) (RepoConfig, error) {
	return LoadRepoConfigWith(defaultDeps, repoRoot)
}

// LoadRepoConfigWith reads repo-root .pop/config.toml using injected dependencies.
func LoadRepoConfigWith(d *Deps, repoRoot string) (RepoConfig, error) {
	path := filepath.Join(repoRoot, ".pop", "config.toml")
	data, err := d.FS.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// No committed config at this anchor. Still warn about a legacy flat
			// file so a teammate's config never silently vanishes (ADR-0137).
			return RepoConfig{Findings: legacyPopTOMLFindings(d, repoRoot)}, nil
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
	// .pop/config.toml. Global/machine-only keys (and the [repo]-only trunk) are ignored
	// but surfaced as non-fatal findings so the rest of the file still loads.
	cfg.Findings = popTOMLScopeFindings(path, md)
	cfg.Findings = append(cfg.Findings, legacyPopTOMLFindings(d, repoRoot)...)
	return cfg, nil
}

// legacyPopTOMLFindings warns when a legacy flat .pop.toml sits at repoRoot.
// That file is no longer read (ADR-0137): the committed in-tree home is now
// .pop/config.toml. This is warn-and-ignore — the flat file's contents are
// never parsed, but its mere presence draws a one-line finding pointing at the
// new path so committed config never silently vanishes.
func legacyPopTOMLFindings(d *Deps, repoRoot string) []Finding {
	legacy := filepath.Join(repoRoot, ".pop.toml")
	if _, err := d.FS.Stat(legacy); err != nil {
		return nil
	}
	return []Finding{{
		Path:    "deprecated.pop_toml",
		Message: fmt.Sprintf("%s is ignored; move repo-scope config to .pop/config.toml (ADR-0137)", legacy),
	}}
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

// ResolveRepoConfig returns the effective RepoConfig for checkoutPath, resolved
// scope-first (ADR-0212 decision 1) — the most specific source wins, and every
// source is a declaration, config's one gap-filler having retired with the
// runtime record (decision 5):
//
//	config.toml [repo."<path>"] → worktree .pop/config.toml → trunk-anchor
//	.pop/config.toml → global config.toml → default
//
// The override layer is then laid over that answer (ADR-0212 decision 2), its
// repository entry over its global one, so an override wins whatever the ladder
// above decided.
//
// The committed .pop/config.toml is read at two anchors (ADR-0083's surviving
// two-anchor law): this worktree first, then the trunk anchor (the Trunk
// worktree, or the repository-identity root for a bare repo), presence deciding.
// Fields are merged individually; a nil pointer in the override means "not set"
// and the next source down wins. trunk exists only in [repo."<path>"] blocks and
// is applied only when the block's key path exactly matches checkoutPath
// (per-checkout semantics).
//
// A missing .pop/config.toml is not an error. A malformed .pop/config.toml degrades to the
// zero config (the error is returned so callers may surface a warning).
func (c *Config) ResolveRepoConfig(d *Deps, checkoutPath string) (RepoConfig, error) {
	// A renamed execution key (queue_base/execution_base → trunk) is recorded at
	// load as a blocking "repo" finding rather than aborting Load(). This getter
	// is the execution-config consumption point, so it surfaces that finding as
	// its error (ADR 0054): consuming commands treat it as fatal, the migration
	// tripwire stays loud, while a command that never resolves repo config (the
	// project dashboard) is unaffected. Checked before touching .pop/config.toml so the
	// fatal config-global finding always wins over a per-checkout problem.
	if err := c.blockingFindingFor("repo"); err != nil {
		return RepoConfig{}, err
	}
	// The shared enumerator does the identity walk, the [repo."<path>"] match, and
	// the two-anchor .pop/config.toml reads (worktree then trunk); the walker merges the
	// shared RepoScopeConfig down the scope-first ladder (ADR-0212, ADR-0122). trunk
	// stays caller-side inside resolveRepoConfig with its exact-checkout-path condition.
	return c.newRepoScope(d, checkoutPath).resolveRepoConfig()
}

// ResolveWorkbenchesWith returns the union of Workbenches from all homes (global
// config, committed .pop/config.toml, and [repo."<path>"]), resolved by walking
// the same scope-first ladder ResolveRepoConfig walks: [repo."<path>"] > worktree
// .pop/config.toml > trunk-anchor .pop/config.toml > global library, with the
// override layer laid over the answer. The committed .pop/config.toml is unioned
// at two anchors (ADR-0083): this worktree outranks the inherited trunk anchor
// (the Trunk worktree, or the repository-identity root for a bare repo).
// Blueprints union by name, so a name a more specific source redefines is taken
// from there and the collision is warned about.
func (c *Config) ResolveWorkbenchesWith(d *Deps, checkoutPath string) ([]Workbench, []string) {
	e := c.newRepoScope(d, checkoutPath)

	var merged RepoScopeConfig
	var warnings []string
	seen := make(map[string]string) // name -> source for collision warnings

	// Walk the ladder lowest rung first, so a name defined higher up overwrites
	// the one below it (ADR-0122 list-by-key), then the override layer over the
	// result — a blueprint a human overrode wins whatever the ladder decided
	// (ADR-0212 decision 2). Each collision is reported against seen, read before
	// this rung's own names are recorded, so the lowest rung can collide with
	// nothing.
	for _, src := range append(e.scopeLadder(), e.overrideSources()...) {
		label := src.label
		pol := repoScopePolicy()
		pol.onCollision = func(keyPath string) {
			name := workbenchCollisionName(keyPath)
			warnings = append(warnings, fmt.Sprintf(
				"session template %q defined in both %s and %s; using %s",
				name, seen[name], label, label,
			))
		}
		scope := RepoScopeConfig{Workbenches: src.scope.Workbenches}
		mergeWalk(&merged, &scope, repoScopeMetadata(scope), pol)
		for _, tmpl := range scope.Workbenches {
			seen[tmpl.Name] = label
		}
	}

	return merged.Workbenches, warnings
}

// parseAttemptRetryDelays parses one Work group's attempt_retry_delays,
// applying the default schedule for an omitted key. An explicit empty array
// yields zero delay (instant retries).
func parseAttemptRetryDelays(section string, raw []string) ([]time.Duration, error) {
	if raw == nil {
		return append([]time.Duration(nil), DefaultTaskAttemptRetryDelays...), nil
	}
	delays := make([]time.Duration, 0, len(raw))
	for i, entry := range raw {
		d, err := time.ParseDuration(entry)
		if err != nil {
			return nil, fmt.Errorf("[%s] attempt_retry_delays[%d]: %w", section, i, err)
		}
		delays = append(delays, d)
	}
	return delays, nil
}

// ResolveImplementAttemptRetryDelays parses [work.implement].attempt_retry_delays.
// The receiver may be nil.
func (c *Config) ResolveImplementAttemptRetryDelays() ([]time.Duration, error) {
	var raw []string
	if c != nil && c.Work != nil && c.Work.Implement != nil {
		raw = c.Work.Implement.AttemptRetryDelays
	}
	return parseAttemptRetryDelays("work.implement", raw)
}

// ResolveVerifyAttemptRetryDelays parses [work.verify].attempt_retry_delays.
// The receiver may be nil.
func (c *Config) ResolveVerifyAttemptRetryDelays() ([]time.Duration, error) {
	var raw []string
	if c != nil && c.Work != nil && c.Work.Verify != nil {
		raw = c.Work.Verify.AttemptRetryDelays
	}
	return parseAttemptRetryDelays("work.verify", raw)
}

// ResolveImplementMaxTries returns the started-attempt cap for implement from
// config: [work.implement].max_tries, else DefaultTaskMaxTries. An explicit
// --max-tries flag is resolved by the caller.
func (c *Config) ResolveImplementMaxTries() int {
	if c != nil && c.Work != nil && c.Work.Implement != nil &&
		c.Work.Implement.MaxTries != nil && *c.Work.Implement.MaxTries > 0 {
		return *c.Work.Implement.MaxTries
	}
	return DefaultTaskMaxTries
}

// ResolveVerifyMaxTries returns the started-attempt cap for verify from config:
// [work.verify].max_tries, else DefaultTaskMaxTries.
func (c *Config) ResolveVerifyMaxTries() int {
	if c != nil && c.Work != nil && c.Work.Verify != nil &&
		c.Work.Verify.MaxTries != nil && *c.Work.Verify.MaxTries > 0 {
		return *c.Work.Verify.MaxTries
	}
	return DefaultTaskMaxTries
}

// ImplementIncludesImplementationConvention reports whether every implement
// prompt carries the resolved `implementation` convention (ADR-0246). It reads
// [work.implement].include_implementation_convention alone — the toggle is
// independent of [work.refine].enabled, so upfront adherence can be driven with
// the pass switched off.
func (c *Config) ImplementIncludesImplementationConvention() bool {
	if c == nil || c.Work == nil || c.Work.Implement == nil {
		return false
	}
	return c.Work.Implement.IncludeImplementationConvention
}

// ImplementAgents returns the commands of the [work.implement].agents list, in
// configured order, or nil.
func (c *Config) ImplementAgents() []string {
	return c.ImplementAgentEntries().Commands()
}

// ImplementAgentEntries returns the [work.implement].agents entries as
// declared, malformed ones included, for surfaces that name entries rather than
// run them.
func (c *Config) ImplementAgentEntries() AgentEntries {
	if c == nil || c.Work == nil || c.Work.Implement == nil {
		return nil
	}
	return c.Work.Implement.Agents
}

// VerifyAgents returns the commands of the [work.verify].agents list, in
// configured order, or nil.
func (c *Config) VerifyAgents() []string {
	return c.VerifyAgentEntries().Commands()
}

// VerifyAgentEntries returns the [work.verify].agents entries as declared.
func (c *Config) VerifyAgentEntries() AgentEntries {
	if v := c.VerifySettings(); v != nil {
		return v.Agents
	}
	return nil
}

// VerifySettings returns the [work.verify] block, or nil when undeclared.
func (c *Config) VerifySettings() *VerifyConfig {
	if c == nil || c.Work == nil {
		return nil
	}
	return c.Work.Verify
}

// RefineAgents returns the commands of the [work.refine].agents list, in
// configured order, or nil.
func (c *Config) RefineAgents() []string {
	return c.RefineAgentEntries().Commands()
}

// RefineAgentEntries returns the [work.refine].agents entries as declared.
func (c *Config) RefineAgentEntries() AgentEntries {
	if r := c.RefineSettings(); r != nil {
		return r.Agents
	}
	return nil
}

// RefineSettings returns the [work.refine] block, or nil when undeclared.
func (c *Config) RefineSettings() *RefineConfig {
	if c == nil || c.Work == nil {
		return nil
	}
	return c.Work.Refine
}

// RoutineAgents returns the commands of the [work.routine].agents list, or nil.
// An empty list leaves the caller to fall through to the implement group.
func (c *Config) RoutineAgents() []string {
	return c.RoutineAgentEntries().Commands()
}

// RoutineAgentEntries returns the [work.routine].agents entries as declared.
func (c *Config) RoutineAgentEntries() AgentEntries {
	if c == nil || c.Work == nil || c.Work.Routine == nil {
		return nil
	}
	return c.Work.Routine.Agents
}

// AttendedAgents returns the commands of the [work.attended].agents list, or
// nil. Every human-facing session pop opens shares this one group (ADR-0194).
func (c *Config) AttendedAgents() []string {
	return c.AttendedAgentEntries().Commands()
}

// AttendedAgentEntries returns the [work.attended].agents entries as declared.
func (c *Config) AttendedAgentEntries() AgentEntries {
	if c == nil || c.Work == nil || c.Work.Attended == nil {
		return nil
	}
	return c.Work.Attended.Agents
}

// TaskAgentOutput returns the configured output mode for one agent preset,
// from [agents.<preset>].output. Defaults to "auto"; validation is owned by the
// task executor.
func (c *Config) TaskAgentOutput(agent string) string {
	if out := c.AgentSettingsFor(agent).Output; out != "" {
		return out
	}
	return "auto"
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

// TmuxSocket returns the configured tmux server socket name (tmux.socket).
// Empty means unset: callers must emit no -L flag (ADR-0199). The receiver
// may be nil.
func (c *Config) TmuxSocket() string {
	if c == nil || c.Tmux == nil {
		return ""
	}
	return c.Tmux.Socket
}

// ConfiguredTmuxSocket returns tmux.socket from the default config path, or ""
// when the key is unset or the config cannot be loaded. Callers hand the
// result to tmux.New so every production construction shares one resolution.
func ConfiguredTmuxSocket() string {
	cfg, err := Load(DefaultConfigPath())
	if err != nil || cfg == nil {
		return ""
	}
	return cfg.TmuxSocket()
}

// TmuxInclude returns the configured Tmux config include path (tmux.include).
// Empty/unset yields DefaultTmuxIncludePath. The receiver may be nil.
func (c *Config) TmuxInclude() string {
	if c == nil || c.Tmux == nil || c.Tmux.Include == "" {
		return DefaultTmuxIncludePath
	}
	return c.Tmux.Include
}

// ConfiguredTmuxInclude returns tmux.include from the default config path, or
// DefaultTmuxIncludePath when unset or unloadable. Callers hand the result to
// tmux.New alongside the socket so every production construction shares one
// resolution; the tmux module never loads config itself.
func ConfiguredTmuxInclude() string {
	cfg, err := Load(DefaultConfigPath())
	if err != nil || cfg == nil {
		return DefaultTmuxIncludePath
	}
	return cfg.TmuxInclude()
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
// scope-first law (ADR-0212 decision 1): the most specific source wins, under
// the override layer, which is laid over that answer rather than ranked inside
// it (decision 2). Highest → lowest, the sources that carry this key:
//
//	config.override.toml [repo."<id>"] override · stated for this repository
//	config.toml [repo."<path>"]        declaration · keyed to this checkout
//	./.pop/config.toml                 declaration · committed, this worktree
//	<trunk>/.pop/config.toml (→ id-root)  declaration · committed, inherited
//	→ none
//
// The key has no global home, so the ladder starts at the repository. The in-tree
// .pop/config.toml is read at two anchors — this worktree and the Trunk worktree,
// the trunk read falling back to the Repository identity root for a bare repo —
// with presence deciding which supplies the value: a worktree with its own
// .pop/config.toml overrides the inherited trunk one, and a worktree without
// inherits trunk's. The inherited anchor reuses ADR-0078's Deps.Trunk resolver
// and its this-is-trunk read-once guard (skipped when the inherited anchor is
// this very checkout, so a stale name never double-warns).
//
// The stated rung stays three-valued (ADR-0078, at the key's new home): an entry
// stated as an empty name is an explicit none and short-circuits to flat/prompt
// here; a named entry uses that name; no entry at all falls through to the
// declarations. The per-checkout entries pop once recorded for itself are gone
// (ADR-0212 decisions 5 and 6) — a preference is stated for the repository, so
// every worktree of it reads the one answer.
//
// A stored name that does not resolve to a real Workbench for this checkout is
// skipped with a non-fatal warning (ADR-0054 style) at each layer and resolution
// continues down the chain — a broken preference never blocks getting into a
// session and never silently vanishes. The receiver may be nil.
func (c *Config) ResolvePreferredWorkbench(d *Deps, checkoutPath string) (string, []string) {
	if c == nil {
		return "", nil
	}
	e := c.newRepoScope(d, checkoutPath)

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
	// chain. Returns (result, done). The stated rung handles its own
	// explicit-none short-circuit and uses consider only for the resolve-or-warn.
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

	// Iterate the shared source chain (the enumerator owns the anchor resolution
	// and the read-once guard). Declaration rungs fall through when empty; the
	// stated rung keeps its three-valued explicit-none short-circuit.
	for _, src := range e.preferredSources() {
		// A stated rung is in the chain only when the override layer holds an
		// entry, so an empty name there is an explicit none — flat here, and
		// nothing below is asked.
		if src.stated && src.name == "" {
			return "", warnings
		}
		if name, done := consider(src.name); done {
			return name, warnings
		}
	}

	// None.
	return "", warnings
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

// ProjectWorktreeDisplay returns how the project dashboard should arrange
// worktree rows. An absent key, an empty value or a value the vocabulary does
// not contain resolves to WorktreeDisplayFlat: an unreadable preference must not
// change which rows the dashboard lists, and the rejected value is already
// surfaced as a load-time finding (worktreeDisplayFindings). The deprecated
// [select] table is honored like every other project key.
func (c *Config) ProjectWorktreeDisplay() WorktreeDisplay {
	if c == nil {
		return WorktreeDisplayFlat
	}
	pc := c.projectConfig()
	if pc == nil {
		return WorktreeDisplayFlat
	}
	if WorktreeDisplay(pc.WorktreeDisplay) == WorktreeDisplayNested {
		return WorktreeDisplayNested
	}
	return WorktreeDisplayFlat
}

// ProjectSessionOrdering returns how the project dashboard orders its rows.
// An absent key, an empty value or a value the vocabulary does not contain
// resolves to SessionOrderingUnified: an unreadable preference must not change
// which row sits next to the prompt, and the rejected value is already
// surfaced as a load-time finding (sessionOrderingFindings). The deprecated
// [select] table is honored like every other project key.
func (c *Config) ProjectSessionOrdering() SessionOrdering {
	if c == nil {
		return SessionOrderingUnified
	}
	pc := c.projectConfig()
	if pc == nil {
		return SessionOrderingUnified
	}
	if SessionOrdering(pc.SessionOrdering) == SessionOrderingLiveFirst {
		return SessionOrderingLiveFirst
	}
	return SessionOrderingUnified
}

// worktreeDisplayFindings rejects a [project] worktree_display value outside the
// two-word vocabulary. Per ADR 0054 it is collected, not thrown: the dashboard
// still opens — flat — and names the offending value in its warning banner
// rather than silently arranging rows the way the operator did not ask for. Only
// the main config is checked, because [project] is not on the include whitelist:
// an included table is dropped and warned about as a whole.
func worktreeDisplayFindings(path string, pc *ProjectConfig) []Finding {
	if pc == nil || pc.WorktreeDisplay == "" {
		return nil
	}
	switch WorktreeDisplay(pc.WorktreeDisplay) {
	case WorktreeDisplayFlat, WorktreeDisplayNested:
		return nil
	}
	return []Finding{{
		Path: "project.worktree_display",
		Message: fmt.Sprintf(
			"%s: [project] worktree_display = %q is not one of %q, %q; using %q",
			path, pc.WorktreeDisplay, WorktreeDisplayFlat, WorktreeDisplayNested, WorktreeDisplayFlat,
		),
	}}
}

// sessionOrderingFindings rejects a [project] session_ordering value outside
// the two-word vocabulary. Per ADR 0054 it is collected, not thrown: the
// dashboard still opens — on the unified timeline — and names the offending
// value in its warning banner rather than silently ordering rows the way the
// operator did not ask for. Only the main config is checked, because [project]
// is not on the include whitelist: an included table is dropped and warned
// about as a whole.
func sessionOrderingFindings(path string, pc *ProjectConfig) []Finding {
	if pc == nil || pc.SessionOrdering == "" {
		return nil
	}
	switch SessionOrdering(pc.SessionOrdering) {
	case SessionOrderingUnified, SessionOrderingLiveFirst:
		return nil
	}
	return []Finding{{
		Path: "project.session_ordering",
		Message: fmt.Sprintf(
			"%s: [project] session_ordering = %q is not one of %q, %q; using %q",
			path, pc.SessionOrdering, SessionOrderingUnified, SessionOrderingLiveFirst, SessionOrderingUnified,
		),
	}}
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
	md := c.monitorDashboardConfig()
	if md == nil {
		return DashboardCursorCurrentRegistered
	}
	switch md.CursorPosition {
	case DashboardCursorCurrentRegistered, DashboardCursorCurrentAny, DashboardCursorFirstActive:
		return md.CursorPosition
	case "":
		if c.Dashboard != nil && c.Dashboard.CurrentPaneAlwaysUnderCursor {
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
// [monitor.dashboard] zoom_on_switch = false to focus the pane in place,
// preserving the window's split layout (e.g. nvim above, agent below).
func (c *Config) DashboardZoomOnSwitch() bool {
	md := c.monitorDashboardConfig()
	if md == nil || md.ZoomOnSwitch == nil {
		return true
	}
	return *md.ZoomOnSwitch
}

// DashboardKillPanePromptEnabled reports whether C-x on the monitor dashboard
// asks y/N before it destroys the cursored pane. Defaults to true: the prompt is
// the only mitigation for killing a pane whose agent outlives it (ADR-0205), so
// turning it off is an explicit acceptance of that risk.
func (c *Config) DashboardKillPanePromptEnabled() bool {
	md := c.monitorDashboardConfig()
	if md == nil || md.KillPanePromptEnabled == nil {
		return true
	}
	return *md.KillPanePromptEnabled
}

// DashboardSortCriteria returns the configured sort criteria for the dashboard.
// Defaults to [status, pane_last_active_at, alphabetical].
func (c *Config) DashboardSortCriteria() []string {
	md := c.monitorDashboardConfig()
	if md == nil || len(md.SortCriteria) == 0 {
		return DefaultSortCriteria
	}
	return md.SortCriteria
}

func (c *Config) projectConfig() *ProjectConfig {
	if c.Project != nil {
		return c.Project
	}
	return c.Select
}

// monitorDashboardConfig resolves the effective [monitor.dashboard] settings,
// aliasing the deprecated [dashboard] table (ADR-0206): returns the new table
// when present, else the deprecated one recast into the new shape. The
// precedence mirrors projectConfig()'s for [project]/[select] — present, the
// new table wins whole, key for key, over the old one.
func (c *Config) monitorDashboardConfig() *MonitorDashboardConfig {
	if c == nil {
		return nil
	}
	if c.Monitor != nil && c.Monitor.Dashboard != nil {
		return c.Monitor.Dashboard
	}
	if c.Dashboard == nil {
		return nil
	}
	return &MonitorDashboardConfig{
		CursorPosition: c.Dashboard.CursorPosition,
		SortCriteria:   c.Dashboard.SortCriteria,
		ZoomOnSwitch:   c.Dashboard.ZoomOnSwitch,
	}
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
	// A machine upgrading into ADR-0212 decision 5 still holds pop's retired
	// runtime record. It folds into the override layer here, before any layer is
	// read, so this very load resolves through what it carried. It happens once
	// per machine: the fold retires the file it read.
	foldRetiredRuntimeRecord(d)
	var cfg Config
	md, err := toml.DecodeFile(path, &cfg)
	if err != nil {
		return nil, err
	}
	overrideMD, err := applyConfigLayerMerge(d, &cfg, path, md)
	if err != nil {
		return nil, err
	}
	if err := includeRefineConventionRenameError(path, md); err != nil {
		return nil, err
	}
	if err := includeRefineConventionRenameError(DefaultOverrideConfigPathWith(d), overrideMD); err != nil {
		return nil, err
	}
	for _, f := range retiredTasksSectionFindings(path, md) {
		cfg.recordFinding(f)
	}
	for _, f := range effortConfigFindings(path, md) {
		cfg.recordFinding(f)
	}
	for _, f := range agentConfigFindings(path, md) {
		cfg.recordFinding(f)
	}
	for _, f := range agentEntryFindings(path, &cfg) {
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
	for _, f := range retiredQueueSectionFindings(path, md) {
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
	// Runs after the [select] alias is folded in, so the finding is raised once
	// for whichever table actually carried the value.
	for _, f := range worktreeDisplayFindings(path, cfg.projectConfig()) {
		cfg.recordFinding(f)
	}
	for _, f := range sessionOrderingFindings(path, cfg.projectConfig()) {
		cfg.recordFinding(f)
	}

	configDir := filepath.Dir(path)
	// Include merge is first-definition-wins across the ADR-0037 whitelist,
	// driven by the include: tags through the shared walker (ADR-0122). One
	// policy threads its claimed ledger across every include so the first source
	// to set a field wins over later ones; the ledger is seeded from the parent
	// config so an include never overrides a field the parent already set. The
	// collision callback reads the walker's dotted key path and rebuilds today's
	// exact per-section warning strings; whitelist enforcement ("ignored")
	// stays with includeFileWarnings below, so untagged sections are dropped
	// silently by the walker and warned once by that helper.
	var currentInclude string
	includePol := includePolicy(func(keyPath string) {
		cfg.Warnings = append(cfg.Warnings, includeCollisionMessage(currentInclude, keyPath))
	}, nil)
	seedIncludeClaims(includePol, &cfg, md, overrideMD)
	for _, include := range cfg.Includes {
		expanded := expandHomeWith(d, include)
		if !filepath.IsAbs(expanded) {
			expanded = filepath.Join(configDir, expanded)
		}
		currentInclude = expanded

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
		for _, f := range agentConfigFindings(expanded, includedMD) {
			cfg.recordFinding(f)
		}
		for _, f := range agentEntryFindings(expanded, &included) {
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
		if err := includeRefineConventionRenameError(expanded, includedMD); err != nil {
			return nil, err
		}
		for _, f := range retiredTasksSectionFindings(expanded, includedMD) {
			cfg.recordFinding(f)
		}
		cfg.Warnings = append(cfg.Warnings, includeFileWarnings(expanded, &included, d)...)

		if included.Workbenches != nil {
			tmplFindings, validTemplates := workbenchFindings(expanded, included.Workbenches)
			for _, f := range tmplFindings {
				cfg.recordFinding(f)
			}
			included.Workbenches = validTemplates
		}

		mergeWalk(&cfg, &included, includedMD, includePol)
	}

	// Preset findings run after includes so a roster that arrived only via an
	// include is validated against the final list. Materialize the resolved
	// roster (shipped defaults when undeclared) so config show surfaces it.
	for _, f := range workViewPresetFindings(path, &cfg) {
		cfg.recordFinding(f)
	}
	materializeWorkViewPresets(&cfg)

	return &cfg, nil
}

// seedIncludeClaims marks every first-wins include field the parent config
// already set as claimed, so a later include loses the collision to it. Map and
// append sections need no seeding — they collide off the destination's live
// keys/order. Work-group claims are value-driven; [workbench] claims are
// metadata-driven off the parent file, a declared `pick_on_create = false` being
// a meaningful value the unset zero value cannot be told from.
// overrideMD adds the claims of the override layer, which outranks every
// hand-authored file including an include.
// workGroupIncludeKeys maps every field-wise [work.<group>] sub-table to the
// include-whitelisted keys it carries. It is derived from the include: tags
// themselves, so a key added to a Work group is seeded for free and the claim
// ledger can never drift from the schema it guards.
var workGroupIncludeKeys = includeKeysByWorkGroup()

func includeKeysByWorkGroup() map[string][]string {
	out := map[string][]string{}
	t := reflect.TypeOf(WorkConfig{})
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Tag.Get(includeTagName) != kindFields {
			continue
		}
		group := f.Type
		for group.Kind() == reflect.Ptr {
			group = group.Elem()
		}
		if group.Kind() != reflect.Struct {
			continue
		}
		var keys []string
		for j := 0; j < group.NumField(); j++ {
			sf := group.Field(j)
			if _, ok := sf.Tag.Lookup(includeTagName); !ok {
				continue
			}
			if name := tomlName(sf); name != "" && name != "-" {
				keys = append(keys, name)
			}
		}
		out[tomlName(f)] = keys
	}
	return out
}

func seedIncludeClaims(policy *mergePolicy, cfg *Config, md, overrideMD toml.MetaData) {
	claimOverrideKeys(policy, overrideMD)
	if cfg.Task != nil && cfg.Task.Git != nil {
		policy.claim("tasks.git")
	}
	if cfg.Work != nil {
		if impl := cfg.Work.Implement; impl != nil {
			if len(impl.Agents) > 0 {
				policy.claim("work.implement.agents")
			}
			if impl.MaxTries != nil {
				policy.claim("work.implement.max_tries")
			}
			if impl.AttemptRetryDelays != nil {
				policy.claim("work.implement.attempt_retry_delays")
			}
			if impl.Git != nil {
				policy.claim("work.implement.git")
			}
		}
		// The sibling groups merge per field, so each key the parent declared is
		// claimed on its own. These claims are metadata-driven rather than
		// value-driven: `enabled = false` and an unset enabled are the same Go
		// value, and only the parent's metadata can tell the deliberate opt-out
		// from silence.
		for group, keys := range workGroupIncludeKeys {
			for _, key := range keys {
				if md.IsDefined("work", group, key) {
					policy.claim("work." + group + "." + key)
				}
			}
		}
		if md.IsDefined("work", "dashboard", "tasks", "presets") {
			policy.claim("work.dashboard.tasks.presets")
		}
	}
	// [agents.<preset>] merges per field, so each field the parent set is claimed
	// on its own. Claims are value-driven, like the Work-group ones above: an
	// unset field is a nil argument list or an empty string, never a meaningful
	// value an include should lose to.
	for preset, block := range cfg.Agents {
		if strings.TrimSpace(block.Output) != "" {
			policy.claim("agents." + preset + ".output")
		}
	}
	if md.IsDefined("workbench", "pick_on_create") {
		policy.claim("workbench.pick_on_create")
	}
	if md.IsDefined("workbench", "order") {
		policy.claim("workbench.order")
	}
}

// includeCollisionMessage rebuilds the byte-identical first-wins skip warning
// for a whitelisted include field, from the walker's dotted key path. The
// per-section wording predates the walker and is pinned by the config test
// suite: map keys stringify into the path, so the map-backed sections are
// matched by prefix and the fixed fields by exact key.
func includeCollisionMessage(path, keyPath string) string {
	if key, ok := strings.CutPrefix(keyPath, "repo."); ok {
		return fmt.Sprintf("%s: [repo.%q] skipped, key already defined (first definition wins)", path, key)
	}
	if agent, ok := strings.CutPrefix(keyPath, "effort."); ok {
		return fmt.Sprintf("%s: [effort.%s] skipped, already defined (first definition wins)", path, agent)
	}
	if rest, ok := strings.CutPrefix(keyPath, "agents."); ok {
		// [agents.<preset>] merges per field, so the reported collision names the
		// field rather than the whole block.
		if preset, field, found := strings.Cut(rest, "."); found {
			return fmt.Sprintf("%s: [agents.%s] %s skipped, already defined (first definition wins)", path, preset, field)
		}
		return fmt.Sprintf("%s: [agents.%s] skipped, already defined (first definition wins)", path, rest)
	}
	switch keyPath {
	case "work.implement.git":
		return fmt.Sprintf("%s: [work.implement.git] skipped, already defined (first definition wins)", path)
	case "tasks.git":
		return fmt.Sprintf("%s: [tasks.git] skipped, already defined (first definition wins)", path)
	case "workbench.pick_on_create":
		return fmt.Sprintf("%s: [workbench] pick_on_create skipped, already defined (first definition wins)", path)
	case "workbench.order":
		return fmt.Sprintf("%s: [workbench] order skipped, already defined (first definition wins)", path)
	}
	// Every [work.<group>] merges per key, so a collision names the group's table
	// and the one key that lost — the whole table is never what was skipped.
	if rest, ok := strings.CutPrefix(keyPath, "work."); ok {
		if group, key, found := strings.Cut(rest, "."); found && !strings.Contains(key, ".") {
			return fmt.Sprintf("%s: [work.%s].%s skipped, already defined (first definition wins)", path, group, key)
		}
	}
	return fmt.Sprintf("%s: %s skipped, already defined (first definition wins)", path, keyPath)
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

// agentConfigFindings reports an unknown key inside an [agents.<preset>] block,
// so a typo'd setting is surfaced rather than silently ignored. The two attended
// keys ADR-0195 cut are named specially: they were read until this release and
// their replacement is an address in another table, so a file still carrying one
// is told where its invocation now lives rather than being told it made a typo.
// The preset name itself is not validated here: config does not know the agent
// adapter registry, and an unknown preset simply never matches a session.
func agentConfigFindings(path string, md toml.MetaData) []Finding {
	validKeys := map[string]bool{"output": true}
	retiredKeys := map[string]bool{"attended_args": true, "attended_model": true}
	var findings []Finding
	seen := make(map[string]bool)
	for _, key := range md.Undecoded() {
		if len(key) < 3 || key[0] != "agents" || validKeys[key[2]] {
			continue
		}
		f := Finding{
			Path:    fmt.Sprintf("agents.%s.%s", key[1], key[2]),
			Message: fmt.Sprintf("%s: [agents.%s] unknown key %q; valid keys: output", path, key[1], key[2]),
		}
		if retiredKeys[key[2]] {
			f.Message = fmt.Sprintf(
				"%s: [agents.%s] %s is no longer read; an attended session's whole invocation lives in its [work.attended].agents entry — use cmd = \"%s …\"",
				path, key[1], key[2], key[1],
			)
		}
		if seen[f.Path] {
			continue
		}
		seen[f.Path] = true
		findings = append(findings, f)
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
		// .pop/config.toml-level / top-level (len==1) renames
		if len(key) == 1 {
			switch key[0] {
			case "worktree_ready":
				add(fmt.Sprintf("%s: worktree_ready was removed; use trunk = \"<path>\" in a global [repo.%q] block to name the Trunk worktree", path, "<path>"))
			case "execution_base":
				add(fmt.Sprintf("%s: execution_base was renamed to trunk; use trunk = \"<path>\" in a global [repo.%q] block", path, "<path>"))
			case "queue_base":
				add(fmt.Sprintf("%s: queue_base was renamed to trunk; use trunk = \"<path>\" in a global [repo.%q] block", path, "<path>"))
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

// validateRepoConfigMetadata keeps the repo-local .pop/config.toml path hard-failing on
// the same migration tripwires (LoadRepoConfig has no Config to carry findings
// on, and a stale key there still surfaces fatally via ResolveRepoConfig's
// returned error). It returns a plain error — deliberately NOT a Finding — so a
// caller iterating checkouts (the queue's representative resolver) can tell a
// fatal config-global migration finding apart from a per-checkout .pop/config.toml
// problem it should degrade past.
func validateRepoConfigMetadata(path string, md toml.MetaData) error {
	if findings := repoRenameFindings(path, md); len(findings) > 0 {
		return errors.New(findings[0].Message)
	}
	return nil
}

// includeRefineConventionRenameError is the load-time refusal for the retired
// [work.implement].include_refine_convention key (ADR-0246). A bool defaulting
// to false would silently lose behaviour under a silent alias or ignore, so the
// old name hard-fails naming the new one — the same habit as
// execution_base/queue_base → trunk.
func includeRefineConventionRenameError(path string, md toml.MetaData) error {
	for _, key := range md.Undecoded() {
		if len(key) == 3 && key[0] == "work" && key[1] == "implement" && key[2] == "include_refine_convention" {
			return fmt.Errorf("%s: [work.implement].include_refine_convention was renamed to include_implementation_convention", path)
		}
	}
	return nil
}

// retiredTasksSectionFindings returns load-time findings when a config file
// still carries the pre-cut [tasks] or [workload] tables. ADR-0194 moved every
// kind-scoped key onto its Work group and cut the old addresses rather than
// aliasing them, so an unread key would otherwise degrade silently to the
// built-in default agent. Each finding names the new address for the table it
// found. [tasks.git] is exempt: it is the one honored read-compat key.
func retiredTasksSectionFindings(path string, md toml.MetaData) []Finding {
	replacements := map[string]string{
		"tasks.implement":            "[work.implement]",
		"tasks.verify":               "[work.verify]",
		"tasks.presets":              "[agents.<preset>]",
		"tasks.max_tries":            "[work.implement].max_tries and [work.verify].max_tries",
		"tasks.attempt_retry_delays": "[work.implement].attempt_retry_delays and [work.verify].attempt_retry_delays",
		"workload":                   "[work]",
		"routines":                   "[work.routine]",
	}
	seen := make(map[string]bool)
	var findings []Finding
	add := func(key, replacement string) {
		if seen[key] {
			return
		}
		seen[key] = true
		findings = append(findings, Finding{
			Path:    "retired_section." + key,
			Message: fmt.Sprintf("%s: [%s] is no longer read; agent lists and task-execution settings are grouped by kind of work — use %s", path, key, replacement),
		})
	}
	for _, key := range md.Undecoded() {
		switch {
		case len(key) >= 2 && key[0] == "tasks":
			if replacement, ok := replacements["tasks."+key[1]]; ok {
				add("tasks."+key[1], replacement)
			} else {
				add("tasks."+key[1], "[work]")
			}
		case len(key) >= 1 && (key[0] == "workload" || key[0] == "routines"):
			add(key[0], replacements[key[0]])
		}
	}
	return findings
}

// retiredQueueSectionFindings returns a load-time finding when a config file
// still carries a [queue] table. The section was cut, not aliased: `pop queue`
// became `pop work` and its timing moved to [work.daemon], so a leftover [queue]
// is an unknown section whose keys are read by nothing (fail-soft, ADR-0054). The
// message keeps the [queue].agents pointer alive for files that set that key,
// since agent fallback lives somewhere else again ([work.implement].agents).
func retiredQueueSectionFindings(path string, md toml.MetaData) []Finding {
	found, agents := false, false
	for _, key := range md.Undecoded() {
		if len(key) > 0 && key[0] == "queue" {
			found = true
			if len(key) == 2 && key[1] == "agents" {
				agents = true
			}
		}
	}
	if !found {
		return nil
	}
	msg := fmt.Sprintf(
		"%s: [queue] is not a config section; supervisor timing moved to [work.daemon] (poll_interval, agent_quota_retry_after, crash_retry_delays)",
		path,
	)
	if agents {
		msg += "; configure agent fallback under [work.implement].agents"
	}
	return []Finding{{Path: "unknown_section.queue", Message: msg}}
}

// repoScopeLegalKeys returns the set of TOML keys that are legal at repo scope,
// derived by reflection from the shared RepoScopeConfig schema (ADR-0083). It is
// the single source of truth for both repo-scope loci — the committed .pop/config.toml
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

// popTOMLScopeFindings enforces scope-legality for a committed .pop/config.toml
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
		msg := fmt.Sprintf("%s: %q is not valid in .pop/config.toml and was ignored (only repo-scope keys are accepted)", path, name)
		// The two [repo]-only keys get a message naming their real home instead of
		// the generic one: both are machine-side, and turn_cap is deliberately not
		// shared so bounding a repository's drains never needs a commit (ADR-0191).
		if name == "trunk" || name == "turn_cap" {
			msg = fmt.Sprintf("%s: %s is only valid in a global [repo.%q] override block and was ignored", path, name, "<path>")
		}
		findings = append(findings, Finding{Path: "repo_scope.unknown_key", Message: msg})
	}
	return findings
}

// repoBlockWarnings returns load-time findings for unknown keys inside
// [repo."<path>"] blocks. Only the shared repo-scope key set (plus the
// [repo]-only trunk and turn_cap) is valid there; any other key is silently
// degraded but surfaced as a finding. The valid set is derived from the shared
// schema (repoBlockLegalKeys), so it stays in sync with .pop/config.toml
// scope-legality and with what a block of the override layer may hold.
func repoBlockWarnings(path string, md toml.MetaData) []Finding {
	validRepoKeys := repoBlockLegalKeys()
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
				"%s: [repo.%q] unknown key %q ignored (only trunk, turn_cap, workbenches, and preferred_workbench are accepted)",
				path, key[1], fieldName,
			),
		})
	}
	return findings
}

// includeFileWarnings returns load-time warnings for non-whitelisted top-level
// keys and nested includes in an included file. Includes carry a fixed whitelist:
// `projects`, `workbenches`, `[workbench]`, `[tasks.git]`, `[work]`,
// `[effort.<agent>]`, `[agents.<preset>]`, and `[repo."<path>"]`.
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
		"workbench":   true, // [workbench] options block (pick_on_create, order)
		"repo":        true,
		"tasks":       true, // only [tasks.git] is still read (ADR-0194)
		"work":        true, // [work.<kind>] Work groups (ADR-0194)
		"effort":      true,
		"agents":      true, // [agents.<preset>] per-preset settings (ADR-0187, ADR-0194)
		"includes":    true, // mentioned in includes, so we track it for warning above
	}

	// Check for non-whitelisted keys
	seen := make(map[string]bool)
	for key := range rawInclude {
		if !whitelisted[key] && !seen[key] {
			seen[key] = true
			warnings = append(warnings, fmt.Sprintf(
				"%s: %q ignored (includes only support projects, workbenches, workbench, repo, tasks, work, effort, and agents blocks)",
				path, key,
			))
		}
	}

	return warnings
}

// ExpandProjects resolves all project paths from the config
// Supports exact paths and glob patterns like ~/Dev/*/*
func (c *Config) ExpandProjects() ([]ExpandedPath, error) {
	return c.ExpandProjectsWith(defaultDeps)
}

// ExpandProjectsWith resolves all project paths using provided dependencies
func (c *Config) ExpandProjectsWith(d *Deps) ([]ExpandedPath, error) {
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
			matches, err := expandGlob(d, expanded)
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

// expandGlob expands a glob pattern into absolute paths.
func expandGlob(d *Deps, pattern string) ([]string, error) {
	base, pat := doublestar.SplitPattern(pattern)

	// Resolve symlinks in the base path once (e.g., ~/Dev -> /private/Dev)
	resolvedBase := base
	if r, err := d.FS.EvalSymlinks(base); err == nil {
		resolvedBase = r
	}

	fsys := d.FS.DirFS(base)
	matches, err := doublestar.Glob(fsys, pat, doublestar.WithNoHidden())
	if err != nil {
		return nil, err
	}

	// Convert to absolute paths using the resolved base
	var results []string
	for _, match := range matches {
		results = append(results, filepath.Join(resolvedBase, match))
	}
	return results, nil
}

func isDirectoryWith(d *Deps, path string) bool {
	info, err := d.FS.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}
