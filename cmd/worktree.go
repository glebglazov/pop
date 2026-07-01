package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/debug"
	"github.com/glebglazov/pop/history"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/session"
	"github.com/glebglazov/pop/ui"
	"github.com/spf13/cobra"
)

var worktreeCmd = &cobra.Command{
	Use:   "worktree",
	Short: "Manage worktree picker commands",
	Long: `Manage worktree picker commands.

Use "pop worktree dashboard" to open the picker.`,
	// Deprecated compatibility path: use `pop worktree dashboard` instead.
	// TODO: remove the direct picker invocation at the next major CLI change.
	RunE: runWorktree,
}

var worktreeDashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Select a git worktree in the current repository",
	Long: `Opens a fuzzy picker to select a git worktree.
Must be run from within a git repository.

Keybindings:
  enter    - switch to worktree (prints path or switches tmux session)
  ctrl-d   - delete worktree
  ctrl-x   - force delete worktree
  esc      - cancel

Example tmux binding:
  bind-key P display-popup -E -w 60% -h 60% 'cd "$(pop worktree dashboard)" && exec $SHELL'`,
	RunE: runWorktree,
}

var switchSession bool
var worktreeYankTarget string

func init() {
	worktreeCmd.PersistentFlags().BoolVarP(&switchSession, "switch", "s", false, "Switch tmux session instead of printing path")
	worktreeCmd.PersistentFlags().StringVar(&worktreeYankTarget, "yank-target", "", "Send yanked path to specified tmux pane instead of system clipboard")
	worktreeCmd.AddCommand(worktreeDashboardCmd)
	rootCmd.AddCommand(worktreeCmd)
}

func runWorktree(cmd *cobra.Command, args []string) error {
	systemWarnings := ensureSystemState()

	// Detect repo context
	ctx, err := project.DetectRepoContext()
	if err != nil {
		return fmt.Errorf("not in a git repository")
	}

	// Load config (optional, don't fail if missing)
	var customCommands []ui.UserDefinedCommand
	var configWarnings []string
	quickAccessModifier := "alt"
	attentionEnabled := false
	updateNoticeEnabled := true
	if cfg, err := config.Load(config.DefaultConfigPath()); err == nil {
		quickAccessModifier = cfg.GetQuickAccessModifier()
		configWarnings = cfg.Warnings
		attentionEnabled = cfg.UnreadNotificationsEnabled("worktree")
		updateNoticeEnabled = cfg.UpdateNoticeEnabled()
		for _, cc := range cfg.CommandsForMode("worktree") {
			customCommands = append(customCommands, ui.UserDefinedCommand{
				Key:     cc.Key,
				Label:   cc.Label,
				Command: cc.Command,
				Exit:    cc.Exit,
			})
		}
	}
	configWarnings = append(configWarnings, systemWarnings...)

	restoreCursorIdx := -1
	for {
		result, err := showWorktreePicker(ctx, customCommands, quickAccessModifier, restoreCursorIdx, configWarnings, attentionEnabled, updateNoticeEnabled)
		restoreCursorIdx = -1
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
			return handleWorktreeSelect(ctx, result.Selected)

		case ui.ActionDelete:
			if result.Selected != nil {
				deleteWorktree(result.Selected.Path, false)
			}
			// Continue loop to show picker again

		case ui.ActionForceDelete:
			if result.Selected != nil {
				deleteWorktree(result.Selected.Path, true)
			}
			// Continue loop to show picker again

		case ui.ActionKillSession:
			if result.Selected != nil {
				restoreCursorIdx = result.CursorIndex
				sessionName := project.SessionName(result.Selected.Path)
				killTmuxSessionByName(sessionName)
			}
			// Continue loop — showWorktreePicker refreshes session state

		case ui.ActionReset:
			if result.Selected != nil {
				removeFromHistory(result.Selected.Path)
			}
			// Continue loop to show picker again

		case ui.ActionRefresh:
			restoreCursorIdx = result.CursorIndex
			// Continue loop — items rebuild with fresh attention state

		case ui.ActionSetPreferredWorkbench:
			// Sets the highlighted worktree's Preferred workbench (ADR-0078);
			// never touches a running session.
			if result.Selected != nil {
				warnPreferredWorkbenchErr("worktree", setPreferredWorkbench(defaultPreferredPickerDeps(), result.Selected.Path))
			}
			restoreCursorIdx = result.CursorIndex
			// Continue loop to show picker again

		case ui.ActionCreateWorktree:
			if err := createWorktree(ctx); err != nil {
				debug.Error("worktree: create: %v", err)
				fmt.Fprintf(os.Stderr, "Failed to create worktree: %v\n", err)
				// Continue loop to show picker again
				continue
			}
			return nil

		case ui.ActionYankPath:
			if result.Selected == nil {
				return nil
			}
			paneID := worktreeYankTarget
			if paneID == "" {
				paneID = os.Getenv("TMUX_PANE")
			}
			if paneID == "" {
				return fmt.Errorf("yank target pane not set — pass --yank-target or run inside tmux")
			}
			return yankPathToPaneWith(defaultTmux, paneID, result.Selected.Path)

		case ui.ActionUserDefinedCommand:
			if result.UserDefinedCommand != nil && result.Selected != nil {
				executeCustomCommand(result.UserDefinedCommand.Command, result.Selected, ctx)
				if result.UserDefinedCommand.Exit {
					return nil
				}
			}
			// Continue loop to show picker again (if exit = false)
		}
	}
}

func showWorktreePicker(ctx *project.RepoContext, customCommands []ui.UserDefinedCommand, quickAccessModifier string, initialCursorIdx int, warnings []string, attentionEnabled, updateNoticeEnabled bool) (ui.Result, error) {
	worktrees, err := project.ListWorktrees(ctx)
	if err != nil {
		return ui.Result{Action: ui.ActionCancel}, fmt.Errorf("failed to list worktrees: %w", err)
	}

	if len(worktrees) == 0 {
		return ui.Result{Action: ui.ActionCancel}, fmt.Errorf("no worktrees found")
	}

	// Load history and sort by recency (oldest first, most recent last)
	hist, err := history.Load(history.DefaultHistoryPath())
	if err != nil {
		hist = &history.History{}
	}

	// Convert to Project for sorting, then back
	projects := make([]project.Project, len(worktrees))
	for i, wt := range worktrees {
		projects[i] = project.Project{Name: wt.Name, Path: wt.Path}
	}
	projects = hist.SortByRecency(projects)

	// Rebuild worktrees list in sorted order
	pathToWorktree := make(map[string]project.Worktree)
	for _, wt := range worktrees {
		pathToWorktree[wt.Path] = wt
	}
	sortedWorktrees := make([]project.Worktree, len(projects))
	for i, p := range projects {
		sortedWorktrees[i] = pathToWorktree[p.Path]
	}

	// Convert to UI items with session icons
	items := buildWorktreeItems(ctx, sortedWorktrees, history.TmuxSessionActivity())

	iconLegends := []ui.IconLegend{
		{Icon: iconDirSession, Desc: "Directory with tmux session"},
	}
	if attentionEnabled {
		iconLegends = append(iconLegends, ui.IconLegend{Icon: iconAttention, Desc: "Agent has unread output"})
		// Apply attention icons to worktree items
		attentionSessions := monitorAttentionSessions()
		if attentionSessions != nil {
			for i := range items {
				sessionName := project.TmuxSessionName(ctx, items[i].Name)
				if attentionSessions[sessionName] {
					items[i].Icon = iconAttention
				}
			}
		}
	}
	opts := []ui.PickerOption{
		ui.WithDelete(),
		ui.WithContext(),
		ui.WithCursorAtEnd(),
		ui.WithKillSession(),
		ui.WithReset(),
		ui.WithCreateWorktree(),
		ui.WithSetPreferredWorkbench(),
		ui.WithQuickAccess(quickAccessModifier),
		ui.WithIconLegend(iconLegends...),
	}
	if initialCursorIdx >= 0 {
		opts = append(opts, ui.WithInitialCursorIndex(initialCursorIdx))
	}
	if len(customCommands) > 0 {
		opts = append(opts, ui.WithUserDefinedCommands(customCommands))
	}
	if len(warnings) > 0 {
		opts = append(opts, ui.WithWarnings(warnings))
	}
	// Gating the call (not just the badge) also prevents the background Update
	// fetch when [updates] notice_enabled = false.
	if updateNoticeEnabled {
		if notice := pickerUpdateNotice(); notice != "" {
			opts = append(opts, ui.WithUpdateNotice(notice))
		}
	}

	return ui.Run(items, opts...)
}

func buildWorktreeItems(ctx *project.RepoContext, worktrees []project.Worktree, sessionActivity map[string]int64) []ui.Item {
	items := make([]ui.Item, len(worktrees))
	for i, wt := range worktrees {
		items[i] = ui.Item{
			Name:    wt.Name,
			Path:    wt.Path,
			Context: wt.Branch,
		}
		sessionName := project.TmuxSessionName(ctx, wt.Name)
		if _, hasSession := sessionActivity[sessionName]; hasSession {
			items[i].Icon = iconDirSession
		}
	}
	return items
}

// createWorktree runs the interactive create flow (ADR-0076): pick a branch,
// derive the worktree name/path, run `git worktree add`, record the new checkout
// in history, and attach a flat session for it immediately.
func createWorktree(ctx *project.RepoContext) error {
	branches, err := project.ListBranches(ctx)
	if err != nil {
		return fmt.Errorf("failed to list branches: %w", err)
	}
	if len(branches) == 0 {
		return fmt.Errorf("no branches found")
	}

	items := make([]ui.Item, len(branches))
	byRef := make(map[string]project.Branch, len(branches))
	for i, b := range branches {
		items[i] = ui.Item{Name: b.Ref, Path: b.Ref}
		byRef[b.Ref] = b
	}

	result, err := ui.Run(items,
		ui.WithHeader("Pick a branch for the new worktree"),
		ui.WithInitialCursorIndex(0))
	if err != nil {
		return err
	}
	if result.Action != ui.ActionConfirm || result.Selected == nil {
		// Esc/cancel in the branch picker: create nothing.
		return nil
	}

	selection := byRef[result.Selected.Path]

	// Name step (ADR-0076): the typed name is the NEW branch name; the picked
	// ref is only the fork base. Empty field, hinted `(base: <ref>)`, empty
	// submit falls back to the branch-derived default. Esc aborts cleanly.
	_, defaultDir := project.DeriveWorktreeName(selection.Ref, selection.IsRemote)
	name, confirmed, err := ui.PromptName("Name the new worktree", defaultDir, selection.Ref)
	if err != nil {
		return err
	}
	if !confirmed {
		// Esc/cancel in the name prompt: create nothing.
		return nil
	}

	path, err := project.AddWorktreeNamed(ctx, selection, name)
	if err != nil {
		return err
	}

	// Shape the new checkout's session: a Workbench when [workbench]
	// pick_on_create is on and one resolves (ADR-0075/0076), else today's flat
	// session. Both paths record the checkout in History.
	return shapeWorktreeSession(defaultWorktreeShapeDeps(), ctx, path)
}

// worktreeShapeDeps carries the seams for shaping a freshly-created worktree's
// session (ADR-0075/0076). It is split out from createWorktree so the
// gated-prompt and flat fall-through paths are unit-testable with mocks; the
// branch/name/`git worktree add` steps above run once, before shaping begins.
type worktreeShapeDeps struct {
	LoadConfig                func() (*config.Config, error)
	PickOnCreate              func(cfg *config.Config) bool
	ResolveWorkbenches        func(cfg *config.Config, path string) []config.SessionTemplate
	ResolvePreferredWorkbench func(cfg *config.Config, path string) (string, []string)
	PromptWorkbench           func(workbenches []config.SessionTemplate) (name string, confirmed bool, err error)
	FindWorkbench             func(workbenches []config.SessionTemplate, name string) (config.SessionTemplate, bool)
	CreateSession             func(tmpl config.SessionTemplate, sessionName, path string) error
	SessionName               func(path string) string
	RecordHistory             func(path string)
	Attach                    func(sessionName string) error
	Flat                      func(ctx *project.RepoContext, item *ui.Item) error
}

// defaultWorktreeShapeDeps wires worktreeShapeDeps to production implementations,
// reusing the existing Workbench resolution (ResolveSessionTemplatesWith — so
// bare-repo Workbenches still propagate to the new worktree), prompt
// (promptWorkbenchForCreate), and create (createSessionFromWorkbench) helpers.
func defaultWorktreeShapeDeps() *worktreeShapeDeps {
	return &worktreeShapeDeps{
		LoadConfig: func() (*config.Config, error) {
			path := cfgFile
			if path == "" {
				path = config.DefaultConfigPath()
			}
			return config.Load(path)
		},
		PickOnCreate: func(cfg *config.Config) bool { return cfg.WorkbenchPickOnCreate() },
		ResolveWorkbenches: func(cfg *config.Config, path string) []config.SessionTemplate {
			templates, _ := cfg.ResolveSessionTemplatesWith(config.DefaultDeps(), path)
			return templates
		},
		ResolvePreferredWorkbench: func(cfg *config.Config, path string) (string, []string) {
			return cfg.ResolvePreferredWorkbench(preferredResolverConfigDeps(cfg), path)
		},
		PromptWorkbench: func(workbenches []config.SessionTemplate) (string, bool, error) {
			return promptWorkbenchForCreate(&ProjectDeps{RunPicker: ui.Run}, workbenches)
		},
		FindWorkbench: findSessionTemplate,
		CreateSession: func(tmpl config.SessionTemplate, sessionName, path string) error {
			return createSessionFromWorkbench(defaultTemplateRuntimeDeps(), tmpl, sessionName, path)
		},
		SessionName:   project.SessionName,
		RecordHistory: recordWorktreeHistory,
		Attach:        func(sessionName string) error { return switchToTmuxTargetWith(defaultTmux, sessionName) },
		Flat:          handleWorktreeSelect,
	}
}

// shapeWorktreeSession honors the [workbench] pick_on_create gate for the native
// worktree-create flow (ADR-0075/0076). When the toggle is on and at least one
// Workbench resolves for the new checkout, it prompts; a concrete Workbench
// choice builds a session that is exactly that Workbench and attaches. The
// "no workbench" sentinel, an Esc, a disabled toggle, and an empty resolved set
// all fall through to today's flat session (the worktree already exists, so
// cancelling never un-creates it).
func shapeWorktreeSession(d *worktreeShapeDeps, ctx *project.RepoContext, path string) error {
	item := &ui.Item{Name: filepath.Base(path), Path: path}

	cfg, err := d.LoadConfig()
	if err == nil {
		// Preferred workbench (ADR-0078): a resolved per-checkout default
		// auto-applies silently and suppresses the prompt regardless of
		// pick_on_create. A stale name resolves to "" with a warning and falls
		// through to the pick_on_create gate below.
		preferred, warns := d.ResolvePreferredWorkbench(cfg, path)
		for _, w := range warns {
			debug.Error("worktree: %s", w)
		}
		if preferred != "" {
			workbenches := d.ResolveWorkbenches(cfg, path)
			if tmpl, ok := d.FindWorkbench(workbenches, preferred); ok {
				sessionName := d.SessionName(path)
				if err := d.CreateSession(tmpl, sessionName, path); err != nil {
					return err
				}
				d.RecordHistory(path)
				return d.Attach(sessionName)
			}
		}
	}
	if err == nil && d.PickOnCreate(cfg) {
		workbenches := d.ResolveWorkbenches(cfg, path)
		if len(workbenches) > 0 {
			name, confirmed, err := d.PromptWorkbench(workbenches)
			if err != nil {
				return err
			}
			if confirmed && name != "" {
				tmpl, ok := d.FindWorkbench(workbenches, name)
				if !ok {
					return fmt.Errorf("workbench %q not found", name)
				}
				sessionName := d.SessionName(path)
				if err := d.CreateSession(tmpl, sessionName, path); err != nil {
					return err
				}
				d.RecordHistory(path)
				return d.Attach(sessionName)
			}
			// "no workbench" or Esc → today's flat session.
		}
	}
	return d.Flat(ctx, item)
}

func handleWorktreeSelect(ctx *project.RepoContext, item *ui.Item) error {
	// Record selection in history (paths from git are already canonical)
	recordWorktreeHistory(item.Path)

	if switchSession {
		return switchTmuxSession(item)
	}
	// Print path for shell integration
	fmt.Println(item.Path)
	return nil
}

// recordWorktreeHistory records a checkout path in project history, logging (not
// propagating) failures — history bookkeeping must never block attaching to the
// new session. Shared by the flat and Workbench create paths.
func recordWorktreeHistory(path string) {
	hist, err := history.Load(history.DefaultHistoryPath())
	if err != nil {
		debug.Error("worktree: load history: %v", err)
	}
	hist.Record(path)
	if err := hist.Save(); err != nil {
		debug.Error("worktree: save history: %v", err)
	}
}

func switchTmuxSession(item *ui.Item) error {
	return switchTmuxSessionWith(defaultTmux, item)
}

func switchTmuxSessionWith(tmux deps.Tmux, item *ui.Item) error {
	return session.AttachWith(&session.Deps{
		Tmux:   tmux,
		InTmux: func() bool { return os.Getenv("TMUX") != "" },
	}, project.SessionName(item.Path), item.Path)
}

func deleteWorktree(path string, force bool) {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)

	cmd := exec.Command("git", args...)
	output, err := cmd.CombinedOutput()

	if err != nil {
		debug.Error("deleteWorktree %s: %v: %s", path, err, output)
		fmt.Fprintf(os.Stderr, "Failed to delete worktree: %s\n%s\n", path, output)
		return
	}
	fmt.Fprintf(os.Stderr, "Deleted: %s\n", path)
	// Worktree is gone — drop its history entry so it no longer skews
	// recency sorting or session-name matching. The tmux session (if any)
	// is left alone; killing it stays an explicit, separate action.
	removeFromHistory(path)
}

// removeFromHistory deletes path from project history, logging (not
// propagating) failures — history cleanup must never block the picker loop.
func removeFromHistory(path string) {
	removeFromHistoryWith(history.DefaultDeps(), history.DefaultHistoryPath(), path)
}

func removeFromHistoryWith(d *history.Deps, histPath, path string) {
	hist, err := history.LoadWith(d, histPath)
	if err != nil {
		debug.Error("worktree: load history: %v", err)
		return
	}
	hist.RemoveWith(d, path)
	if err := hist.SaveWith(d); err != nil {
		debug.Error("worktree: save history: %v", err)
	}
}

func executeCustomCommand(command string, item *ui.Item, ctx *project.RepoContext) {
	cmd := exec.Command("sh", "-c", command)

	// Set environment variables
	cmd.Env = append(os.Environ(),
		"POP_PATH="+item.Path,
		"POP_NAME="+filepath.Base(item.Path),
		"POP_WORKTREE_PATH="+item.Path,
		"POP_WORKTREE_NAME="+filepath.Base(item.Path),
		"POP_BRANCH="+item.Context,
		"POP_REPO_ROOT="+ctx.GitRoot,
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		debug.Error("worktree: custom command %q: %v", command, err)
		fmt.Fprintf(os.Stderr, "Custom command failed: %v\n", err)
	}
}
