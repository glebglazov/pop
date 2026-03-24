package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebglazov/pop/debug"
	"github.com/glebglazov/pop/monitor"
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
	paneCmd.AddCommand(paneSetStatusCmd)
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
	Long: `Create a named pane running the given command in the "agent" window.

The pane starts an interactive shell in the project directory (respecting
direnv and other shell hooks), then sends the command to it.

Behavior:
  - Idempotent: if a pane with <name> is already running, prints its ID
  - Auto-recreate: if the pane exists but its command has exited, kills
    it and creates a fresh one
  - Background: does not steal focus from your current window
  - Remain-on-exit: pane stays open after the command finishes so you
    can read its output

Uses tmux new-window/split-window to create panes, select-pane -T to
set the title, and send-keys to dispatch the command after the shell
initializes.`,
	Args: cobra.ExactArgs(2),
	RunE: runPaneCreate,
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
	Long: `Kill the named pane in the agent window.

Remaining panes are automatically re-tiled. If this is the last pane,
the agent window is destroyed.

Uses tmux kill-pane to destroy the pane and select-layout tiled to
rebalance the remaining panes.`,
	Args: cobra.ExactArgs(1),
	RunE: runPaneKill,
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
	Long: `Find a pane by name and print its tmux pane ID (e.g., %5).

Returns a non-zero exit code if the pane doesn't exist, so you can use
this to check whether a pane is running:

  pop pane find server && echo "running" || echo "not found"

Uses tmux list-panes with #{pane_title} to match panes by name in the
agent window.`,
	Args: cobra.ExactArgs(1),
	RunE: runPaneFind,
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
	Long: `List all panes in the agent window as tab-separated lines:

  <title>\t<pane_id>

Example output:
  server	%5
  db	%6
  logs	%7

Uses tmux list-panes with #{pane_title} and #{pane_id} format variables.`,
	Args: cobra.NoArgs,
	RunE: runPaneList,
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

Each argument after <name> is passed as a separate key to tmux. Keys
are NOT auto-terminated with Enter — include it explicitly if needed.

Examples:
  pop pane send server "npm run dev" Enter   # type command and press Enter
  pop pane send server C-c                   # send Ctrl+C (interrupt)
  pop pane send server C-d                   # send Ctrl+D (EOF)
  pop pane send server C-l                   # send Ctrl+L (clear screen)
  pop pane send server q                     # send literal "q"
  pop pane send server Up Enter              # re-run last command

Tmux special key names: Enter, Escape, Space, Tab, Up, Down, Left,
Right, BSpace, DC (delete), End, Home, IC (insert), NPage (page down),
PPage (page up), F1-F12, C-<key> (ctrl), M-<key> (alt).`,
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
	Long: `Capture the named pane's content and print it to stdout.

Includes the visible screen plus 50 lines of scrollback history.
ANSI color codes are stripped for clean, token-efficient output.

Works on both live and dead panes (remain-on-exit keeps the content
available after the command exits).

Uses tmux capture-pane with -S -50 (scrollback).`,
	Args: cobra.ExactArgs(1),
	RunE: runPaneCapture,
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

	out, err := exec.Command("tmux", "capture-pane", "-p", "-S", "-50", "-t", paneID).Output()
	if err != nil {
		return fmt.Errorf("failed to capture pane %q: %w", name, err)
	}

	fmt.Print(string(out))
	return nil
}

// --- set-status ---

var paneSetStatusCmd = &cobra.Command{
	Use:   "set-status [pane_id] <status>",
	Short: "Set pane monitoring status",
	Long: `Set the monitoring status of a tmux pane.

If pane_id is omitted, uses $TMUX_PANE from the environment.
The pane is auto-registered on first status change.

Valid statuses: working, needs_attention, read.

State transitions:
  working → needs_attention   Agent stopped or sent a notification
  working → read              No-op (already not flagged)
  needs_attention → read      User has seen the output
  needs_attention → working   Agent resumed work
  read → working              Agent resumed work

Auto-registration:
  If the pane is not yet tracked, it is auto-registered on the first
  call with working or needs_attention. A read status on an untracked
  pane is a no-op — there is nothing to mark as read.

Special behavior:
  If the pane is currently active (visible to the user) and the
  requested status is needs_attention, it is downgraded to read
  automatically — the user is already looking at it.`,
	Args:   cobra.RangeArgs(1, 2),
	Hidden: true,
	RunE:   runPaneSetStatus,
}

func runPaneSetStatus(cmd *cobra.Command, args []string) error {
	debug.Init()
	defer debug.Close()

	var paneID string
	var status monitor.PaneStatus
	if len(args) == 2 {
		paneID = args[0]
		status = monitor.PaneStatus(args[1])
	} else {
		paneID = os.Getenv("TMUX_PANE")
		status = monitor.PaneStatus(args[0])
	}
	if paneID == "" {
		return nil
	}

	// read is high-frequency and usually a no-op — skip logging entirely
	// to avoid noise. Only log when we actually change state (below).
	if status != monitor.StatusRead {
		debug.Log("[set-status] %s: invoked with %s", paneID, status)
	}

	// If the user is already looking at the pane, treat needs_attention as
	// read — they can see the output in real time, so there's nothing to flag.
	if status == monitor.StatusNeedsAttention && isActiveTmuxPane(paneID) {
		debug.Log("[set-status] %s: needs_attention on active pane — downgrading to read", paneID)
		status = monitor.StatusRead
	}

	statePath := monitor.DefaultStatePath()
	state, err := monitor.Load(statePath)
	if err != nil {
		return nil // silently ignore — called from hook
	}

	entry, ok := state.Panes[paneID]
	if !ok {
		// Only auto-register for actionable statuses
		if status == monitor.StatusRead {
			return nil
		}
		// Auto-register: look up the tmux session for this pane
		session, err := tmuxPaneSession(paneID)
		if err != nil {
			debug.Log("[set-status] %s: failed to look up session, skipping: %v", paneID, err)
			return nil
		}
		debug.Log("[set-status] %s: auto-registering in session=%s with status=%s", paneID, session, status)
		state.Panes[paneID] = &monitor.PaneEntry{
			PaneID:    paneID,
			Session:   session,
			Status:    status,
			UpdatedAt: time.Now(),
		}
		return state.Save()
	}

	// read only transitions from needs_attention (not from working)
	if status == monitor.StatusRead && entry.Status != monitor.StatusNeedsAttention {
		return nil
	}

	if entry.Status == status {
		return nil // no change
	}

	debug.Log("[set-status] %s (session=%s): %s → %s", paneID, entry.Session, entry.Status, status)
	entry.Status = status
	entry.UpdatedAt = time.Now()
	return state.Save()
}
