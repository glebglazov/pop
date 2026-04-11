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
	"github.com/glebglazov/pop/ui"
	"github.com/spf13/cobra"
)

var tmuxCDPane string
var noHistory bool

var selectCmd = &cobra.Command{
	Use:   "select",
	Short: "Select a project from configured directories",
	Long: `Opens a fuzzy picker to select a project.
Projects with git worktrees are expanded to show individual worktrees.
Selecting a project opens or switches to a tmux session.

Example tmux binding:
  bind-key p display-popup -E -w 60% -h 60% 'pop select'`,
	RunE: runSelect,
}

func init() {
	rootCmd.AddCommand(selectCmd)
	selectCmd.Flags().StringVar(&tmuxCDPane, "tmux-cd", "", "Send cd command to specified tmux pane instead of switching session")
	selectCmd.Flags().BoolVar(&noHistory, "no-history", false, "Do not record selection in history")
}

// SelectDeps holds dependencies for the select command.
// See docs/rfc-select-deps.md for rationale.
type SelectDeps struct {
	// Core dependencies
	Tmux    deps.Tmux
	Project *project.Deps

	// Data loading
	LoadConfig  func() (*config.Config, error)
	LoadHistory func() (*history.History, error)

	// Picker — the critical testing seam
	RunPicker func(items []ui.Item, opts ...ui.PickerOption) (ui.Result, error)

	// Session state
	SessionActivity    func() map[string]int64
	AttentionSessions  func() map[string]bool
	AttentionPanes     func() []ui.AttentionPane
	AttentionCallbacks func() ui.AttentionCallbacks

	// Side effects (take deps.Tmux as first arg to match *With signatures)
	OpenSession      func(tmux deps.Tmux, item *ui.Item) error
	OpenWindow       func(tmux deps.Tmux, item *ui.Item) error
	KillSession      func(tmux deps.Tmux, name string)
	SendCDToPane     func(tmux deps.Tmux, paneID, path string) error
	SwitchToTarget   func(tmux deps.Tmux, target string) error
	SwitchAndZoom    func(tmux deps.Tmux, target string) error
	RunCustomCommand func(command string, item *ui.Item)
	// EnsureSystemState synchronously runs integration checks and kicks off
	// the monitor daemon in a goroutine. Returns warnings for the picker.
	EnsureSystemState func() []string
	RunConfigure      func() error

	// Environment
	InTmux         func() bool
	CurrentSession func(tmux deps.Tmux) string

	// CLI flags (populated by cobra handler before calling RunSelect)
	TMuxCDPane string
	NoHistory  bool
}

// DefaultSelectDeps returns SelectDeps wired to real production implementations.
func DefaultSelectDeps() *SelectDeps {
	return &SelectDeps{
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

		RunPicker: ui.Run,

		SessionActivity:    history.TmuxSessionActivity,
		AttentionSessions:  monitorAttentionSessions,
		AttentionPanes:     buildAttentionPanes,
		AttentionCallbacks: attentionCallbacks,

		OpenSession:       openTmuxSessionWith,
		OpenWindow:        openTmuxWindowWith,
		KillSession:       killTmuxSessionWith,
		SendCDToPane:      sendCDToPaneWith,
		SwitchToTarget:    switchToTmuxTargetWith,
		SwitchAndZoom:     switchToTmuxTargetAndZoomWith,
		RunCustomCommand:  executeSelectCustomCommand,
		EnsureSystemState: ensureSystemState,
		RunConfigure: func() error {
			cd := defaultConfigureDeps()
			cd.ShowWelcome = true
			return runConfigureWith(cd)
		},

		InTmux:         func() bool { return os.Getenv("TMUX") != "" },
		CurrentSession: currentTmuxSessionWith,
	}
}

func runSelect(cmd *cobra.Command, args []string) error {
	d := DefaultSelectDeps()
	d.TMuxCDPane = tmuxCDPane
	d.NoHistory = noHistory
	return RunSelect(d)
}

// RunSelect runs the select command with the given dependencies.
// It orchestrates config loading, project expansion, history sorting,
// the picker loop, and action dispatch.
func RunSelect(d *SelectDeps) error {
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

	// Expand project paths
	paths, err := cfg.ExpandProjects()
	if err != nil {
		return fmt.Errorf("failed to expand projects: %w", err)
	}

	if len(paths) == 0 {
		return fmt.Errorf("no projects found. Check your config at %s", cfgPath)
	}

	// Expand projects, showing worktrees for bare repos (parallel).
	// Per-project errors and panics are captured so one bad project can't
	// crash the whole select flow.
	expanded, expansionErrors := expandProjectsWith(d.Project, paths)

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
			if !excludedSessionNames[sanitizeSessionName(ep.Name)] {
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
			Name:    ep.Name,
			Path:    ep.Path,
			Context: ep.ProjectName,
		}
	}

	// Load custom commands for select mode
	var customCommands []ui.UserDefinedCommand
	for _, cc := range cfg.CommandsForMode("select") {
		customCommands = append(customCommands, ui.UserDefinedCommand{
			Key:     cc.Key,
			Label:   cc.Label,
			Command: cc.Command,
			Exit:    cc.Exit,
		})
	}

	// Run picker loop
	inTmux := d.InTmux()
	restoreCursorIdx := -1
	for {
		// Refresh session state each iteration
		var attention map[string]bool
		if cfg.UnreadNotificationsEnabled("select") {
			attention = d.AttentionSessions()
		}
		items := buildSessionAwareItemsWith(baseItems, hist, d.SessionActivity(), excludedSessionNames, attention)

		quickAccessModifier := cfg.GetQuickAccessModifier()
		iconLegends := []ui.IconLegend{
			{Icon: iconDirSession, Desc: "Directory with tmux session"},
			{Icon: iconStandaloneSession, Desc: "Standalone tmux session"},
		}
		if cfg.UnreadNotificationsEnabled("select") {
			iconLegends = append(iconLegends, ui.IconLegend{Icon: iconAttention, Desc: "Agent has unread output"})
		}
		opts := []ui.PickerOption{
			ui.WithCursorAtEnd(),
			ui.WithKillSession(),
			ui.WithReset(),
			ui.WithQuickAccess(quickAccessModifier),
			ui.WithIconLegend(iconLegends...),
		}
		if cfg.UnreadNotificationsEnabled("select") {
			if attentionPanes := d.AttentionPanes(); len(attentionPanes) > 0 {
				opts = append(opts, ui.WithAttentionPanes(attentionPanes, d.AttentionCallbacks()))
			}
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
		result, err := d.RunPicker(items, opts...)
		if err != nil {
			return err
		}

		switch result.Action {
		case ui.ActionCancel:
			return nil

		case ui.ActionSelect:
			if result.Selected == nil {
				return nil
			}
			if isStandaloneSession(*result.Selected) {
				return d.SwitchToTarget(d.Tmux, standaloneSessionName(*result.Selected))
			}
			if !d.NoHistory {
				hist.Record(result.Selected.Path)
				if err := hist.Save(); err != nil {
					debug.Error("select: save history: %v", err)
				}
			}
			if d.TMuxCDPane != "" {
				return d.SendCDToPane(d.Tmux, d.TMuxCDPane, result.Selected.Path)
			}
			return d.OpenSession(d.Tmux, result.Selected)

		case ui.ActionOpenWindow:
			if result.Selected == nil || isStandaloneSession(*result.Selected) {
				continue
			}
			if !d.NoHistory {
				hist.Record(result.Selected.Path)
				if err := hist.Save(); err != nil {
					debug.Error("select: save history: %v", err)
				}
			}
			return d.OpenWindow(d.Tmux, result.Selected)

		case ui.ActionKillSession:
			if result.Selected != nil {
				restoreCursorIdx = result.CursorIndex
				if isStandaloneSession(*result.Selected) {
					d.KillSession(d.Tmux, standaloneSessionName(*result.Selected))
				} else {
					d.KillSession(d.Tmux, result.Selected.Name)
				}
			}
			// Continue loop — session state refreshes automatically

		case ui.ActionReset:
			if result.Selected != nil && !isStandaloneSession(*result.Selected) {
				hist.Remove(result.Selected.Path)
				if err := hist.Save(); err != nil {
					debug.Error("select: save history: %v", err)
				}
				baseItems = sortBaseItemsByHistory(baseItems, hist)
			}
			// No-op for standalone sessions; continue loop

		case ui.ActionSwitchToPane:
			if result.Selected != nil {
				if !d.NoHistory {
					sessionName := result.Selected.Context
					var histPath string
					for _, item := range items {
						if sanitizeSessionName(item.Name) == sessionName {
							histPath = item.Path
							break
						}
					}
					if histPath == "" {
						histPath = sessionHistoryPath(sessionName, hist)
					}
					hist.Record(histPath)
					if err := hist.Save(); err != nil {
						debug.Error("select: save history: %v", err)
					}
				}
				return d.SwitchAndZoom(d.Tmux, result.Selected.Path)
			}

		case ui.ActionRefresh:
			restoreCursorIdx = result.CursorIndex
			// Continue loop — items rebuild with fresh attention state

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
		sanitized := sanitizeSessionName(item.Name)
		projectSessionNames[sanitized] = true
	}

	// Apply icons to project items that have active sessions
	items := make([]ui.Item, len(baseItems))
	copy(items, baseItems)
	for i := range items {
		sanitized := sanitizeSessionName(items[i].Name)
		if _, hasSession := sessionActivity[sanitized]; hasSession {
			items[i].Icon = iconDirSession
		} else {
			items[i].Icon = ""
		}
	}

	// Override icons for sessions needing attention
	if attentionSessions != nil {
		for i := range items {
			sanitized := sanitizeSessionName(items[i].Name)
			if attentionSessions[sanitized] {
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
	sessionName := sanitizeSessionName(item.Name)
	inTmux := os.Getenv("TMUX") != ""

	_, err := tmux.Command("has-session", "-t="+sessionName)
	sessionExists := err == nil

	if !sessionExists {
		if _, err := tmux.Command("new-session", "-ds", sessionName, "-c", item.Path); err != nil {
			return fmt.Errorf("failed to create tmux session: %w", err)
		}
	}

	if inTmux {
		_, err := tmux.Command("switch-client", "-t", sessionName)
		return err
	}
	// attach-session needs stdio wired — cannot go through the generic Command
	attachCmd := exec.Command("tmux", "attach-session", "-t", sessionName)
	attachCmd.Stdin = os.Stdin
	attachCmd.Stdout = os.Stdout
	attachCmd.Stderr = os.Stderr
	return attachCmd.Run()
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

func executeSelectCustomCommand(command string, item *ui.Item) {
	cmd := exec.Command("sh", "-c", command)
	cmd.Env = append(os.Environ(),
		"POP_PATH="+item.Path,
		"POP_NAME="+item.Name,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		debug.Error("select: custom command %q: %v", command, err)
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
				for _, wt := range worktrees {
					projects = append(projects, project.ExpandedProject{
						Name:        displayName + "/" + wt.Name,
						Path:        wt.Path,
						ProjectName: projectName,
						IsWorktree:  true,
					})
				}
			} else {
				// Regular project
				projects = append(projects, project.ExpandedProject{
					Name:        displayName,
					Path:        ep.Path,
					ProjectName: projectName,
					IsWorktree:  false,
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
