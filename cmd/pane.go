package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var paneProject string

var paneCmd = &cobra.Command{
	Use:   "pane",
	Short: "Manage named tmux panes",
	Long: `Manage named tmux panes in a shared "agent" window.

Designed for agentic workflows where agents need to create, find,
send commands to, and read output from named panes.

All subcommands accept --project <path> to target a specific project's
tmux session. Without it, operates on the current tmux session.`,
}

func init() {
	rootCmd.AddCommand(paneCmd)
	paneCmd.PersistentFlags().StringVar(&paneProject, "project", "", "Target project path (uses its tmux session)")
	paneCmd.AddCommand(paneCreateCmd)
	paneCmd.AddCommand(paneKillCmd)
	paneCmd.AddCommand(paneFindCmd)
	paneCmd.AddCommand(paneListCmd)
	paneCmd.AddCommand(paneSendCmd)
	paneCmd.AddCommand(paneCaptureCmd)
}

// resolveSession returns the tmux session name to operate on.
// If --project is set, derives session name from path and ensures session exists.
// Otherwise uses the current tmux session.
func resolveSession() (string, error) {
	if paneProject != "" {
		name := sanitizeSessionName(filepath.Base(paneProject))
		if err := exec.Command("tmux", "has-session", "-t="+name).Run(); err != nil {
			if err := exec.Command("tmux", "new-session", "-ds", name, "-c", paneProject).Run(); err != nil {
				return "", fmt.Errorf("failed to create session %q: %w", name, err)
			}
		}
		return name, nil
	}
	session := currentTmuxSession()
	if session == "" {
		return "", fmt.Errorf("not inside a tmux session (use --project to target one)")
	}
	return session, nil
}

// findPane finds a pane by title in the given session's "agent" window.
// Returns the pane_id (e.g., "%5") or error if not found.
func findPane(session, name string) (string, error) {
	out, err := exec.Command("tmux", "list-panes", "-t", session+":agent", "-F", "#{pane_title}|#{pane_id}").Output()
	if err != nil {
		return "", fmt.Errorf("no agent window in session %q", session)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(line, "|", 2)
		if len(parts) == 2 && parts[0] == name {
			return parts[1], nil
		}
	}
	return "", fmt.Errorf("pane %q not found in session %q", name, session)
}

// hasAgentWindow checks if the "agent" window exists in the given session.
func hasAgentWindow(session string) bool {
	out, _ := exec.Command("tmux", "list-windows", "-t", session, "-F", "#{window_name}").Output()
	for _, w := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if w == "agent" {
			return true
		}
	}
	return false
}

// isPaneDead checks if a pane's process has exited.
func isPaneDead(paneID string) bool {
	out, err := exec.Command("tmux", "display-message", "-t", paneID, "-p", "#{pane_dead}").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "1"
}

// --- create ---

var paneCreateCmd = &cobra.Command{
	Use:   "create <name> <command>",
	Short: "Create a named pane in the agent window",
	Args:  cobra.ExactArgs(2),
	RunE:  runPaneCreate,
}

func runPaneCreate(cmd *cobra.Command, args []string) error {
	name, command := args[0], args[1]

	session, err := resolveSession()
	if err != nil {
		return err
	}

	// Determine project directory so the pane's shell starts there,
	// allowing direnv and other shell hooks to initialize naturally.
	dir := paneProject
	if dir == "" {
		dir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get working directory: %w", err)
		}
	}

	// If pane with this name exists and is alive, return it
	// If it exists but is dead, kill it and recreate below
	if existingID, err := findPane(session, name); err == nil {
		if !isPaneDead(existingID) {
			fmt.Println(existingID)
			return nil
		}
		exec.Command("tmux", "kill-pane", "-t", existingID).Run()
	}

	// Create pane with an interactive shell (no command) in the project
	// directory. The shell's rc files run, which triggers direnv and any
	// other hooks so environment variables are loaded before the command.
	var paneID string
	if !hasAgentWindow(session) {
		out, err := exec.Command("tmux", "new-window", "-d", "-P", "-F", "#{pane_id}", "-t", session, "-n", "agent", "-c", dir).Output()
		if err != nil {
			return fmt.Errorf("failed to create agent window: %w", err)
		}
		paneID = strings.TrimSpace(string(out))
	} else {
		out, err := exec.Command("tmux", "split-window", "-d", "-P", "-F", "#{pane_id}", "-t", session+":agent", "-c", dir).Output()
		if err != nil {
			return fmt.Errorf("failed to create pane: %w", err)
		}
		paneID = strings.TrimSpace(string(out))
		exec.Command("tmux", "select-layout", "-t", session+":agent", "tiled").Run()
	}

	if err := exec.Command("tmux", "select-pane", "-t", paneID, "-T", name).Run(); err != nil {
		return fmt.Errorf("failed to set pane title: %w", err)
	}
	exec.Command("tmux", "set-option", "-p", "-t", paneID, "remain-on-exit", "on").Run()

	// Send the command to the shell after it has initialized
	exec.Command("tmux", "send-keys", "-t", paneID, command, "Enter").Run()

	fmt.Println(paneID)
	return nil
}

// --- kill ---

var paneKillCmd = &cobra.Command{
	Use:   "kill <name>",
	Short: "Kill a named pane",
	Args:  cobra.ExactArgs(1),
	RunE:  runPaneKill,
}

func runPaneKill(cmd *cobra.Command, args []string) error {
	name := args[0]

	session, err := resolveSession()
	if err != nil {
		return err
	}

	paneID, err := findPane(session, name)
	if err != nil {
		return err
	}

	if err := exec.Command("tmux", "kill-pane", "-t", paneID).Run(); err != nil {
		return fmt.Errorf("failed to kill pane %q: %w", name, err)
	}

	// Re-tile remaining panes if agent window still exists
	exec.Command("tmux", "select-layout", "-t", session+":agent", "tiled").Run()

	return nil
}

// --- find ---

var paneFindCmd = &cobra.Command{
	Use:   "find <name>",
	Short: "Find a named pane and print its pane ID",
	Args:  cobra.ExactArgs(1),
	RunE:  runPaneFind,
}

func runPaneFind(cmd *cobra.Command, args []string) error {
	name := args[0]

	session, err := resolveSession()
	if err != nil {
		return err
	}

	paneID, err := findPane(session, name)
	if err != nil {
		return err
	}

	fmt.Println(paneID)
	return nil
}

// --- list ---

var paneListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all named panes in the agent window",
	Args:  cobra.NoArgs,
	RunE:  runPaneList,
}

func runPaneList(cmd *cobra.Command, args []string) error {
	session, err := resolveSession()
	if err != nil {
		return err
	}

	out, err := exec.Command("tmux", "list-panes", "-t", session+":agent", "-F", "#{pane_title}\t#{pane_id}").Output()
	if err != nil {
		return fmt.Errorf("no agent window in session %q", session)
	}

	fmt.Print(string(out))
	return nil
}

// --- send ---

var paneSendCmd = &cobra.Command{
	Use:   "send <name> <keys...>",
	Short: "Send literal keys to a named pane",
	Long: `Send literal keys to a named pane via tmux send-keys.

Keys are passed directly to tmux. Examples:
  pop pane send server "npm run dev" Enter   # type command and press Enter
  pop pane send server C-c                   # send Ctrl+C
  pop pane send server q                     # send literal "q"`,
	Args: cobra.MinimumNArgs(2),
	RunE: runPaneSend,
}

func runPaneSend(cmd *cobra.Command, args []string) error {
	name := args[0]
	keys := args[1:]

	session, err := resolveSession()
	if err != nil {
		return err
	}

	paneID, err := findPane(session, name)
	if err != nil {
		return err
	}

	tmuxArgs := append([]string{"send-keys", "-t", paneID}, keys...)
	if err := exec.Command("tmux", tmuxArgs...).Run(); err != nil {
		return fmt.Errorf("failed to send keys to pane %q: %w", name, err)
	}
	return nil
}

// --- capture ---

var paneCaptureCmd = &cobra.Command{
	Use:   "capture <name>",
	Short: "Capture and print pane content",
	Args:  cobra.ExactArgs(1),
	RunE:  runPaneCapture,
}

func runPaneCapture(cmd *cobra.Command, args []string) error {
	name := args[0]

	session, err := resolveSession()
	if err != nil {
		return err
	}

	paneID, err := findPane(session, name)
	if err != nil {
		return err
	}

	out, err := exec.Command("tmux", "capture-pane", "-p", "-e", "-S", "-500", "-t", paneID).Output()
	if err != nil {
		return fmt.Errorf("failed to capture pane %q: %w", name, err)
	}

	fmt.Print(string(out))
	return nil
}
