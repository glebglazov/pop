package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/debug"
	"github.com/glebglazov/pop/history"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
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
  up/down, ctrl-p/ctrl-n - navigate
  ctrl-b/ctrl-f         - page up/down
  ctrl-u, alt-backspace - clear the filter
  ctrl-h                - show help
  enter                 - switch to worktree (prints path or switches tmux session)
  ctrl-a                - create worktree
  ctrl-t                - create managed worktree
  ctrl-l                - fold worktree in a tagged tmux pane
  ctrl-k                - kill the worktree's tmux session
  ctrl-r                - reset worktree history
  ctrl-y                - yank path to a tmux pane
  ctrl-d                - delete worktree
  ctrl-x                - force delete worktree
  alt-c                 - open the Config dashboard over the picker
  alt-1..9              - quick select (modifier is configurable)
  esc, ctrl-c           - cancel

While the Config dashboard is open it owns the keyboard: no picker key does
anything, ctrl-x included, so removing an override there cannot delete a
worktree here. Closing it returns to the picker with the filter and cursor
untouched.

Example tmux binding:
  bind-key P display-popup -E -w 60% -h 60% 'cd "$(pop worktree dashboard)" && exec $SHELL'

The Config dashboard wants more room than this picker does, so its own
standalone binding is roomier:
  bind-key C display-popup -E -w 80% -h 80% 'pop config dashboard'`,
	RunE: runWorktree,
}

var worktreeFoldCmd = &cobra.Command{
	Use:   "fold [<name>]",
	Short: "Rebase a worktree onto trunk and fast-forward trunk",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runWorktreeFold,
}

var switchSession bool
var worktreeYankTarget string
var worktreeFoldYes bool
var worktreeFoldAgentPresets []string
var worktreeFoldAgentCmd string

func init() {
	worktreeCmd.PersistentFlags().BoolVarP(&switchSession, "switch", "s", false, "Switch tmux session instead of printing path")
	worktreeCmd.PersistentFlags().StringVar(&worktreeYankTarget, "yank-target", "", "Send yanked path to specified tmux pane instead of system clipboard")
	worktreeCmd.AddCommand(worktreeDashboardCmd)
	worktreeFoldCmd.Flags().BoolVarP(&worktreeFoldYes, "yes", "y", false, "Fold without interactive confirmation")
	worktreeFoldCmd.Flags().StringArrayVar(&worktreeFoldAgentPresets, "agent", nil, "Agent preset for fold-conflict assistance, optionally followed by extra agent args")
	worktreeFoldCmd.Flags().StringVar(&worktreeFoldAgentCmd, "agent-cmd", "", "Trusted shell prefix for fold-conflict assistance; generated prompt passed as final positional argument")
	worktreeCmd.AddCommand(worktreeFoldCmd)
	rootCmd.AddCommand(worktreeCmd)
}

func runWorktreeFold(cmd *cobra.Command, args []string) error {
	d := cmdLayerDeps()
	cwd, err := d.DirOrGetwd()
	if err != nil {
		return fmt.Errorf("worktree fold: resolve current checkout: %w", err)
	}
	name := ""
	if len(args) == 1 {
		name = args[0]
	}
	cfgPath := cfgFile
	if cfgPath == "" {
		cfgPath = taskConfigPath()
	}
	cfg, err := taskConfigLoad(cfgPath)
	if err != nil {
		return fmt.Errorf("worktree fold: %w", err)
	}
	agentPreset := ""
	if len(worktreeFoldAgentPresets) > 0 {
		agentPreset = worktreeFoldAgentPresets[len(worktreeFoldAgentPresets)-1]
	}
	return runWorktreeFoldWith(d, cfg, cwd, name, binding.FoldOptions{
		Yes:                 worktreeFoldYes,
		In:                  cmd.InOrStdin(),
		ConfirmCheckoutFold: true,
		AgentPreset:         agentPreset,
		AgentCmd:            worktreeFoldAgentCmd,
	}, cmd.OutOrStdout())
}

func runWorktreeFoldWith(d *Deps, cfg *config.Config, cwd, name string, opts binding.FoldOptions, out io.Writer) error {
	path, err := resolveWorktreeFoldPath(d.projectDeps(), cwd, name)
	if err != nil {
		return fmt.Errorf("worktree fold: %w", err)
	}
	// A binding on this checkout is not this verb's business to refuse: the fold
	// itself names every bound set in its confirmation and settles the ones it
	// finishes (ADR-0251).
	if _, err := binding.FoldCheckout(d.tasksDeps(), cfg, path, opts, out); err != nil {
		return fmt.Errorf("worktree fold: %w", err)
	}
	return nil
}

func resolveWorktreeFoldPath(d *project.Deps, cwd, name string) (string, error) {
	ctx, err := project.DetectRepoContextFromPathWith(d, cwd)
	if err != nil {
		return "", fmt.Errorf("not in a git repository")
	}
	if strings.TrimSpace(name) == "" {
		if ctx.IsBare {
			return "", fmt.Errorf("current directory is not a worktree")
		}
		return ctx.GitRoot, nil
	}
	worktrees, err := project.ListWorktreesWith(d, ctx)
	if err != nil {
		return "", fmt.Errorf("list worktrees: %w", err)
	}
	var matches []string
	for _, wt := range worktrees {
		if wt.Name == name {
			matches = append(matches, wt.Path)
		}
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("worktree %q not found", name)
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("worktree name %q is ambiguous", name)
	}
	return matches[0], nil
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
		// Surface non-fatal .pop/config.toml scope-legality findings (ADR-0083): a
		// global/machine-only or [repo]-only key committed to .pop/config.toml is ignored
		// but warned about here. The error is deliberately dropped — findings are
		// carried regardless and this flow degrades rather than aborts (ADR-0054).
		if rc, _ := cfg.ResolveRepoConfig(config.DefaultDeps(), ctx.GitRoot); len(rc.Findings) > 0 {
			for _, f := range rc.Findings {
				configWarnings = append(configWarnings, f.Message)
			}
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
			// Selecting an existing worktree gets the same birth-time shaping
			// as the create/project paths, gated on session-absence (ADR-0075):
			// no live session → Preferred auto-applies / pick_on_create prompts /
			// flat fall-through; a live session attaches flat with no reshaping.
			return openWorktreeWithShaping(defaultWorktreeShapeDeps(), ctx, result.Selected.Path)

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
				sessionName := checkoutSessionName(result.Selected.Path)
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

		case ui.ActionFoldWorktree:
			if result.Selected != nil {
				restoreCursorIdx = result.CursorIndex
				if err := launchWorktreeFold(defaultTmuxMod, result.Selected); err != nil {
					debug.Error("worktree: fold: %v", err)
					fmt.Fprintf(os.Stderr, "Fold refused: %v\n", err)
				}
			}
			// Fold runs in its pane; return to a freshly-built picker.

		case ui.ActionCreateWorktree:
			if err := createWorktree(ctx); err != nil {
				debug.Error("worktree: create: %v", err)
				fmt.Fprintf(os.Stderr, "Failed to create worktree: %v\n", err)
				// Continue loop to show picker again
				continue
			}
			return nil

		case ui.ActionCreateManagedWorktree:
			if err := createManagedWorktree(ctx); err != nil {
				debug.Error("worktree: create managed: %v", err)
				fmt.Fprintf(os.Stderr, "Failed to create managed worktree: %v\n", err)
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
			return yankPathToPaneWith(defaultTmuxMod, paneID, result.Selected.Path)

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
	hist, err := history.LoadWith(cmdHistoryDeps())
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
	items := buildWorktreeItems(ctx, sortedWorktrees, history.TmuxSessionActivity(), cmdLayerDeps().tasksDeps())

	iconLegends := []ui.IconLegend{
		{Icon: iconDirSession, Desc: "Directory with tmux session"},
		{Icon: iconBoundManaged, Desc: "Managed worktree (Task set bound)"},
		{Icon: iconUnboundManaged, Desc: "Unbound managed worktree (no Task set bound)"},
	}
	if attentionEnabled {
		iconLegends = append(iconLegends, ui.IconLegend{Icon: iconAttention, Desc: "Agent has unread output"})
		// Apply attention icons to worktree items
		attentionSessions := monitorAttentionSessions()
		if attentionSessions != nil {
			for i := range items {
				sessionName := project.TmuxSessionNameAt(ctx, items[i].Path, items[i].Name)
				if attentionSessions[sessionName] {
					items[i].Icon = iconAttention
				}
			}
		}
	}
	return ui.Run(items, worktreePickerOptions(customCommands, quickAccessModifier, initialCursorIdx, warnings, iconLegends, updateNoticeEnabled)...)
}

// worktreePickerOptions is the worktree picker's key set and chrome, apart from
// the items, so a test can put the same picker together and press a key at it.
func worktreePickerOptions(customCommands []ui.UserDefinedCommand, quickAccessModifier string, initialCursorIdx int, warnings []string, iconLegends []ui.IconLegend, updateNoticeEnabled bool) []ui.PickerOption {
	opts := []ui.PickerOption{
		ui.WithDelete(),
		ui.WithContext(),
		ui.WithCursorAtEnd(),
		ui.WithKillSession(),
		ui.WithReset(),
		ui.WithCreateWorktree(),
		ui.WithFoldWorktree(),
		ui.WithQuickAccess(quickAccessModifier),
		ui.WithIconLegend(iconLegends...),
		// While the Config dashboard is open every key above is suspended, which
		// is what keeps ctrl+x meaning "remove the override" there and "force
		// delete this worktree" here (ADR-0202 decision 11).
		ui.WithConfigDashboard(configDashboardOpener()),
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

	return opts
}

// launchWorktreeFold starts the checkout-addressed Fold outside the picker
// process. The picker owns stdout as its selected-path result, so this helper
// deliberately has no output writer and sends all Fold interaction to a pane.
func launchWorktreeFold(mod tmuxmod.Tmux, item *ui.Item) error {
	if item == nil {
		return nil
	}
	if !mod.InTmux() {
		return fmt.Errorf("run `pop worktree fold` directly outside tmux")
	}
	session := checkoutSessionName(item.Path)
	paneID, err := tmuxmod.EnsureTaggedPane(
		mod,
		tmuxmod.TagFold,
		session,
		tmuxmod.DrainWindow,
		item.Path,
		item.Path,
		"pop worktree fold",
	)
	if err != nil {
		return fmt.Errorf("launch `pop worktree fold`: %w", err)
	}
	return mod.SetPaneTitle(paneID, "fold · "+item.Name)
}

// buildWorktreeItems converts worktrees to picker items, applying the session
// icon and the managed-worktree marker. The marker renders all three states the
// classifier reports — bound managed, unbound managed, and (as a blank column)
// an ordinary human worktree — so the picker never conflates a live managed
// checkout with one a human made. The classification is the one binding-store
// read this surface makes (ADR-0152) — bounded to here; it never runs during
// project expansion.
func buildWorktreeItems(ctx *project.RepoContext, worktrees []project.Worktree, sessionActivity map[string]int64, td *tasks.Deps) []ui.Item {
	items := make([]ui.Item, len(worktrees))
	for i, wt := range worktrees {
		items[i] = ui.Item{
			Name:    wt.Name,
			Path:    wt.Path,
			Context: wt.Branch,
		}
		sessionName := project.TmuxSessionNameAt(ctx, wt.Path, wt.Name)
		if _, hasSession := sessionActivity[sessionName]; hasSession {
			items[i].Icon = iconDirSession
		}
		if state, err := binding.ClassifyManagedWorktree(td, wt.Path); err == nil {
			switch state {
			case binding.ManagedUnbound:
				items[i].Marker = iconUnboundManaged
			case binding.ManagedBound:
				items[i].Marker = iconBoundManaged
			case binding.OrdinaryWorktree:
				items[i].Marker = ""
			}
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

	items, byRef := baseRefPickerItems(branches)

	result, err := ui.Run(items,
		ui.WithHeader("Pick a branch for the new worktree"),
		ui.WithCursorAtEnd())
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
	// session. Both paths record the checkout in History. A freshly-created
	// worktree has no session yet, so the session-absence gate is transparent
	// here and this behaves exactly as before.
	return openWorktreeWithShaping(defaultWorktreeShapeDeps(), ctx, path)
}

// createManagedWorktree runs the managed-create flow (ADR-0152): pick a base
// ref — the Trunk worktree's branch preselected, so Enter accepts it with no
// further input — then fork a pop-managed worktree under the managed-worktree
// root and attach, shaped exactly like the ordinary create path. No name is
// requested (the directory carries a generated scratch name) and no Task set
// is involved: the worktree stays unbound until a set registers from inside it.
func createManagedWorktree(ctx *project.RepoContext) error {
	td := cmdLayerDeps().tasksDeps()
	branches, err := project.ListBranches(ctx)
	if err != nil {
		return fmt.Errorf("failed to list branches: %w", err)
	}
	if len(branches) == 0 {
		return fmt.Errorf("no branches found")
	}

	items, byRef := baseRefPickerItems(branches)

	opts := []ui.PickerOption{
		ui.WithHeader("Pick a base ref for the managed worktree"),
		ui.WithCursorAtEnd(),
	}
	cfg, _ := config.Load(config.DefaultConfigPathWith(cmdLayerDeps().configDeps()))
	if idx := trunkBranchCursorIndex(td, cmdLayerDeps().configDeps(), cfg, ctx.GitRoot, items); idx >= 0 {
		opts = append(opts, ui.WithInitialCursorIndex(idx))
	}

	result, err := ui.Run(items, opts...)
	if err != nil {
		return err
	}
	if result.Action != ui.ActionConfirm || result.Selected == nil {
		// Esc/cancel in the base-ref picker: create nothing.
		return nil
	}
	selection := byRef[result.Selected.Path]

	b, err := binding.ProvisionScratchWorktree(td, ctx.GitRoot, selection.Ref, time.Now())
	if err != nil {
		return err
	}

	// A freshly-created managed worktree has no session yet, so the
	// session-absence gate is transparent and this attaches exactly like the
	// ordinary create path, Workbench shaping included.
	return openWorktreeWithShaping(defaultWorktreeShapeDeps(), ctx, b.RuntimePath)
}

// baseRefPickerItems builds the base-ref picker's items from branches.
// ListBranches orders main/master first, but the picker anchors to the bottom
// with the cursor there (like the dashboards). Reverse into items so
// main/master land on the bottom row under the cursor — unified cursor
// position AND the sensible fork base stays the default.
func baseRefPickerItems(branches []project.Branch) ([]ui.Item, map[string]project.Branch) {
	items := make([]ui.Item, len(branches))
	byRef := make(map[string]project.Branch, len(branches))
	for i, b := range branches {
		items[len(branches)-1-i] = ui.Item{Name: b.Ref, Path: b.Ref}
		byRef[b.Ref] = b
	}
	return items, byRef
}

// trunkBranchCursorIndex returns the index of the Trunk worktree's branch in
// the base-ref item list, so the managed-create picker opens with it
// preselected (ADR-0152). It returns -1 — the caller falls back to the
// cursor-at-end default — when no trunk resolves, the trunk is detached, or
// its branch is not among the items.
func trunkBranchCursorIndex(td *tasks.Deps, cd *config.Deps, cfg *config.Config, checkoutPath string, items []ui.Item) int {
	trunkPath, bare, err := binding.ResolveTrunkPathWith(cd, td, cfg, checkoutPath)
	if err != nil || bare || strings.TrimSpace(trunkPath) == "" {
		return -1
	}
	branch := binding.CurrentBranch(td, trunkPath)
	if branch == "" {
		return -1
	}
	for i, item := range items {
		if item.Name == branch {
			return i
		}
	}
	return -1
}

// worktreeShapeDeps carries the seams for shaping a freshly-created worktree's
// session (ADR-0075/0076). It is split out from createWorktree so the
// gated-prompt and flat fall-through paths are unit-testable with mocks; the
// branch/name/`git worktree add` steps above run once, before shaping begins.
type worktreeShapeDeps struct {
	LoadConfig                func() (*config.Config, error)
	PickOnCreate              func(cfg *config.Config) bool
	ResolveWorkbenches        func(cfg *config.Config, path string) []config.Workbench
	ResolvePreferredWorkbench func(cfg *config.Config, path string) (string, []string)
	PromptWorkbench           func(order []string, workbenches []config.Workbench) (name string, confirmed bool, err error)
	FindWorkbench             func(workbenches []config.Workbench, name string) (config.Workbench, bool)
	CreateSession             func(tmpl config.Workbench, sessionName, path string) error
	SessionName               func(path string) string
	SessionExists             func(sessionName string) bool
	RecordHistory             func(path string)
	Attach                    func(sessionName string) error
	Flat                      func(ctx *project.RepoContext, item *ui.Item) error
}

// defaultWorktreeShapeDeps wires worktreeShapeDeps to production implementations,
// reusing the existing Workbench resolution (ResolveWorkbenchesWith — so
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
		ResolveWorkbenches: func(cfg *config.Config, path string) []config.Workbench {
			templates, _ := cfg.ResolveWorkbenchesWith(config.DefaultDeps(), path)
			return templates
		},
		ResolvePreferredWorkbench: func(cfg *config.Config, path string) (string, []string) {
			return cfg.ResolvePreferredWorkbench(preferredResolverConfigDeps(cfg), path)
		},
		PromptWorkbench: func(order []string, workbenches []config.Workbench) (string, bool, error) {
			return promptWorkbenchForCreate(&ProjectDeps{RunPicker: ui.Run}, order, workbenches)
		},
		FindWorkbench: findWorkbench,
		CreateSession: func(tmpl config.Workbench, sessionName, path string) error {
			return createSessionFromWorkbench(defaultTemplateRuntimeDeps(), tmpl, sessionName, path)
		},
		SessionName:   checkoutSessionName,
		SessionExists: func(sessionName string) bool { return defaultTmuxMod.HasSession(sessionName) },
		RecordHistory: recordWorktreeHistory,
		Attach:        func(sessionName string) error { return switchToTmuxTargetWith(defaultTmuxMod, sessionName) },
		Flat:          handleWorktreeSelect,
	}
}

// openWorktreeWithShaping opens path's checkout with birth-time Workbench
// shaping, gated on session-absence (ADR-0075). When path's session already
// exists it attaches flat with no reshaping of the built session; when the
// session is absent it runs the same shaping the create flow uses — a resolved
// Preferred workbench auto-applies silently, else pick_on_create prompts, else
// it falls through to today's flat attach. This is the single shared entry
// point for the worktree-picker select path and the native create flow (and,
// later, the queue-dashboard open).
func openWorktreeWithShaping(d *worktreeShapeDeps, ctx *project.RepoContext, path string) error {
	if d.SessionExists(d.SessionName(path)) {
		// Session already built (ADR-0075): attach flat, never reshape.
		return d.Flat(ctx, &ui.Item{Name: filepath.Base(path), Path: path})
	}
	return shapeWorktreeSession(d, ctx, path)
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
			name, confirmed, err := d.PromptWorkbench(cfg.WorkbenchOrder(), workbenches)
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
	hist, err := history.LoadWith(cmdHistoryDeps())
	if err != nil {
		debug.Error("worktree: load history: %v", err)
		return
	}
	if err := hist.Record(path); err != nil {
		debug.Error("worktree: record history: %v", err)
	}
}

func switchTmuxSession(item *ui.Item) error {
	return switchTmuxSessionWith(defaultTmuxMod, item)
}

func switchTmuxSessionWith(mod tmuxmod.Tmux, item *ui.Item) error {
	return tmuxmod.Attach(mod, checkoutSessionName(item.Path), item.Path)
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
	removeFromHistoryWith(cmdHistoryDeps(), path)
}

func removeFromHistoryWith(d *history.Deps, path string) {
	hist, err := history.LoadWith(d)
	if err != nil {
		debug.Error("worktree: load history: %v", err)
		return
	}
	if err := hist.Remove(path); err != nil {
		debug.Error("worktree: remove history entry: %v", err)
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
