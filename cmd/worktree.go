package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/glebglazov/pop/history"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/ui"
	"github.com/spf13/cobra"
)

var worktreeCmd = &cobra.Command{
	Use:   "worktree",
	Short: "Select a git worktree in the current repository",
	Long: `Opens a fuzzy picker to select a git worktree.
Must be run from within a git repository.

Keybindings:
  enter    - switch to worktree (prints path or switches tmux session)
  ctrl-d   - delete worktree
  ctrl-x   - force delete worktree
  ctrl-n   - create new worktree
  esc      - cancel

Example tmux binding:
  bind-key P display-popup -E -w 60% -h 60% 'cd "$(pop worktree)" && exec $SHELL'`,
	RunE: runWorktree,
}

var switchSession bool

func init() {
	worktreeCmd.Flags().BoolVarP(&switchSession, "switch", "s", false, "Switch tmux session instead of printing path")
	rootCmd.AddCommand(worktreeCmd)
}

func runWorktree(cmd *cobra.Command, args []string) error {
	// Detect repo context
	ctx, err := project.DetectRepoContext()
	if err != nil {
		return fmt.Errorf("not in a git repository")
	}

	for {
		result, err := showWorktreePicker(ctx)
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

		case ui.ActionNew:
			if err := createWorktree(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to create worktree: %v\n", err)
			}
			return nil // Exit after create

		case ui.ActionReset:
			if result.Selected != nil {
				hist, _ := history.Load(history.DefaultHistoryPath())
				hist.Remove(result.Selected.Path)
				hist.Save()
			}
			// Continue loop to show picker again
		}
	}
}

func showWorktreePicker(ctx *project.RepoContext) (ui.Result, error) {
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

	// Convert to UI items
	items := make([]ui.Item, len(sortedWorktrees))
	for i, wt := range sortedWorktrees {
		items[i] = ui.Item{
			Name:    wt.Name,
			Path:    wt.Path,
			Context: wt.Branch,
		}
	}

	return ui.Run(items,
		ui.WithDelete(),
		ui.WithNew(),
		ui.WithContext(),
		ui.WithCursorAtEnd(),
		ui.WithReset(),
	)
}

func handleWorktreeSelect(ctx *project.RepoContext, item *ui.Item) error {
	// Record selection in history (paths from git are already canonical)
	hist, _ := history.Load(history.DefaultHistoryPath())
	hist.Record(item.Path)
	hist.Save()

	if switchSession {
		return switchTmuxSession(ctx, item)
	}
	// Print path for shell integration
	fmt.Println(item.Path)
	return nil
}

func switchTmuxSession(ctx *project.RepoContext, item *ui.Item) error {
	sessionName := project.TmuxSessionName(ctx, item.Name)

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

func deleteWorktree(path string, force bool) {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)

	cmd := exec.Command("git", args...)
	output, err := cmd.CombinedOutput()

	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to delete worktree: %s\n%s\n", path, output)
	} else {
		fmt.Fprintf(os.Stderr, "Deleted: %s\n", path)
	}
}

func createWorktree(ctx *project.RepoContext) error {
	// This is a simplified version - can be expanded later
	// For now, print instructions
	fmt.Println("Creating new worktree...")
	fmt.Println("Use: git worktree add <path> <branch>")
	return nil
}
