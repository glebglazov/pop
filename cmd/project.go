package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	runtimedebug "runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/debug"
	"github.com/glebglazov/pop/history"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/session"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
	"github.com/glebglazov/pop/ui"
	"github.com/spf13/cobra"
)

var tmuxCDPane string
var yankTarget string
var noHistory bool

var projectCmd = &cobra.Command{
	Use:   "project",
	Short: "Manage project picker commands",
	Long: `Manage project picker commands.

Use "pop project dashboard" to open the picker.`,
	// Deprecated compatibility path: use `pop project dashboard` instead.
	// TODO: remove the direct picker invocation at the next major CLI change.
	RunE: runProject,
}

var projectDashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Open the project picker",
	Long: `Opens the project picker to choose a project, worktree, or standalone session.
Projects with git worktrees are expanded to show individual worktrees.
Choosing a project opens or switches to a tmux session.

Example tmux binding:
  bind-key p display-popup -E -w 60% -h 60% 'pop project dashboard'`,
	RunE: runProject,
}

// Deprecated: use `pop project dashboard` instead. Hidden alias for existing
// keybindings. TODO: remove at next major release.
var selectCmd = &cobra.Command{
	Use:    "select",
	Short:  "Open the project picker (alias for project dashboard)",
	Hidden: true,
	RunE:   runProject,
}

func init() {
	rootCmd.AddCommand(projectCmd)
	rootCmd.AddCommand(selectCmd)
	projectCmd.AddCommand(projectDashboardCmd)
	projectCmd.PersistentFlags().StringVar(&tmuxCDPane, "tmux-cd", "", "Send cd command to specified tmux pane instead of switching session")
	projectCmd.PersistentFlags().StringVar(&yankTarget, "yank-target", "", "Send yanked path to specified tmux pane instead of system clipboard")
	projectCmd.PersistentFlags().BoolVar(&noHistory, "no-history", false, "Do not record selection in history")
	selectCmd.Flags().StringVar(&tmuxCDPane, "tmux-cd", "", "Send cd command to specified tmux pane instead of switching session")
	selectCmd.Flags().StringVar(&yankTarget, "yank-target", "", "Send yanked path to specified tmux pane instead of system clipboard")
	selectCmd.Flags().BoolVar(&noHistory, "no-history", false, "Do not record selection in history")
}

// ProjectDeps holds dependencies for the project command.
// See docs/rfc-select-deps.md for rationale.
type ProjectDeps struct {
	// Core dependencies
	Tmux    deps.Tmux
	Project *project.Deps

	// Data loading
	LoadConfig  func() (*config.Config, error)
	LoadHistory func() (*history.History, error)

	// ManagedWorktrees discovers pop-managed worktrees under ManagedWorktreesRoot
	// via a filesystem-only walk — no store open, no git fork (ADR-0110). A seam so
	// tests supply a fixed set (or none) without a real queue data dir.
	ManagedWorktrees func() []project.ExpandedProject

	// Picker — the critical testing seam
	RunPicker func(items []ui.Item, opts ...ui.PickerOption) (ui.Result, error)

	// Session state
	SessionActivity   func() map[string]int64
	AttentionSessions func() map[string]bool

	// Side effects (take deps.Tmux as first arg to match *With signatures)
	OpenSession func(tmux deps.Tmux, item *ui.Item) error
	// OpenSessionWithWorkbench creates a session that is exactly the named
	// Workbench (stray shell window removed) and attaches to it. Used by the
	// picker create-path when [workbench] pick_on_create is on (ADR-0075).
	OpenSessionWithWorkbench func(tmux deps.Tmux, item *ui.Item, workbenchName string) error
	OpenWindow               func(tmux deps.Tmux, item *ui.Item) error
	KillSession              func(tmux deps.Tmux, name string)
	SendCDToPane             func(tmux deps.Tmux, paneID, path string) error
	YankPathToPane           func(tmux deps.Tmux, paneID, path string) error
	SwitchToTarget           func(tmux deps.Tmux, target string) error
	SwitchAndZoom            func(tmux deps.Tmux, target string) error
	RunCustomCommand         func(command string, item *ui.Item)
	// EnsureSystemState synchronously runs integration checks and kicks off
	// the monitor daemon in a goroutine. Returns warnings for the picker.
	EnsureSystemState func() []string
	RunConfigure      func() error

	// UpdateNotice returns the dimmed top-right Update notice text, or "" for
	// none. It is a seam so tests never touch the real cache or network.
	UpdateNotice func() string

	// ResolveWorkbenches returns the Workbenches resolved for a project path,
	// used by the create-path prompt (ADR-0075). A seam so tests can supply a
	// fixed set without touching .pop.toml or the global library.
	ResolveWorkbenches func(cfg *config.Config, path string) []config.Workbench

	// ResolvePreferredWorkbench returns the preferred-Workbench name to
	// auto-apply for a checkout (or "" for none) plus any non-fatal warnings
	// (ADR-0078). When it resolves a name the create-path applies it silently and
	// suppresses the pick_on_create prompt. A seam so tests drive the auto-apply
	// vs prompt decision without config.
	ResolvePreferredWorkbench func(cfg *config.Config, path string) (string, []string)

	// Environment
	InTmux         func() bool
	CurrentSession func(tmux deps.Tmux) string

	// CLI flags (populated by cobra handler before calling RunProject)
	TMuxCDPane string
	YankTarget string
	NoHistory  bool
}

// DefaultProjectDeps returns ProjectDeps wired to real production implementations.
func DefaultProjectDeps() *ProjectDeps {
	return &ProjectDeps{
		Tmux:    defaultTmux,
		Project: project.DefaultDeps(),

		LoadConfig: func() (*config.Config, error) {
			cfgPath := cfgFile
			if cfgPath == "" {
				cfgPath = config.DefaultConfigPath()
			}
			return config.Load(cfgPath)
		},
		LoadHistory: func() (*history.History, error) {
			return history.Load(history.DefaultHistoryPath())
		},

		ManagedWorktrees: func() []project.ExpandedProject {
			td := tasks.DefaultDeps()
			return discoverManagedWorktreesWith(td.FS, binding.ManagedWorktreesRoot(td))
		},

		RunPicker: ui.Run,

		SessionActivity:   history.TmuxSessionActivity,
		AttentionSessions: monitorAttentionSessions,

		OpenSession:              openTmuxSessionWith,
		OpenSessionWithWorkbench: openTmuxSessionWithWorkbenchWith,
		OpenWindow:               openTmuxWindowWith,
		KillSession:              killTmuxSessionWith,
		SendCDToPane:             sendCDToPaneWith,
		YankPathToPane:           yankPathToPaneWith,
		SwitchToTarget:           switchToTmuxTargetWith,
		SwitchAndZoom:            switchToTmuxTargetAndZoomWith,
		RunCustomCommand:         executeProjectCustomCommand,
		EnsureSystemState:        ensureSystemState,
		RunConfigure: func() error {
			cd := defaultConfigureDeps()
			cd.ShowWelcome = true
			return runConfigureWith(cd)
		},

		UpdateNotice: pickerUpdateNotice,

		ResolveWorkbenches: func(cfg *config.Config, path string) []config.Workbench {
			templates, _ := cfg.ResolveWorkbenchesWith(config.DefaultDeps(), path)
			return templates
		},

		ResolvePreferredWorkbench: func(cfg *config.Config, path string) (string, []string) {
			return cfg.ResolvePreferredWorkbench(preferredResolverConfigDeps(cfg), path)
		},

		InTmux:         func() bool { return os.Getenv("TMUX") != "" },
		CurrentSession: currentTmuxSessionWith,
	}
}

func runProject(cmd *cobra.Command, args []string) error {
	d := DefaultProjectDeps()
	d.TMuxCDPane = tmuxCDPane
	d.YankTarget = yankTarget
	d.NoHistory = noHistory
	return RunProject(d)
}

// RunProject runs the project command with the given dependencies.
// It orchestrates config loading, project expansion, history sorting,
// the picker loop, and action dispatch.
func RunProject(d *ProjectDeps) error {
	// cfgPath is resolved only for the "no projects found" diagnostic message;
	// LoadConfig hides how the config is actually loaded.
	cfgPath := cfgFile
	if cfgPath == "" {
		cfgPath = config.DefaultConfigPath()
	}

	cfg, err := d.LoadConfig()
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("failed to load config: %w", err)
		}
		// Config doesn't exist — run interactive init
		if err := d.RunConfigure(); err != nil {
			return err
		}
		cfg, err = d.LoadConfig()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
	}

	systemWarnings := d.EnsureSystemState()

	// The projects list is essential to this command (ADR 0054): a blocking
	// finding on it leaves nothing to switch to, so the call site treats the
	// getter's error as fatal. Non-essential findings (display_depth, a bad
	// glob) are not surfaced here — they degrade to the warning banner below.
	if _, err := cfg.ProjectEntries(); err != nil {
		return fmt.Errorf("invalid projects configuration: %w", err)
	}

	// Expand project paths
	paths, err := cfg.ExpandProjects()
	if err != nil {
		return fmt.Errorf("failed to expand projects: %w", err)
	}

	if len(paths) == 0 {
		return fmt.Errorf("no projects found. Check your config at %s", cfgPath)
	}

	// Discover pop-managed worktrees concurrently with the configured-project
	// expansion (ADR-0110). The walk is filesystem-only — no store, no git — so
	// it can't slow expansion or fork; a nil seam simply contributes nothing.
	managedCh := make(chan []project.ExpandedProject, 1)
	go func() {
		if d.ManagedWorktrees == nil {
			managedCh <- nil
			return
		}
		managedCh <- d.ManagedWorktrees()
	}()

	// Expand projects, showing worktrees for bare repos (parallel).
	// Per-project errors and panics are captured so one bad project can't
	// crash the whole project flow.
	expanded, expansionErrors := expandProjectsWith(d.Project, paths)

	// Fold in the managed worktrees; they sort by History recency alongside
	// configured entries and dedupe against live sessions like any other entry.
	expanded = append(expanded, (<-managedCh)...)

	// Get current tmux session name for optional exclusion
	var excludedSessionNames map[string]bool
	if cfg.ShouldExcludeCurrentSession() {
		if currentSession := d.CurrentSession(d.Tmux); currentSession != "" {
			excludedSessionNames = map[string]bool{currentSession: true}
		}
	}
	if len(excludedSessionNames) > 0 {
		filtered := expanded[:0]
		for _, ep := range expanded {
			if !excludedSessionNames[ep.SessionName] {
				filtered = append(filtered, ep)
			}
		}
		expanded = filtered
	}

	// If every single project failed to expand, we can't start normal
	// handling — surface the failure instead of showing an empty picker.
	if len(expanded) == 0 && len(expansionErrors) > 0 {
		return fmt.Errorf("failed to expand any projects: %d errors (see ~/.local/share/pop/pop.log for details)", len(expansionErrors))
	}

	// Disambiguate projects with the same name
	project.DisambiguateNames(expanded, cfg.GetDisambiguationStrategy())

	// Load history and sort by recency (oldest first, most recent last)
	hist, err := d.LoadHistory()
	if err != nil {
		hist = &history.History{}
	}

	// Convert to Project for sorting, then back
	projects := make([]project.Project, len(expanded))
	for i, ep := range expanded {
		projects[i] = project.Project{Name: ep.Name, Path: ep.Path}
	}
	projects = hist.SortByRecency(projects)

	// Rebuild expanded list in sorted order
	pathToExpanded := make(map[string]project.ExpandedProject)
	for _, ep := range expanded {
		pathToExpanded[ep.Path] = ep
	}
	sortedExpanded := make([]project.ExpandedProject, len(projects))
	for i, p := range projects {
		sortedExpanded[i] = pathToExpanded[p.Path]
	}

	// Build base items (no icons, no sessions) — done once
	baseItems := make([]ui.Item, len(sortedExpanded))
	for i, ep := range sortedExpanded {
		baseItems[i] = ui.Item{
			Name:        ep.Name,
			Path:        ep.Path,
			Context:     ep.ProjectName,
			SessionName: ep.SessionName,
		}
	}

	// Load custom commands for project picker mode
	var customCommands []ui.UserDefinedCommand
	for _, cc := range cfg.CommandsForMode("project") {
		customCommands = append(customCommands, ui.UserDefinedCommand{
			Key:     cc.Key,
			Label:   cc.Label,
			Command: cc.Command,
			Exit:    cc.Exit,
		})
	}

	// Compute the Update notice once: it surfaces at most once per calendar day,
	// so a single computation up front stamps shown-at and keeps the badge
	// stable across picker-loop iterations.
	// The kill switch ([updates] notice_enabled = false) suppresses the badge
	// and, by skipping the call entirely, prevents the background Update fetch
	// PickerNotice would otherwise schedule.
	var updateNotice string
	if d.UpdateNotice != nil && cfg.UpdateNoticeEnabled() {
		updateNotice = d.UpdateNotice()
	}

	// Run picker loop
	inTmux := d.InTmux()
	restoreCursorIdx := -1
	for {
		// Refresh session state each iteration
		var attention map[string]bool
		if cfg.UnreadNotificationsEnabled("project") {
			attention = d.AttentionSessions()
		}
		items := buildSessionAwareItemsWith(baseItems, hist, d.SessionActivity(), excludedSessionNames, attention)

		quickAccessModifier := cfg.GetQuickAccessModifier()
		iconLegends := []ui.IconLegend{
			{Icon: iconDirSession, Desc: "Directory with tmux session"},
			{Icon: iconStandaloneSession, Desc: "Standalone tmux session"},
		}
		if cfg.UnreadNotificationsEnabled("project") {
			iconLegends = append(iconLegends, ui.IconLegend{Icon: iconAttention, Desc: "Agent has unread output"})
		}
		opts := []ui.PickerOption{
			ui.WithCursorAtEnd(),
			ui.WithKillSession(),
			ui.WithReset(),
			ui.WithSetPreferredWorkbench(),
			ui.WithQuickAccess(quickAccessModifier),
			ui.WithIconLegend(iconLegends...),
		}
		if inTmux {
			opts = append(opts, ui.WithOpenWindow())
		}
		if len(customCommands) > 0 {
			opts = append(opts, ui.WithUserDefinedCommands(customCommands))
		}
		warnings := cfg.Warnings
		if len(expansionErrors) > 0 {
			warnings = append(warnings, fmt.Sprintf("%d project(s) failed to expand: %s (see pop.log)", len(expansionErrors), strings.Join(expansionErrors, ", ")))
		}
		warnings = append(warnings, systemWarnings...)
		if len(warnings) > 0 {
			opts = append(opts, ui.WithWarnings(warnings))
		}
		if restoreCursorIdx >= 0 {
			opts = append(opts, ui.WithInitialCursorIndex(restoreCursorIdx))
			restoreCursorIdx = -1
		}
		if updateNotice != "" {
			opts = append(opts, ui.WithUpdateNotice(updateNotice))
		}
		result, err := d.RunPicker(items, opts...)
		if err != nil {
			return err
		}

		switch result.Action {
		case ui.ActionCancel:
			return nil

		case ui.ActionConfirm:
			if result.Selected == nil {
				return nil
			}
			if isStandaloneSession(*result.Selected) {
				return d.SwitchToTarget(d.Tmux, standaloneSessionName(*result.Selected))
			}
			if !d.NoHistory {
				hist.Record(result.Selected.Path)
				if err := hist.Save(); err != nil {
					debug.Error("project: save history: %v", err)
				}
			}
			if d.TMuxCDPane != "" {
				return d.SendCDToPane(d.Tmux, d.TMuxCDPane, result.Selected.Path)
			}
			// Preferred workbench (ADR-0078): a resolved per-checkout default
			// auto-applies silently and suppresses the prompt regardless of
			// pick_on_create. A stale name resolves to "" with a warning and
			// falls through to today's behavior. Fires only when this selection
			// creates a brand-new session.
			if !d.Tmux.HasSession(result.Selected.SessionName) {
				preferred, warns := d.ResolvePreferredWorkbench(cfg, result.Selected.Path)
				for _, w := range warns {
					debug.Error("project: %s", w)
				}
				if preferred != "" {
					return d.OpenSessionWithWorkbench(d.Tmux, result.Selected, preferred)
				}
			}
			// Picker-time Workbench selection (ADR-0075), opt-in via
			// [workbench] pick_on_create. Fires only when this selection
			// creates a brand-new session and at least one Workbench resolves
			// for the project path; otherwise the create-path is unchanged.
			if cfg.WorkbenchPickOnCreate() && !d.Tmux.HasSession(result.Selected.SessionName) {
				workbenches := d.ResolveWorkbenches(cfg, result.Selected.Path)
				if len(workbenches) > 0 {
					name, confirmed, err := promptWorkbenchForCreate(d, cfg.WorkbenchOrder(), workbenches)
					if err != nil {
						return err
					}
					if !confirmed {
						// Esc in the Workbench list: nothing created, return
						// to the project picker with the cursor preserved.
						restoreCursorIdx = result.CursorIndex
						continue
					}
					if name != "" {
						return d.OpenSessionWithWorkbench(d.Tmux, result.Selected, name)
					}
					// "no workbench": fall through to today's flat session.
				}
			}
			return d.OpenSession(d.Tmux, result.Selected)

		case ui.ActionOpenWindow:
			if result.Selected == nil || isStandaloneSession(*result.Selected) {
				continue
			}
			if !d.NoHistory {
				hist.Record(result.Selected.Path)
				if err := hist.Save(); err != nil {
					debug.Error("project: save history: %v", err)
				}
			}
			return d.OpenWindow(d.Tmux, result.Selected)

		case ui.ActionYankPath:
			if result.Selected == nil {
				return nil
			}
			paneID := d.YankTarget
			if paneID == "" {
				paneID = os.Getenv("TMUX_PANE")
			}
			if paneID == "" {
				return fmt.Errorf("yank target pane not set — pass --yank-target or run inside tmux")
			}
			return d.YankPathToPane(d.Tmux, paneID, result.Selected.Path)

		case ui.ActionKillSession:
			if result.Selected != nil {
				restoreCursorIdx = result.CursorIndex
				if isStandaloneSession(*result.Selected) {
					d.KillSession(d.Tmux, standaloneSessionName(*result.Selected))
				} else {
					d.KillSession(d.Tmux, result.Selected.SessionName)
				}
			}
			// Continue loop — session state refreshes automatically

		case ui.ActionReset:
			if result.Selected != nil && !isStandaloneSession(*result.Selected) {
				hist.Remove(result.Selected.Path)
				if err := hist.Save(); err != nil {
					debug.Error("project: save history: %v", err)
				}
				baseItems = sortBaseItemsByHistory(baseItems, hist)
			}
			// No-op for standalone sessions; continue loop

		case ui.ActionRefresh:
			restoreCursorIdx = result.CursorIndex
			// Continue loop — items rebuild with fresh attention state

		case ui.ActionSetPreferredWorkbench:
			// Sets the per-checkout Preferred workbench (ADR-0078); never touches
			// a running session. Skip standalone sessions (no real checkout).
			if result.Selected != nil && !isStandaloneSession(*result.Selected) {
				warnPreferredWorkbenchErr("project", setPreferredWorkbench(defaultPreferredPickerDeps(), result.Selected.Path))
			}
			restoreCursorIdx = result.CursorIndex
			// Continue loop — preference set, session state unchanged.

		case ui.ActionUserDefinedCommand:
			if result.UserDefinedCommand != nil && result.Selected != nil {
				d.RunCustomCommand(result.UserDefinedCommand.Command, result.Selected)
				if result.UserDefinedCommand.Exit {
					return nil
				}
			}
		}
	}
}

func sortBaseItemsByHistory(items []ui.Item, hist *history.History) []ui.Item {
	projects := make([]project.Project, len(items))
	for i, item := range items {
		projects[i] = project.Project{Name: item.Name, Path: item.Path}
	}
	projects = hist.SortByRecency(projects)
	pathToItem := make(map[string]ui.Item, len(items))
	for _, item := range items {
		pathToItem[item.Path] = item
	}
	sorted := make([]ui.Item, len(projects))
	for i, p := range projects {
		sorted[i] = pathToItem[p.Path]
	}
	return sorted
}

func buildSessionAwareItems(baseItems []ui.Item, hist *history.History, excludedSessionNames map[string]bool, monitorEnabled bool) []ui.Item {
	var attentionSessions map[string]bool
	if monitorEnabled {
		attentionSessions = monitorAttentionSessions()
	}
	return buildSessionAwareItemsWith(baseItems, hist, history.TmuxSessionActivity(), excludedSessionNames, attentionSessions)
}

func buildSessionAwareItemsWith(baseItems []ui.Item, hist *history.History, sessionActivity map[string]int64, excludedSessionNames map[string]bool, attentionSessions map[string]bool) []ui.Item {
	// Build set of session names that correspond to project items
	projectSessionNames := make(map[string]bool)
	for _, item := range baseItems {
		projectSessionNames[item.SessionName] = true
	}

	// Apply icons to project items that have active sessions
	items := make([]ui.Item, len(baseItems))
	copy(items, baseItems)
	for i := range items {
		if _, hasSession := sessionActivity[items[i].SessionName]; hasSession {
			items[i].Icon = iconDirSession
		} else {
			items[i].Icon = ""
		}
	}

	// Override icons for sessions needing attention
	if attentionSessions != nil {
		for i := range items {
			if attentionSessions[items[i].SessionName] {
				items[i].Icon = iconAttention
			}
		}
	}

	// Add standalone sessions (not matching any project or excluded project)
	for sessionName := range sessionActivity {
		if !projectSessionNames[sessionName] && !excludedSessionNames[sessionName] {
			icon := iconStandaloneSession
			if attentionSessions != nil && attentionSessions[sessionName] {
				icon = iconAttention
			}
			items = append(items, ui.Item{
				Name: sessionName,
				Path: tmuxSessionPathPrefix + sessionName,
				Icon: icon,
			})
		}
	}

	// Sort by unified timeline
	return sortByUnifiedRecency(items, hist, sessionActivity)
}

func sortByUnifiedRecency(items []ui.Item, hist *history.History, sessionActivity map[string]int64) []ui.Item {
	historyTimes := make(map[string]time.Time)
	for _, e := range hist.Entries {
		historyTimes[e.Path] = e.LastAccess
	}

	getAccessTime := func(item ui.Item) (time.Time, bool) {
		if t, ok := historyTimes[item.Path]; ok {
			return t, true
		}
		if isStandaloneSession(item) {
			if ts, ok := sessionActivity[standaloneSessionName(item)]; ok {
				return time.Unix(ts, 0), true
			}
		}
		return time.Time{}, false
	}

	sorted := make([]ui.Item, len(items))
	copy(sorted, items)

	sort.SliceStable(sorted, func(i, j int) bool {
		ti, oki := getAccessTime(sorted[i])
		tj, okj := getAccessTime(sorted[j])

		if oki && okj {
			return ti.Before(tj)
		}
		if oki {
			return false
		}
		if okj {
			return true
		}
		return sorted[i].Name < sorted[j].Name
	})

	return sorted
}

func openTmuxSession(item *ui.Item) error {
	return openTmuxSessionWith(defaultTmux, item)
}

func openTmuxSessionWith(tmux deps.Tmux, item *ui.Item) error {
	return session.AttachWith(&session.Deps{
		Tmux:   tmux,
		InTmux: func() bool { return os.Getenv("TMUX") != "" },
	}, item.SessionName, item.Path)
}

// noWorkbenchLabel is the "<empty>" no-workbench entry in the create-path
// Workbench prompt; choosing it creates today's flat session unchanged
// (ADR-0075). It leads the default order but [workbench] order can move it. It is
// distinguished from real Workbenches by its empty Item.Path sentinel, so a
// Workbench that happens to share this display name is still picked correctly.
const noWorkbenchLabel = "<empty>"

// workbenchItemPathPrefix prefixes the Item.Path of each resolved Workbench in
// the create-path prompt so every entry has a unique, non-empty picker key,
// reserving the empty path for the no-workbench sentinel.
const workbenchItemPathPrefix = "workbench:"

// workbenchOption is one candidate row in an interactive Workbench list: its
// on-screen Label (which is also its [workbench] order token) and the ui.Item
// rendered for it. The special "<empty>"/"<reset>" options carry their own
// labels here alongside real Workbenches.
type workbenchOption struct {
	Label string
	Item  ui.Item
}

// orderWorkbenchOptions arranges the candidate options of an interactive
// Workbench list per the [workbench] order rule (task 03). Both interactive
// lists — the pick_on_create create prompt and the Preferred-workbench picker —
// route through here so they order identically for the same inputs:
//
//  1. Options whose Label is named in order front-load, in the sequence they
//     appear in order.
//  2. Every unnamed option follows in the order candidates were given, which
//     callers build as the default: "<empty>", Workbenches in resolution order,
//     "<reset>".
//  3. An order token matching no candidate Label is ignored (same tolerance as a
//     stale Preferred workbench name).
func orderWorkbenchOptions(order []string, candidates []workbenchOption) []ui.Item {
	used := make([]bool, len(candidates))
	items := make([]ui.Item, 0, len(candidates))
	for _, token := range order {
		for i := range candidates {
			if !used[i] && candidates[i].Label == token {
				used[i] = true
				items = append(items, candidates[i].Item)
				break
			}
		}
	}
	for i := range candidates {
		if !used[i] {
			items = append(items, candidates[i].Item)
		}
	}
	return items
}

// promptWorkbenchForCreate shows a quick-search list of resolved Workbenches plus
// the "<empty>" no-workbench option (ADR-0075), ordered by [workbench] order via
// orderWorkbenchOptions (task 03). It returns the chosen Workbench name ("" for
// the no-workbench entry) and whether the user confirmed a choice (false when
// Esc was pressed, i.e. return to the project picker, create nothing).
func promptWorkbenchForCreate(d *ProjectDeps, order []string, workbenches []config.Workbench) (name string, confirmed bool, err error) {
	candidates := make([]workbenchOption, 0, len(workbenches)+1)
	candidates = append(candidates, workbenchOption{Label: noWorkbenchLabel, Item: ui.Item{Name: noWorkbenchLabel}})
	for _, wb := range workbenches {
		candidates = append(candidates, workbenchOption{Label: wb.Name, Item: ui.Item{Name: wb.Name, Path: workbenchItemPathPrefix + wb.Name}})
	}
	items := orderWorkbenchOptions(order, candidates)

	result, err := d.RunPicker(items, ui.WithInitialCursorIndex(0),
		ui.WithHeader("Pick a workbench to start a session"))
	if err != nil {
		return "", false, err
	}
	if result.Action != ui.ActionConfirm || result.Selected == nil {
		return "", false, nil
	}
	if result.Selected.Path == "" {
		// The no-workbench sentinel → today's flat session.
		return "", true, nil
	}
	return result.Selected.Name, true, nil
}

// openTmuxSessionWithWorkbenchWith creates a brand-new tmux session that is
// exactly the named Workbench (stray shell window removed) and attaches to it.
// It is the production implementation of ProjectDeps.OpenSessionWithWorkbench
// (ADR-0075 picker create-path).
func openTmuxSessionWithWorkbenchWith(tmux deps.Tmux, item *ui.Item, workbenchName string) error {
	td := defaultTemplateRuntimeDeps()
	td.Tmux = tmux
	cfg, err := td.LoadConfig()
	if err != nil {
		return err
	}
	templates, warnings := cfg.ResolveWorkbenchesWith(td.ConfigDeps, item.Path)
	for _, w := range warnings {
		warnf(td, "%s\n", w)
	}
	tmpl, ok := findWorkbench(templates, workbenchName)
	if !ok {
		return fmt.Errorf("workbench %q not found", workbenchName)
	}
	if err := createSessionFromWorkbench(td, tmpl, item.SessionName, item.Path); err != nil {
		return err
	}
	return switchToTmuxTargetWith(tmux, item.SessionName)
}

func openTmuxWindow(item *ui.Item) error {
	return openTmuxWindowWith(defaultTmux, item)
}

func openTmuxWindowWith(tmux deps.Tmux, item *ui.Item) error {
	windowName := sanitizeSessionName(item.Name)

	session, err := tmux.Command("display-message", "-p", "#S")
	if err != nil {
		return fmt.Errorf("failed to get current tmux session: %w", err)
	}

	listOut, err := tmux.Command("list-windows", "-t", session, "-F", "#{window_name}")
	if err != nil {
		return fmt.Errorf("failed to list tmux windows: %w", err)
	}

	for _, name := range strings.Split(listOut, "\n") {
		if name == windowName {
			_, err := tmux.Command("select-window", "-t", session+":"+windowName)
			return err
		}
	}

	_, err = tmux.Command("new-window", "-t", session, "-n", windowName, "-c", item.Path)
	return err
}

func sanitizeSessionName(name string) string {
	// Replace dots and colons with underscores for tmux compatibility
	name = strings.ReplaceAll(name, ".", "_")
	name = strings.ReplaceAll(name, ":", "_")
	return name
}

func killTmuxSession(name string) {
	killTmuxSessionWith(defaultTmux, name)
}

func killTmuxSessionWith(tmux deps.Tmux, name string) {
	sessionName := sanitizeSessionName(name)
	_, err := tmux.Command("kill-session", "-t", sessionName)
	if err != nil {
		debug.Error("killTmuxSession %s: %v", sessionName, err)
		fmt.Fprintf(os.Stderr, "Failed to kill session: %s\n", sessionName)
	} else {
		fmt.Fprintf(os.Stderr, "Killed session: %s\n", sessionName)
	}
}

func executeProjectCustomCommand(command string, item *ui.Item) {
	cmd := exec.Command("sh", "-c", command)
	cmd.Env = append(os.Environ(),
		"POP_PATH="+item.Path,
		"POP_NAME="+item.Name,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		debug.Error("project: custom command %q: %v", command, err)
		fmt.Fprintf(os.Stderr, "Custom command failed: %v\n", err)
	}
}

func sendCDToPane(paneID, path string) error {
	return sendCDToPaneWith(defaultTmux, paneID, path)
}

func sendCDToPaneWith(tmux deps.Tmux, paneID, path string) error {
	_, err := tmux.Command("send-keys", "-t", paneID, fmt.Sprintf("cd %q && clear", path), "Enter")
	return err
}

func yankPathToPaneWith(tmux deps.Tmux, paneID, path string) error {
	_, err := tmux.Command("send-keys", "-t", paneID, path)
	return err
}

// discoverManagedWorktreesWith walks the pop-managed worktrees root with
// filesystem calls only — it opens no store and forks no git (ADR-0110), so
// project expansion stays store- and fork-free. Layout is
// <root>/<repoKey>/<worktreeName>, where repoKey is <basename>-<shortHash>.
// Each worktree becomes a flat ExpandedProject whose display name and session
// name are both <basename>/<worktreeName>; the session name matches the drain's
// own naming so a live drain session dedupes rather than duplicating. A missing
// root (no managed worktrees yet, or ReadDir error) yields no entries.
func discoverManagedWorktreesWith(fs deps.FileSystem, root string) []project.ExpandedProject {
	repoEntries, err := fs.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []project.ExpandedProject
	for _, repoEntry := range repoEntries {
		if !repoEntry.IsDir() {
			continue
		}
		repoKey := repoEntry.Name()
		basename := repoKeyBasename(repoKey)
		repoPath := filepath.Join(root, repoKey)
		wtEntries, err := fs.ReadDir(repoPath)
		if err != nil {
			continue
		}
		for _, wtEntry := range wtEntries {
			if !wtEntry.IsDir() {
				continue
			}
			name := basename + "/" + wtEntry.Name()
			out = append(out, project.ExpandedProject{
				Name:        name,
				Path:        filepath.Join(repoPath, wtEntry.Name()),
				ProjectName: basename,
				IsWorktree:  true,
				SessionName: sanitizeSessionName(name),
			})
		}
	}
	return out
}

// repoKeyBasename strips the trailing "-<shortHash>" from a managed-worktree
// repoKey to recover the human-readable repository basename. The short hash is
// exactly tasks.ShortHashLen hex characters; only a suffix matching that shape is
// stripped, so a basename that itself contains dashes survives intact.
func repoKeyBasename(repoKey string) string {
	i := strings.LastIndex(repoKey, "-")
	if i < 0 {
		return repoKey
	}
	suffix := repoKey[i+1:]
	if len(suffix) != tasks.ShortHashLen || !isHexString(suffix) {
		return repoKey
	}
	return repoKey[:i]
}

func isHexString(s string) bool {
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

// expandProjects runs expandProjectsWith using the default project dependencies.
func expandProjects(paths []config.ExpandedPath) ([]project.ExpandedProject, []string) {
	return expandProjectsWith(project.DefaultDeps(), paths)
}

// expandProjectsWith expands each configured path into one or more ExpandedProjects
// in parallel. Bare repos with worktrees are expanded to individual worktrees;
// regular directories become a single entry. The returned slice preserves the
// input order. failedNames contains filepath.Base of any paths whose expansion
// errored or panicked — expansion of other paths continues in both cases.
func expandProjectsWith(d *project.Deps, paths []config.ExpandedPath) (expanded []project.ExpandedProject, failedNames []string) {
	type expandResult struct {
		index    int
		path     string
		projects []project.ExpandedProject
		err      error
	}

	results := make(chan expandResult, len(paths))
	var wg sync.WaitGroup

	for i, p := range paths {
		wg.Add(1)
		go func(idx int, ep config.ExpandedPath) {
			defer wg.Done()

			var (
				projects  []project.ExpandedProject
				expandErr error
			)

			// Recover from panics inside the goroutine so one bad project
			// can't crash the whole process. The panic becomes an error
			// on the result channel and flows through the existing error
			// handling below.
			defer func() {
				if r := recover(); r != nil {
					expandErr = fmt.Errorf("panic expanding %s: %v", ep.Path, r)
					debug.Error("expandProjects: panic on %q: %v\n%s", ep.Path, r, runtimedebug.Stack())
				}
				results <- expandResult{index: idx, path: ep.Path, projects: projects, err: expandErr}
			}()

			displayName := ui.LastNSegments(ep.Path, ep.DisplayDepth)
			projectName := filepath.Base(ep.Path)

			if project.HasWorktreesWith(d, ep.Path) {
				// Bare repo with worktrees - expand to individual worktrees
				worktrees, err := project.ListWorktreesForPathWith(d, ep.Path)
				if err != nil {
					expandErr = err
					return
				}
				ctx := &project.RepoContext{RepoName: projectName, IsBare: true}
				for _, wt := range worktrees {
					projects = append(projects, project.ExpandedProject{
						Name:        displayName + "/" + wt.Name,
						Path:        wt.Path,
						ProjectName: projectName,
						IsWorktree:  true,
						SessionName: project.TmuxSessionName(ctx, wt.Name),
					})
				}
			} else {
				// Regular project
				projects = append(projects, project.ExpandedProject{
					Name:        displayName,
					Path:        ep.Path,
					ProjectName: projectName,
					IsWorktree:  false,
					SessionName: project.TmuxSessionName(&project.RepoContext{IsBare: false}, filepath.Base(ep.Path)),
				})
			}
		}(i, p)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results maintaining original order
	resultsByIndex := make(map[int][]project.ExpandedProject, len(paths))
	for r := range results {
		resultsByIndex[r.index] = r.projects
		if r.err != nil {
			debug.Error("expandProjects: %q: %v", r.path, r.err)
			failedNames = append(failedNames, filepath.Base(r.path))
		}
	}

	// Flatten in original order
	for i := range paths {
		expanded = append(expanded, resultsByIndex[i]...)
	}

	return expanded, failedNames
}
