package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/history"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/ui"
	"github.com/spf13/cobra"
)

var tmuxCDPane string

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
}

func runSelect(cmd *cobra.Command, args []string) error {
	// Load config
	cfgPath := cfgFile
	if cfgPath == "" {
		cfgPath = config.DefaultConfigPath()
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("failed to load config: %w", err)
		}
		// Config doesn't exist — run interactive init
		d := defaultConfigureDeps()
		d.ShowWelcome = true
		if err := runConfigureWith(d); err != nil {
			return err
		}
		cfg, err = config.Load(cfgPath)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
	}

	// Expand project paths
	paths, err := cfg.ExpandProjects()
	if err != nil {
		return fmt.Errorf("failed to expand projects: %w", err)
	}

	if len(paths) == 0 {
		return fmt.Errorf("no projects found. Check your config at %s", cfgPath)
	}

	// Expand projects, showing worktrees for bare repos (parallel)
	type expandResult struct {
		index    int
		projects []project.ExpandedProject
	}

	results := make(chan expandResult, len(paths))
	var wg sync.WaitGroup

	for i, p := range paths {
		wg.Add(1)
		go func(idx int, ep config.ExpandedPath) {
			defer wg.Done()

			displayName := ui.LastNSegments(ep.Path, ep.DisplayDepth)
			projectName := filepath.Base(ep.Path)
			var projects []project.ExpandedProject

			if project.HasWorktrees(ep.Path) {
				// Bare repo with worktrees - expand to individual worktrees
				worktrees, err := project.ListWorktreesForPath(ep.Path)
				if err == nil {
					for _, wt := range worktrees {
						projects = append(projects, project.ExpandedProject{
							Name:        displayName + "/" + wt.Name,
							Path:        wt.Path,
							ProjectName: projectName,
							IsWorktree:  true,
						})
					}
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

			results <- expandResult{index: idx, projects: projects}
		}(i, p)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results maintaining original order
	resultsByIndex := make(map[int][]project.ExpandedProject)
	for r := range results {
		resultsByIndex[r.index] = r.projects
	}

	// Get current directory for optional filtering (resolve symlinks for proper comparison)
	var cwd string
	if cfg.ExcludeCurrentDir {
		cwd, _ = os.Getwd()
		if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
			cwd = resolved
		}
	}

	var expanded []project.ExpandedProject
	for i := range paths {
		for _, ep := range resultsByIndex[i] {
			// Skip current directory if configured (ep.Path is already canonical)
			if cfg.ExcludeCurrentDir && ep.Path == cwd {
				continue
			}
			expanded = append(expanded, ep)
		}
	}

	// Disambiguate projects with the same name
	project.DisambiguateNames(expanded, cfg.GetDisambiguationStrategy())

	// Load history and sort by recency (oldest first, most recent last)
	hist, err := history.Load(history.DefaultHistoryPath())
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

	// Convert to UI items
	items := make([]ui.Item, len(sortedExpanded))
	for i, ep := range sortedExpanded {
		items[i] = ui.Item{
			Name:    ep.Name,
			Path:    ep.Path,
			Context: ep.ProjectName, // Store project name for session naming
		}
	}

	// Run picker loop
	inTmux := os.Getenv("TMUX") != ""
	for {
		quickAccessModifier := cfg.GetQuickAccessModifier()
		opts := []ui.PickerOption{ui.WithCursorAtEnd(), ui.WithKillSession(), ui.WithReset(), ui.WithQuickAccess(quickAccessModifier)}
		if inTmux {
			opts = append(opts, ui.WithOpenWindow())
		}
		result, err := ui.Run(items, opts...)
		if err != nil {
			return err
		}

		switch result.Action {
		case ui.ActionCancel:
			os.Exit(1)

		case ui.ActionSelect:
			if result.Selected == nil {
				os.Exit(1)
			}
			// Record selection in history (paths are already canonical from config)
			hist.Record(result.Selected.Path)
			hist.Save()
			if tmuxCDPane != "" {
				return sendCDToPane(tmuxCDPane, result.Selected.Path)
			}
			// Open tmux session
			return openTmuxSession(result.Selected)

		case ui.ActionOpenWindow:
			if result.Selected == nil {
				os.Exit(1)
			}
			hist.Record(result.Selected.Path)
			hist.Save()
			return openTmuxWindow(result.Selected)

		case ui.ActionKillSession:
			if result.Selected != nil {
				killTmuxSession(result.Selected.Name)
			}
			// Continue loop to show picker again

		case ui.ActionReset:
			if result.Selected != nil {
				hist.Remove(result.Selected.Path)
				hist.Save()
				items = sortItemsByHistory(items, hist)
			}
			// Continue loop to show picker again
		}
	}
}

func sortItemsByHistory(items []ui.Item, hist *history.History) []ui.Item {
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

func openTmuxSession(item *ui.Item) error {
	// Session name: use the display name (project/worktree or just project)
	sessionName := sanitizeSessionName(item.Name)

	// Check if we're in tmux
	inTmux := os.Getenv("TMUX") != ""

	// Check if session exists
	checkCmd := exec.Command("tmux", "has-session", "-t="+sessionName)
	sessionExists := checkCmd.Run() == nil

	if !sessionExists {
		// Create new session
		newCmd := exec.Command("tmux", "new-session", "-ds", sessionName, "-c", item.Path)
		if err := newCmd.Run(); err != nil {
			return fmt.Errorf("failed to create tmux session: %w", err)
		}
	}

	if inTmux {
		// Switch to session
		switchCmd := exec.Command("tmux", "switch-client", "-t", sessionName)
		return switchCmd.Run()
	} else {
		// Attach to session
		attachCmd := exec.Command("tmux", "attach-session", "-t", sessionName)
		attachCmd.Stdin = os.Stdin
		attachCmd.Stdout = os.Stdout
		attachCmd.Stderr = os.Stderr
		return attachCmd.Run()
	}
}

func openTmuxWindow(item *ui.Item) error {
	windowName := sanitizeSessionName(item.Name)

	// Get current session name
	out, err := exec.Command("tmux", "display-message", "-p", "#S").Output()
	if err != nil {
		return fmt.Errorf("failed to get current tmux session: %w", err)
	}
	session := strings.TrimSpace(string(out))

	// Check if window with this name already exists
	listOut, err := exec.Command("tmux", "list-windows", "-t", session, "-F", "#{window_name}").Output()
	if err != nil {
		return fmt.Errorf("failed to list tmux windows: %w", err)
	}

	for _, name := range strings.Split(strings.TrimSpace(string(listOut)), "\n") {
		if name == windowName {
			// Window exists, switch to it
			return exec.Command("tmux", "select-window", "-t", session+":"+windowName).Run()
		}
	}

	// Create new window
	return exec.Command("tmux", "new-window", "-t", session, "-n", windowName, "-c", item.Path).Run()
}

func sanitizeSessionName(name string) string {
	// Replace dots and colons with underscores for tmux compatibility
	name = strings.ReplaceAll(name, ".", "_")
	name = strings.ReplaceAll(name, ":", "_")
	return name
}

func killTmuxSession(name string) {
	sessionName := sanitizeSessionName(name)
	cmd := exec.Command("tmux", "kill-session", "-t", sessionName)
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to kill session: %s\n", sessionName)
	} else {
		fmt.Fprintf(os.Stderr, "Killed session: %s\n", sessionName)
	}
}

func sendCDToPane(paneID, path string) error {
	cmd := exec.Command("tmux", "send-keys", "-t", paneID, fmt.Sprintf("cd %q", path), "Enter")
	return cmd.Run()
}

