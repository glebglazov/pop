package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/debug"
	"github.com/glebglazov/pop/history"
	"github.com/glebglazov/pop/internal/deps"
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
	paneSetStatusCmd.Flags().String("source", "", "source identifier for filtering (e.g. tmux-global)")
	paneCmd.AddCommand(paneStatusCmd)
}

// resolveSession returns the tmux session name to operate on.
// If --project is set, derives session name from path and ensures session exists.
// Otherwise uses the current tmux session.
func resolveSession() (string, error) {
	return resolveSessionWith(defaultTmux)
}

func resolveSessionWith(tmux deps.Tmux) (string, error) {
	if paneProject != "" {
		name := sanitizeSessionName(filepath.Base(paneProject))
		_, err := tmux.Command("has-session", "-t="+name)
		if err != nil {
			_, err := tmux.Command("new-session", "-ds", name, "-c", paneProject)
			if err != nil {
				return "", fmt.Errorf("failed to create session %q: %w", name, err)
			}
		}
		return name, nil
	}
	session := currentTmuxSessionWith(tmux)
	if session == "" {
		return "", fmt.Errorf("not inside a tmux session (use --project to target one)")
	}
	return session, nil
}

// findPane finds a pane by title in the given session's "agent" window.
// Returns the pane_id (e.g., "%5") or error if not found.
func findPane(session, name string) (string, error) {
	return findPaneWith(defaultTmux, session, name)
}

func findPaneWith(tmux deps.Tmux, session, name string) (string, error) {
	out, err := tmux.Command("list-panes", "-t", session+":agent", "-F", "#{pane_title}|#{pane_id}")
	if err != nil {
		return "", fmt.Errorf("no agent window in session %q", session)
	}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "|", 2)
		if len(parts) == 2 && parts[0] == name {
			return parts[1], nil
		}
	}
	return "", fmt.Errorf("pane %q not found in session %q", name, session)
}

// hasAgentWindow checks if the "agent" window exists in the given session.
func hasAgentWindow(session string) bool {
	return hasAgentWindowWith(defaultTmux, session)
}

func hasAgentWindowWith(tmux deps.Tmux, session string) bool {
	out, err := tmux.Command("list-windows", "-t", session, "-F", "#{window_name}")
	if err != nil {
		debug.Error("hasAgentWindow %s: %v", session, err)
	}
	for _, w := range strings.Split(out, "\n") {
		if w == "agent" {
			return true
		}
	}
	return false
}

// isPaneDead checks if a pane's process has exited.
func isPaneDead(paneID string) bool {
	return isPaneDeadWith(defaultTmux, paneID)
}

func isPaneDeadWith(tmux deps.Tmux, paneID string) bool {
	out, err := tmux.Command("display-message", "-t", paneID, "-p", "#{pane_dead}")
	if err != nil {
		return false
	}
	return out == "1"
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
	return runPaneCreateWith(defaultTmux, args[0], args[1])
}

func runPaneCreateWith(tmux deps.Tmux, name, command string) error {
	session, err := resolveSessionWith(tmux)
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
	if existingID, err := findPaneWith(tmux, session, name); err == nil {
		if !isPaneDeadWith(tmux, existingID) {
			fmt.Println(existingID)
			return nil
		}
		if _, err := tmux.Command("kill-pane", "-t", existingID); err != nil {
			debug.Error("pane create: kill dead pane %s: %v", existingID, err)
		}
	}

	// Create pane with an interactive shell (no command) in the project
	// directory. The shell's rc files run, which triggers direnv and any
	// other hooks so environment variables are loaded before the command.
	var paneID string
	if !hasAgentWindowWith(tmux, session) {
		out, err := tmux.Command("new-window", "-d", "-P", "-F", "#{pane_id}", "-t", session, "-n", "agent", "-c", dir)
		if err != nil {
			return fmt.Errorf("failed to create agent window: %w", err)
		}
		paneID = out
	} else {
		out, err := tmux.Command("split-window", "-d", "-P", "-F", "#{pane_id}", "-t", session+":agent", "-c", dir)
		if err != nil {
			return fmt.Errorf("failed to create pane: %w", err)
		}
		paneID = out
		if _, err := tmux.Command("select-layout", "-t", session+":agent", "tiled"); err != nil {
			debug.Error("pane create: select-layout: %v", err)
		}
	}

	if _, err := tmux.Command("select-pane", "-t", paneID, "-T", name); err != nil {
		return fmt.Errorf("failed to set pane title: %w", err)
	}
	if _, err := tmux.Command("set-option", "-p", "-t", paneID, "remain-on-exit", "on"); err != nil {
		debug.Error("pane create: set remain-on-exit %s: %v", paneID, err)
	}

	// Send the command to the shell after it has initialized
	if _, err := tmux.Command("send-keys", "-t", paneID, command, "Enter"); err != nil {
		debug.Error("pane create: send-keys %s: %v", paneID, err)
	}

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
	return runPaneKillWith(defaultTmux, args[0])
}

func runPaneKillWith(tmux deps.Tmux, name string) error {
	session, err := resolveSessionWith(tmux)
	if err != nil {
		return err
	}

	paneID, err := findPaneWith(tmux, session, name)
	if err != nil {
		return err
	}

	if _, err := tmux.Command("kill-pane", "-t", paneID); err != nil {
		return fmt.Errorf("failed to kill pane %q: %w", name, err)
	}

	// Re-tile remaining panes if agent window still exists
	if _, err := tmux.Command("select-layout", "-t", session+":agent", "tiled"); err != nil {
		debug.Error("pane kill: select-layout: %v", err)
	}

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
	return runPaneListWith(defaultTmux)
}

func runPaneListWith(tmux deps.Tmux) error {
	session, err := resolveSessionWith(tmux)
	if err != nil {
		return err
	}

	out, err := tmux.Command("list-panes", "-t", session+":agent", "-F", "#{pane_title}\t#{pane_id}")
	if err != nil {
		return fmt.Errorf("no agent window in session %q", session)
	}

	fmt.Println(out)
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
	return runPaneSendWith(defaultTmux, args[0], args[1:])
}

func runPaneSendWith(tmux deps.Tmux, name string, keys []string) error {
	session, err := resolveSessionWith(tmux)
	if err != nil {
		return err
	}

	paneID, err := findPaneWith(tmux, session, name)
	if err != nil {
		return err
	}

	tmuxArgs := append([]string{"send-keys", "-t", paneID}, keys...)
	if _, err := tmux.Command(tmuxArgs...); err != nil {
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
	return runPaneCaptureWith(defaultTmux, args[0])
}

func runPaneCaptureWith(tmux deps.Tmux, name string) error {
	session, err := resolveSessionWith(tmux)
	if err != nil {
		return err
	}

	paneID, err := findPaneWith(tmux, session, name)
	if err != nil {
		return err
	}

	out, err := tmux.Command("capture-pane", "-p", "-S", "-50", "-t", paneID)
	if err != nil {
		return fmt.Errorf("failed to capture pane %q: %w", name, err)
	}

	fmt.Println(out)
	return nil
}

// --- set-status ---

var paneSetStatusCmd = &cobra.Command{
	Use:   "set-status [pane_id] <status>",
	Short: "Set pane monitoring status",
	Long: `Set the monitoring status of a tmux pane.

If pane_id is omitted, uses $TMUX_PANE from the environment.
The pane is auto-registered on the first call with any status.

Valid statuses: working, needs_attention, idle.
"read" is accepted as a deprecated alias for "idle".

State transitions:
  working         → needs_attention   Agent stopped or sent a notification
  working         → idle              User has seen the output / agent calm
  needs_attention → idle              User has seen the output
  needs_attention → working           Agent resumed work
  idle            → working           Agent resumed work
  idle            → needs_attention   Agent has output

Auto-registration:
  If the pane is not yet tracked, it is auto-registered on the first
  call with an "agentic" status (working or needs_attention). A call
  with status "idle" on an untracked pane is a no-op — idle is only
  used by agent extensions for housekeeping and by the tmux-global
  auto-read hook, neither of which should register brand-new panes
  in the dashboard. Only panes that an extension has actively claimed
  via working/needs_attention qualify as "agentic" and are displayed.

  The new entry is seeded with LastVisited=now so it sorts to the
  bottom of its status group in the dashboard (closest to the cursor).

Special behavior:
  When [pane_monitoring] dismiss_attention_in_active_pane = true,
  if the pane is currently active (visible to the user) and the
  requested status is needs_attention, it is downgraded to idle
  automatically — the user is already looking at it.`,
	Args:   cobra.RangeArgs(1, 2),
	Hidden: true,
	RunE:   runPaneSetStatus,
}

func runPaneSetStatus(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(config.DefaultConfigPath())
	if err != nil {
		debug.Error("pane set-status: load config: %v", err)
	}
	if cfg == nil {
		cfg = &config.Config{}
	}
	source, _ := cmd.Flags().GetString("source")
	return runPaneSetStatusWith(defaultTmux, cfg, source, args)
}

func runPaneSetStatusWith(tmux deps.Tmux, cfg *config.Config, source string, args []string) error {
	debug.Init()
	defer debug.Close()

	var paneID string
	var rawStatus string
	if len(args) == 2 {
		paneID = args[0]
		rawStatus = args[1]
	} else {
		paneID = os.Getenv("TMUX_PANE")
		rawStatus = args[0]
	}
	// Normalize the deprecated "read" alias to the canonical "idle".
	status := monitor.PaneStatus(rawStatus)
	if status == "read" {
		status = monitor.StatusIdle
	}
	if source != "" && cfg.ShouldIgnoreStatusFrom(source) {
		return nil
	}

	if paneID == "" {
		return nil
	}

	// idle is high-frequency (agent integrations call it eagerly on plugin
	// load and on every session.idle / agent_end event) — skip logging to
	// avoid noise. Only log when we actually change state (below).
	if status != monitor.StatusIdle {
		debug.Log("[set-status] %s: invoked with %s", paneID, status)
	}

	statePath := monitor.DefaultStatePath()
	state, err := monitor.Load(statePath)
	if err != nil {
		debug.Error("pane set-status: load state: %v", err)
		return nil // called from hook
	}

	entry, ok := state.Panes[paneID]
	if !ok {
		// Only "agentic" statuses (working / needs_attention) register new
		// panes in the monitor state. idle is deliberately a no-op for
		// untracked panes:
		//   - The tmux-global auto-read hook fires on every pane navigation
		//     and would otherwise pollute the dashboard with every pane the
		//     user visits (showing them all as idle).
		//   - Agent extensions (opencode, pi) eagerly send idle on plugin
		//     load as housekeeping to clear stale "working" from crashed
		//     runs; they rely on this no-op to avoid skewing the dashboard.
		// The dashboard should only surface panes that an extension has
		// actively claimed by sending working or needs_attention.
		if status == monitor.StatusIdle {
			return nil
		}
		// Auto-register: look up the tmux session for this pane.
		session, err := tmuxPaneSessionWith(tmux, paneID)
		if err != nil {
			debug.Error("[set-status] %s: failed to look up session, skipping: %v", paneID, err)
			return nil
		}
		debug.Log("[set-status] %s: auto-registering in session=%s with status=%s", paneID, session, status)
		now := time.Now()
		state.Panes[paneID] = &monitor.PaneEntry{
			PaneID:    paneID,
			Session:   session,
			Status:    status,
			UpdatedAt: now,
			// Seed LastVisited so the pane sorts to the bottom of its
			// status group (closest to the cursor) on first appearance,
			// rather than the top (farthest from the cursor) which is
			// what an unset zero-value would do under the ascending
			// sort in cmd/dashboard.go:sortDashboardPanes.
			LastVisited: now,
		}
		return state.Save()
	}

	// For idle: always record the visit. Unlike the old "read" semantics
	// (which short-circuited and refused to transition working → read),
	// the merged idle should actually update the status field too — this
	// is what lets agent integrations clear a stale "working" status left
	// over from a crashed previous run by calling setStatus("idle") on
	// plugin load.
	visitedNow := false
	if status == monitor.StatusIdle {
		entry.LastVisited = time.Now()
		visitedNow = true
	}

	// If configured, treat needs_attention as idle when the user is already
	// looking at the pane — they can see the output in real time.
	if cfg.DismissAttentionInActivePane() && status == monitor.StatusNeedsAttention && isActiveTmuxPaneWith(tmux, paneID) {
		debug.Log("[set-status] %s: needs_attention on active pane — downgrading to idle", paneID)
		status = monitor.StatusIdle
	}

	if entry.Status == status {
		// Status unchanged. If we just updated LastVisited, still persist
		// the change so the dashboard sort sees the new visit time.
		if visitedNow {
			return state.Save()
		}
		return nil
	}

	debug.Log("[set-status] %s (session=%s): %s → %s", paneID, entry.Session, entry.Status, status)
	entry.Status = status
	entry.UpdatedAt = time.Now()
	return state.Save()
}

// --- status ---

var paneStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show monitor state for all tracked panes",
	Args:  cobra.NoArgs,
	RunE:  runPaneStatus,
}

func runPaneStatus(cmd *cobra.Command, args []string) error {
	state := loadMonitorState()
	if state == nil {
		fmt.Fprintln(os.Stderr, "monitor daemon is not running")
		return nil
	}

	entries := state.PanesAll()
	if len(entries) == 0 {
		fmt.Println("no tracked panes")
		return nil
	}

	// Also load pop history for session_last_visit_at
	hist, err := history.Load(history.DefaultHistoryPath())
	if err != nil {
		debug.Error("pane status: load history: %v", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "PANE\tSESSION\tSTATUS\tFOLLOWING\tUPDATED_AT\tPANE_LAST_VISITED\tSESSION_LAST_VISIT")
	for _, entry := range entries {
		lastVisited := "-"
		if !entry.LastVisited.IsZero() {
			lastVisited = entry.LastVisited.Format("2006-01-02 15:04:05")
		}
		sessionVisit := "-"
		if ts := sessionAccessTime(entry.Session, hist); ts > 0 {
			sessionVisit = time.Unix(ts, 0).Format("2006-01-02 15:04:05")
		}
		following := ""
		if entry.Following {
			following = "yes"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			entry.PaneID,
			entry.Session,
			entry.Status,
			following,
			entry.UpdatedAt.Format("2006-01-02 15:04:05"),
			lastVisited,
			sessionVisit,
		)
	}
	return w.Flush()
}
