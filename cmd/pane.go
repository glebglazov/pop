package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/debug"
	"github.com/glebglazov/pop/history"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/monitor"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/session"
	"github.com/spf13/cobra"
)

var paneProject string

// paneOnSocketSendFailed is invoked when a daemon socket send fails so the
// next call can reach a running daemon. Tests may replace it to observe the hook.
var paneOnSocketSendFailed = func() { go ensureMonitorDaemon() }

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
	paneSetStatusCmd.Flags().Bool("no-register", false, "only update already-tracked panes, never auto-register new ones")
	paneSetStatusCmd.Flags().String("label", "", "display label for dashboard (e.g. cursor, claude); overrides tmux pane_current_command")
	paneCmd.AddCommand(paneSetTopicCmd)
	paneSetTopicCmd.Flags().Bool("clear", false, "clear the pane's topic")
	paneSetTopicCmd.Flags().Bool("derive", false, "derive the topic from an agent hook payload read on stdin (e.g. Claude Code UserPromptSubmit)")
	paneSetTopicCmd.Flags().String("label", "", "agent whose hook payload is on stdin (claude, codex, cursor, pi, opencode); selects the payload adapter for --derive")
	paneCmd.AddCommand(paneStatusCmd)
	paneCmd.AddCommand(paneFollowCmd)
	paneCmd.AddCommand(paneUnfollowCmd)
	paneCmd.AddCommand(paneVisitCmd)
}

// resolveSession returns the tmux session name to operate on.
// If --project is set, derives session name from path and ensures session exists.
// Otherwise uses the current tmux session.
func resolveSession() (string, error) {
	return resolveSessionWith(defaultTmux)
}

func resolveSessionWith(tmux deps.Tmux) (string, error) {
	if paneProject != "" {
		name := project.SessionName(paneProject)
		if err := session.EnsureWith(sessionDeps(tmux), name, paneProject); err != nil {
			return "", err
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

var paneSendPaneID string

var paneSendCmd = &cobra.Command{
	Use:   "send [--pane-id <pane_id>] <name> <keys...>",
	Short: "Send literal keys to a pane",
	Long: `Send literal keys to a named pane or explicit tmux pane ID via tmux send-keys.

Each argument after <name> is passed as a separate key to tmux. Keys
are NOT auto-terminated with Enter — include it explicitly if needed.

Examples:
  pop pane send server "npm run dev" Enter   # type command and press Enter
  pop pane send --pane-id %63 "hello" Enter  # send to an explicit tmux pane ID
  pop pane send server C-c                   # send Ctrl+C (interrupt)
  pop pane send server C-d                   # send Ctrl+D (EOF)
  pop pane send server C-l                   # send Ctrl+L (clear screen)
  pop pane send server q                     # send literal "q"
  pop pane send server Up Enter              # re-run last command

Tmux special key names: Enter, Escape, Space, Tab, Up, Down, Left,
Right, BSpace, DC (delete), End, Home, IC (insert), NPage (page down),
PPage (page up), F1-F12, C-<key> (ctrl), M-<key> (alt).`,
	Args: cobra.MinimumNArgs(1),
	RunE: runPaneSend,
}

func runPaneSend(cmd *cobra.Command, args []string) error {
	if paneSendPaneID != "" {
		return runPaneSendToPaneIDWith(defaultTmux, paneSendPaneID, args)
	}
	if len(args) < 2 {
		return fmt.Errorf("requires a pane name and at least one key")
	}
	return runPaneSendWith(defaultTmux, args[0], args[1:])
}

func init() {
	paneSendCmd.Flags().StringVar(&paneSendPaneID, "pane-id", "", "Target an explicit tmux pane ID instead of a named pane")
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

func runPaneSendToPaneIDWith(tmux deps.Tmux, paneID string, keys []string) error {
	if paneID == "" {
		return fmt.Errorf("--pane-id requires a pane ID")
	}
	if len(keys) == 0 {
		return fmt.Errorf("--pane-id requires at least one key")
	}
	tmuxArgs := append([]string{"send-keys", "-t", paneID}, keys...)
	if _, err := tmux.Command(tmuxArgs...); err != nil {
		return fmt.Errorf("failed to send keys to pane ID %q: %w", paneID, err)
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

Valid statuses: working, unread, clear.
"needs_attention" is accepted as a deprecated alias for "unread".
"idle" and "read" are accepted as deprecated aliases for "clear".

State transitions:
  working → unread    Agent stopped or sent a notification
  working → clear     User has seen the output / agent calm
  unread  → clear     User has seen the output
  unread  → working   Agent resumed work
  clear   → working   Agent resumed work
  clear   → unread    Agent has output

Auto-registration:
  If the pane is not yet tracked, it is auto-registered on the first
  call. The new entry is seeded with LastActiveAt=now so it sorts to the
  bottom of its status group in the dashboard (closest to the cursor).

  Use --label to override the dashboard display (e.g. --label cursor).
  By default the label comes from tmux pane_current_command, which is
  often misleading for Node-based agents (shows "node" instead of the
  agent name).

  Callers that do not want to register new panes (e.g. tmux-global
  auto-clear hooks) should pass --no-register.

Special behavior:
  When [pane_monitoring] dismiss_unread_in_active_pane = true,
  if the pane is currently active (visible to the user) and the
  requested status is unread, it is downgraded to clear automatically
  — the user is already looking at it.`,
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
	noRegister, _ := cmd.Flags().GetBool("no-register")
	label, _ := cmd.Flags().GetString("label")
	return runPaneSetStatusWith(defaultTmux, cfg, source, noRegister, label, args)
}

func runPaneSetStatusWith(tmux deps.Tmux, cfg *config.Config, source string, noRegister bool, label string, args []string) error {
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

	if paneID == "" {
		return nil
	}

	// Client-side source filtering — cheap check, avoids socket round-trip.
	if source != "" && cfg.ShouldIgnoreStatusFrom(source) {
		return nil
	}

	// TCP server is opt-in via [pane_monitoring] tcp_server. When disabled,
	// skip the dial entirely and write state directly — no daemon round-trip,
	// no "connection refused" fallback noise in the debug log.
	if !cfg.PaneMonitoringTCPServer() {
		return runPaneSetStatusDirect(tmux, cfg, paneID, rawStatus, source, noRegister, label)
	}

	req := monitor.Request{
		Cmd:        "set-status",
		PaneID:     paneID,
		Status:     rawStatus,
		Label:      label,
		Source:     source,
		NoRegister: noRegister,
	}

	addr := monitorAddr(cfg)
	resp, err := monitor.SendRequest(addr, req)
	if err != nil {
		debug.Error("pane set-status: socket send failed, falling back to direct write: %v", err)
		// Ensure daemon is starting for next call.
		paneOnSocketSendFailed()
		return runPaneSetStatusDirect(tmux, cfg, paneID, rawStatus, source, noRegister, label)
	}

	if !resp.OK {
		debug.Error("pane set-status: daemon error: %s", resp.Error)
	}
	return nil
}

// runPaneSetStatusDirect is the fallback path used when the daemon socket
// is unavailable (cold start).
func runPaneSetStatusDirect(tmux deps.Tmux, cfg *config.Config, paneID, rawStatus, source string, noRegister bool, label string) error {
	status := monitor.NormalizeStatus(rawStatus)

	if status != monitor.StatusClear {
		debug.Log("[set-status] %s: invoked with %s (direct)", paneID, status)
	}

	store := monitor.NewStore(monitor.DefaultStatePath(), nil)
	err := store.ReportStatus(tmux, monitor.ReportStatusInput{
		PaneID:                paneID,
		Status:                status,
		Label:                 label,
		NoRegister:            noRegister,
		DismissUnreadInActive: cfg.DismissUnreadInActivePane(),
	})
	if err != nil {
		debug.Error("pane set-status: %v", err)
	}
	return err
}

// --- set-topic ---

var paneSetTopicCmd = &cobra.Command{
	Use:   "set-topic [pane_id] <text>",
	Short: "Set a pane's topic",
	Long: `Set the topic of a tmux pane — a short, machine-set phrase
describing what the pane's conversation is about.

The topic is stored as the per-pane tmux user-option @pop_topic (set via
'set-option -p', readable as '#{@pop_topic}' from any tmux format), which is
its single source of truth (ADR 0058). It is not kept in monitor state.

If pane_id is omitted, uses $TMUX_PANE from the environment. A leading
pane_id is recognised by its '%' prefix; everything after it is the topic
text (joined with spaces if passed as multiple words). Pass --clear to wipe
the topic.

Pass --derive to read an agent hook payload on stdin and set a derived topic.
Use --label to name the agent whose payload is on stdin (claude, codex, cursor,
pi, opencode); each agent's prompt-submit hook delivers the prompt differently,
so the label selects the matching payload adapter. pop owns the Topic recipes
(ADR 0057): when [pane_monitoring] topic_agents is configured (e.g.
["claude", "ollama:llama3.2"]) pop builds a model prompt and runs each recipe
in order, using the first non-empty result — any nonzero exit / error / empty /
timeout falls through reason-blind to the next recipe. pop runs no model itself
and reads no keys; each recipe shells out to an already-authed CLI. With no
recipes configured the user's prompt is truncated. Either way the result is
normalized into a lowercase kebab slug of at most [pane_monitoring] topic_words
words (default 5) before it is written. A missing/unparseable payload, an agent
whose hook exposes no prompt text, or every recipe failing with no prior topic
falls back to truncation; a non-empty existing topic is never clobbered.

Derive runs once per pane (ADR 0025): a pane whose @pop_topic is already
non-empty is left untouched. The option dies with the pane, so retiring or
restarting the pane re-derives, and --clear is a free manual refresh.

The topic shows in the dashboard's descriptive parenthetical, dimmed to
mark it machine-derived. A user-authored note always overrides it.`,
	Args:   cobra.ArbitraryArgs,
	Hidden: true,
	RunE:   runPaneSetTopic,
}

func runPaneSetTopic(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(config.DefaultConfigPath())
	if err != nil {
		debug.Error("pane set-topic: load config: %v", err)
	}
	if cfg == nil {
		cfg = &config.Config{}
	}
	clear, _ := cmd.Flags().GetBool("clear")
	derive, _ := cmd.Flags().GetBool("derive")
	label, _ := cmd.Flags().GetString("label")

	if derive {
		paneID, topic, ok := deriveTopic(cmd.InOrStdin(), args, cfg, label)
		if !ok {
			// Missing/unparseable payload, empty prompt, an existing Topic to
			// keep, or a command failure: no-op so we never clobber it.
			return nil
		}
		return setPaneTopicOption(defaultTmux, paneID, topic)
	}

	paneID, topic, err := parseSetTopicArgs(clear, args)
	if err != nil {
		return err
	}
	return setPaneTopicOption(defaultTmux, paneID, topic)
}

// deriveTopic reads an agent hook payload from r and resolves a Topic. The
// optional leading pane_id (a '%' prefixed arg) overrides $TMUX_PANE. label
// names the agent so the right payload adapter is chosen. It uses the
// production prev-Topic lookup and command runner; see deriveTopicWith for the
// injectable core.
func deriveTopic(r io.Reader, args []string, cfg *config.Config, label string) (paneID, topic string, ok bool) {
	return deriveTopicWith(r, args, cfg, label, defaultTopicOptionLookup, runTopicRecipe)
}

// prevTopicLookup returns the pane's current Topic (its @pop_topic user-option,
// the single source of truth — ADR 0058) and its session. The Topic gates the
// once-per-pane guard; the session rides through into the command payload.
type prevTopicLookup func(paneID string) (prevTopic, session string)

// topicRecipeRunner runs one Topic recipe: it executes argv (with optional
// stdin) under the context's deadline and returns the process stdout. A non-nil
// error covers a non-zero exit and a timeout (via the context). Recipes shell
// out to an already-authenticated CLI — pop makes no model call of its own.
type topicRecipeRunner func(ctx context.Context, argv []string, stdin []byte) (string, error)

// topicRecipePayload is the per-derive JSON contract pop builds internally and
// feeds to recipes that want it (the "cmd:" escape hatch receives it on stdin —
// ADR 0024's shape, preserved). prompt/pane_id/session/prev_topic are always
// present; transcript_path rides only when the agent's hook exposed one.
type topicRecipePayload struct {
	PrevTopic      string `json:"prev_topic"`
	Prompt         string `json:"prompt"`
	TranscriptPath string `json:"transcript_path,omitempty"`
	PaneID         string `json:"pane_id"`
	Session        string `json:"session"`
}

// deriveTopicWith is the injectable core of the derive path. It first applies
// the once-per-pane guard (ADR 0025/0058): a pane whose @pop_topic is already
// non-empty is a no-op, so the Topic the option holds is kept and a user note
// overrides it in display. On a fresh pane with prompt text it generates a
// Topic via pop-owned recipes (ADR 0057): it builds a model prompt and runs
// each configured topic_agents recipe in order, normalizing each result into a
// kebab slug and using the first non-empty one. Any nonzero exit / error /
// empty / timeout falls through reason-blind to the next recipe. If every recipe
// fails (or none are configured) it falls back to truncating the prompt, so a
// Topic always resolves and the agent is never blocked. The caller writes the
// result to @pop_topic.
func deriveTopicWith(r io.Reader, args []string, cfg *config.Config, label string, lookup prevTopicLookup, run topicRecipeRunner) (paneID, topic string, ok bool) {
	paneID = os.Getenv("TMUX_PANE")
	if len(args) > 0 && strings.HasPrefix(args[0], "%") {
		paneID = args[0]
	}

	data, err := io.ReadAll(r)
	if err != nil {
		debug.Error("pane set-topic --derive: read stdin: %v", err)
		return "", "", false
	}
	prompt, transcriptPath, err := parseTopicPayload(data, label)
	if err != nil {
		debug.Error("pane set-topic --derive: %v", err)
		return "", "", false
	}

	// Derive once per pane (ADR 0025/0058): the Topic lives in @pop_topic, which
	// dies with the pane. A pane that already has a non-empty @pop_topic never
	// re-derives — every prompt after the first is a no-op that keeps it.
	prevTopic, session := lookup(paneID)
	if prevTopic != "" {
		return "", "", false
	}

	// No prompt text exposed: degrade to no Topic and run no recipe (a model
	// call on an empty prompt is pointless), never clobbering an existing Topic.
	if strings.TrimSpace(prompt) == "" {
		return "", "", false
	}

	// The Topic format is a pop-owned contract (ADR 0057): whatever a recipe (or
	// the truncation fallback) produces is normalized into a kebab slug before it
	// reaches @pop_topic.
	maxWords := cfg.PaneMonitoringTopicWords()
	recipeTimeout := cfg.PaneMonitoringTopicDerivationTimeout()

	// Build the model prompt (instructing a <=maxWords-word lowercase hyphen
	// slug) and the per-derive JSON payload once; both are reused across recipes.
	modelPrompt := buildTopicModelPrompt(prompt, maxWords)
	payload, err := json.Marshal(topicRecipePayload{
		PrevTopic:      prevTopic,
		Prompt:         prompt,
		TranscriptPath: transcriptPath,
		PaneID:         paneID,
		Session:        session,
	})
	if err != nil {
		debug.Error("pane set-topic --derive: marshal payload: %v", err)
		payload = nil
	}

	// Run each recipe in order; first non-empty (after normalization) wins.
	// Failures fall through reason-blind: pop never branches on the error shape.
	for _, ref := range cfg.PaneMonitoringTopicAgents() {
		recipe, ok := resolveTopicRecipe(ref)
		if !ok {
			debug.Log("pane set-topic --derive: unknown topic recipe %q — skipping", ref)
			continue
		}
		argv, stdin := recipe.build(modelPrompt, payload)
		ctx, cancel := context.WithTimeout(context.Background(), recipeTimeout)
		out, runErr := run(ctx, argv, stdin)
		cancel()
		if runErr != nil {
			debug.Error("pane set-topic --derive: recipe %q failed: %v", ref, runErr)
			continue
		}
		// parse extracts the result text (JSON for agents that emit it, plain
		// otherwise); capTopic first-lines and char-caps it, then slugifyTopic
		// normalizes to the kebab contract — uniform across every recipe.
		if derived := slugifyTopic(capTopic(recipe.parse(out)), maxWords); derived != "" {
			debug.Log("pane set-topic --derive: recipe %q set topic %q on pane %s", ref, derived, paneID)
			return paneID, derived, true
		}
		debug.Log("pane set-topic --derive: recipe %q produced no usable topic", ref)
	}

	// Every recipe failed (or none configured), and prevTopic is empty here (the
	// once-guard returns early otherwise), so fall back to truncation; that
	// truncated Topic then freezes the pane.
	topic = slugifyTopic(truncateTopic(prompt), maxWords)
	if topic == "" {
		return "", "", false
	}
	debug.Log("pane set-topic --derive: truncation fallback set topic %q on pane %s", topic, paneID)
	return paneID, topic, true
}

// topicRecipe is one curated agent-CLI invocation pop knows how to run (ADR
// 0057). build returns the argv to exec and the bytes to feed on stdin (the
// model prompt for curated agents, the JSON payload for the "cmd:" escape
// hatch). parse extracts the result text from the CLI's stdout, handling
// structured JSON for agents that emit it — pop reads only the result text and
// never branches on the error shape.
type topicRecipe struct {
	build func(modelPrompt string, payload []byte) (argv []string, stdin []byte)
	parse func(stdout string) string
}

// defaultOllamaModel is the local model the bare "ollama" recipe runs when the
// reference carries no "ollama:<model>" argument.
const defaultOllamaModel = "llama3.2"

// resolveTopicRecipe maps a topic_agents reference to its curated recipe. The
// reference is "<name>" or "<name>:<arg>": "claude" and "ollama[:<model>]" are
// curated agent CLIs; "cmd:<shell command>" (alias "sh:") is the documented
// escape hatch for any other CLI, run via `sh -c` with the JSON payload on
// stdin. An unknown name (or an empty escape-hatch command) returns ok=false so
// the caller skips it reason-blind.
func resolveTopicRecipe(ref string) (topicRecipe, bool) {
	name, arg, _ := strings.Cut(strings.TrimSpace(ref), ":")
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "claude":
		return claudeRecipe(), true
	case "ollama":
		model := strings.TrimSpace(arg)
		if model == "" {
			model = defaultOllamaModel
		}
		return ollamaRecipe(model), true
	case "cmd", "sh":
		command := strings.TrimSpace(arg)
		if command == "" {
			return topicRecipe{}, false
		}
		return shellRecipe(command), true
	default:
		return topicRecipe{}, false
	}
}

// claudeRecipe runs `claude -p --output-format json`, feeding the model prompt
// on stdin and extracting only the structured result text from its JSON stdout.
func claudeRecipe() topicRecipe {
	return topicRecipe{
		build: func(modelPrompt string, _ []byte) ([]string, []byte) {
			return []string{"claude", "-p", "--output-format", "json"}, []byte(modelPrompt)
		},
		parse: parseClaudeResult,
	}
}

// parseClaudeResult extracts the "result" field from `claude -p --output-format
// json` output. pop reads only the result text — it does not branch on is_error
// or any other field (ADR 0057). Non-JSON or a missing result yields "", which
// the caller treats as empty and falls through to the next recipe.
func parseClaudeResult(stdout string) string {
	var r struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &r); err != nil {
		debug.Log("pane set-topic --derive: claude recipe output not JSON: %v", err)
		return ""
	}
	return r.Result
}

// ollamaRecipe runs `ollama run <model>` against a local model, feeding the
// model prompt on stdin; the plain-text stdout is the result (the caller
// first-lines, caps, and slugifies it).
func ollamaRecipe(model string) topicRecipe {
	return topicRecipe{
		build: func(modelPrompt string, _ []byte) ([]string, []byte) {
			return []string{"ollama", "run", model}, []byte(modelPrompt)
		},
		parse: plainTopicResult,
	}
}

// shellRecipe is the "cmd:" escape hatch: an arbitrary CLI run via `sh -c`,
// receiving the per-derive JSON payload on stdin (ADR 0024's contract). Its
// stdout is plain text — the caller first-lines, caps, and slugifies it.
func shellRecipe(command string) topicRecipe {
	return topicRecipe{
		build: func(_ string, payload []byte) ([]string, []byte) {
			return []string{"sh", "-c", command}, payload
		},
		parse: plainTopicResult,
	}
}

// plainTopicResult is the parse step for recipes whose stdout is the result
// text verbatim (no structured wrapper). The caller applies capTopic/slugify.
func plainTopicResult(stdout string) string { return stdout }

// buildTopicModelPrompt wraps the user's prompt with instructions for the model
// to reply with a lowercase, <=maxWords-word hyphen slug and nothing else. The
// output is normalized regardless (slugifyTopic), so this is a quality nudge,
// not a contract pop relies on.
func buildTopicModelPrompt(prompt string, maxWords int) string {
	return fmt.Sprintf(`Name the topic of this coding session.
Reply with ONLY a short label: lowercase, at most %d words, words joined by
single hyphens, no other punctuation, no quotes, and no explanation.

The user's message:
%s`, maxWords, prompt)
}

// topicPayloadAdapter maps one agent's "user submitted prompt" hook payload
// (JSON on stdin) into the prompt text and an optional transcript_path that
// `set-topic --derive` consumes. Each integrated agent delivers the prompt
// under a different shape, so derivation picks an adapter by the --label passed
// on the hook command — the same per-agent variance set-status carries via
// --label. A non-nil error is reserved for malformed JSON; a well-formed
// payload that exposes no prompt text returns an empty prompt (the caller then
// degrades to no Topic, never an error). transcript_path is forwarded only by
// agents that expose one — pop never parses the transcript itself (ADR 0024).
type topicPayloadAdapter func(data []byte) (prompt, transcriptPath string, err error)

// topicPayloadAdapters maps an agent label to its payload adapter. The empty
// label is treated as Claude (the unlabeled default, preserving slice-04
// behavior). Agents whose prompt-submit hook exposes no prompt text are absent
// here and resolve to a degrade adapter.
var topicPayloadAdapters = map[string]topicPayloadAdapter{
	"":         claudeTopicPayload,
	"claude":   claudeTopicPayload,
	"codex":    promptOnlyTopicPayload,
	"cursor":   promptOnlyTopicPayload,
	"pi":       promptOnlyTopicPayload,
	"opencode": promptOnlyTopicPayload,
}

// parseTopicPayload selects the adapter for label and extracts the prompt and
// optional transcript_path from the agent's hook payload.
func parseTopicPayload(data []byte, label string) (prompt, transcriptPath string, err error) {
	adapter, ok := topicPayloadAdapters[strings.ToLower(label)]
	if !ok {
		// Unknown label: an agent we don't have an adapter for. Degrade to no
		// Topic rather than erroring, so a future agent's hook never breaks.
		debug.Log("pane set-topic --derive: no payload adapter for label %q — degrading to no Topic", label)
		return "", "", nil
	}
	return adapter(data)
}

// claudeTopicPayload parses Claude Code's UserPromptSubmit payload, the only
// integrated agent that exposes a transcript_path.
func claudeTopicPayload(data []byte) (prompt, transcriptPath string, err error) {
	var payload struct {
		Prompt         string `json:"prompt"`
		TranscriptPath string `json:"transcript_path"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", "", fmt.Errorf("parse claude payload: %w", err)
	}
	return payload.Prompt, payload.TranscriptPath, nil
}

// promptOnlyTopicPayload parses the {"prompt": "..."} shape shared by agents
// whose prompt-submit hook exposes the text but no transcript: codex's and
// cursor's hook JSON, and the pi/opencode extensions, which serialize the
// submitted message into this shape themselves. transcript_path is
// deliberately not read — these agents don't provide one, so it stays out of
// the recipe payload contract. A payload with no prompt field yields an empty
// prompt and the caller degrades silently.
func promptOnlyTopicPayload(data []byte) (prompt, transcriptPath string, err error) {
	var payload struct {
		Prompt string `json:"prompt"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", "", fmt.Errorf("parse payload: %w", err)
	}
	return payload.Prompt, "", nil
}

// defaultTopicOptionLookup reads a pane's current Topic and session from tmux
// using the production tmux dependency. The Topic is the @pop_topic user-option
// (the single source of truth — ADR 0058), read alongside the session name.
func defaultTopicOptionLookup(paneID string) (prevTopic, session string) {
	return topicOptionLookup(defaultTmux, paneID)
}

// topicOptionLookup reads @pop_topic and the session name off a pane in one
// tmux call. A read error or a pane that can't be found yields empties, so a
// fresh (or gone) pane derives. The format puts the tab-free session name
// first, so a Topic containing tabs still round-trips intact.
func topicOptionLookup(tmux deps.Tmux, paneID string) (prevTopic, session string) {
	if paneID == "" {
		return "", ""
	}
	out, err := tmux.Command("display-message", "-p", "-t", paneID, "#{session_name}\t#{@pop_topic}")
	if err != nil {
		debug.Error("pane set-topic --derive: read @pop_topic: %v", err)
		return "", ""
	}
	line := strings.TrimRight(out, "\n")
	parts := strings.SplitN(line, "\t", 2)
	session = parts[0]
	if len(parts) == 2 {
		prevTopic = parts[1]
	}
	return prevTopic, session
}

// runTopicRecipe is the production topicRecipeRunner: it execs a recipe's argv
// (with optional stdin) under the context's deadline and returns its stdout. A
// non-zero exit or a timeout surfaces as a non-nil error, which the caller
// treats reason-blind. This is the single exec point for every recipe — curated
// agent CLIs and the `sh -c` escape hatch alike — so pop links no model SDK.
func runTopicRecipe(ctx context.Context, argv []string, stdin []byte) (string, error) {
	if len(argv) == 0 {
		return "", fmt.Errorf("empty recipe argv")
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	if len(stdin) > 0 {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return out.String(), nil
}

// capTopic trims the command's stdout, keeps the first line only, and caps it
// at topicMaxChars runes (matching truncation), appending an ellipsis when cut.
func capTopic(out string) string {
	line := out
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	line = strings.TrimSpace(line)
	if utf8.RuneCountInString(line) > topicMaxChars {
		runes := []rune(line)
		line = strings.TrimRight(string(runes[:topicMaxChars]), " ") + "…"
	}
	return line
}

const (
	topicMaxWords = 8
	topicMaxChars = 60
)

// slugifyTopic normalizes derived Topic text into pop's canonical format (ADR
// 0057): a lowercase kebab slug of at most maxWords words. It lowercases, treats
// every non-alphanumeric rune (punctuation and whitespace alike) as a separator
// so extra spacing collapses, keeps the first maxWords words, and joins them
// with "-". Empty, whitespace-only, or punctuation-only input yields "". This is
// the single normalizer reused by the derive path here and by later slices
// (recipe output, drain pre-seed), so it is a standalone function.
func slugifyTopic(text string, maxWords int) string {
	if maxWords < 1 {
		maxWords = 1
	}
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	if len(words) > maxWords {
		words = words[:maxWords]
	}
	return strings.Join(words, "-")
}

// truncateTopic collapses whitespace and trims the prompt to the first
// ~topicMaxWords words / ~topicMaxChars runes, appending an ellipsis when it
// cuts. An empty or whitespace-only prompt yields "".
func truncateTopic(prompt string) string {
	fields := strings.Fields(prompt)
	if len(fields) == 0 {
		return ""
	}

	cut := false
	if len(fields) > topicMaxWords {
		fields = fields[:topicMaxWords]
		cut = true
	}
	collapsed := strings.Join(fields, " ")

	if utf8.RuneCountInString(collapsed) > topicMaxChars {
		runes := []rune(collapsed)
		collapsed = strings.TrimRight(string(runes[:topicMaxChars]), " ")
		cut = true
	}

	if cut {
		return collapsed + "…"
	}
	return collapsed
}

// parseSetTopicArgs resolves the optional leading pane_id (recognised by its
// '%' prefix) and the topic text. Without --clear a topic is required; with
// --clear any trailing text is ignored.
func parseSetTopicArgs(clear bool, args []string) (paneID, topic string, err error) {
	rest := args
	if len(rest) > 0 && strings.HasPrefix(rest[0], "%") {
		paneID = rest[0]
		rest = rest[1:]
	} else {
		paneID = os.Getenv("TMUX_PANE")
	}
	if clear {
		return paneID, "", nil
	}
	if len(rest) == 0 {
		return "", "", fmt.Errorf("set-topic requires topic text (or --clear)")
	}
	return paneID, strings.Join(rest, " "), nil
}

// setPaneTopicOption writes (or clears) a pane's Topic to the per-pane tmux
// user-option @pop_topic — the single source of truth for the Topic (ADR 0058),
// referenceable as #{@pop_topic} from any tmux format. An empty topic clears
// the option. Unlike the old monitor-state path this writes straight to tmux:
// no daemon, no socket, no registration — tmux serializes its own option
// writes, and the option dies with the pane (the ADR 0025 lifecycle).
func setPaneTopicOption(tmux deps.Tmux, paneID, topic string) error {
	debug.Init()
	defer debug.Close()

	if paneID == "" {
		return nil
	}
	if _, err := tmux.Command("set-option", "-p", "-t", paneID, "@pop_topic", topic); err != nil {
		debug.Error("pane set-topic: set @pop_topic on %s: %v", paneID, err)
		return err
	}
	return nil
}

// preSeedTopicFromTitle returns the drain pre-seed hook (ADR 0058): at drain
// spawn pop slugifies the task Title into the canonical Topic format (the same
// slugifyTopic normalizer recipe-derived Topics use — ADR 0057) and writes it to
// the current pane's @pop_topic. Because the once-per-pane guard reads
// #{@pop_topic}, the agent's `set-topic --derive` hook then sees the option
// already set and no-ops — a drained pane gets an accurate Topic with zero model
// calls and zero recipe runs, while non-drain panes still fall through to the
// recipe chain.
//
// It guards on the existing option so the first task in a whole-set drain wins
// (matching the once-per-pane derive guard) and a later task never clobbers it;
// an empty/punctuation-only Title that slugs to "" is left untouched. A pane
// outside tmux ($TMUX_PANE unset) is a silent no-op.
func preSeedTopicFromTitle(tmux deps.Tmux, maxWords int) func(taskTitle string) {
	return func(taskTitle string) {
		paneID := os.Getenv("TMUX_PANE")
		if paneID == "" {
			return
		}
		// Once per pane: a pane that already carries a Topic (a prior task in this
		// drain, or a manual set) is never re-seeded.
		if prev, _ := topicOptionLookup(tmux, paneID); prev != "" {
			return
		}
		slug := slugifyTopic(taskTitle, maxWords)
		if slug == "" {
			return
		}
		_ = setPaneTopicOption(tmux, paneID, slug)
	}
}

// --- status ---

var paneStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show monitor state for all tracked panes",
	Args:  cobra.NoArgs,
	RunE:  runPaneStatus,
}

// ensurePaneStatusDaemon makes `pane status` self-healing: unlike Doctor
// (read-only by contract), status starts the daemon if it is not answering and
// waits briefly so the same invocation reports an accurate, live result.
func ensurePaneStatusDaemon(cfg *config.Config) {
	if !cfg.PaneMonitoringTCPServer() {
		if !monitor.IsDaemonRunning(monitor.DefaultPIDPath()) {
			fmt.Fprintln(os.Stderr, "monitor daemon not running — starting…")
			ensureMonitorDaemon()
		}
		return
	}
	addr := monitorAddr(cfg)
	if _, err := monitor.Handshake(addr); err == nil {
		return // already answering
	}
	fmt.Fprintln(os.Stderr, "monitor daemon not running — starting…")
	ensureMonitorDaemon()
	if waitForDaemon(addr, 2*time.Second) == nil {
		fmt.Fprintf(os.Stderr,
			"monitor daemon did not come up at %s (port may be held by another process)\n", addr)
	}
}

func runPaneStatus(cmd *cobra.Command, args []string) error {
	cfg := loadConfigQuietly()
	ensurePaneStatusDaemon(cfg)

	// Read state from disk regardless of daemon status — ensurePaneStatusDaemon
	// has already started one if it could.
	state := loadMonitorStateAlways()
	if state == nil || len(state.PanesAll()) == 0 {
		fmt.Println("no tracked panes")
		return nil
	}

	entries := state.PanesAll()

	// Also load pop history for session_last_visit_at
	hist, err := history.Load(history.DefaultHistoryPath())
	if err != nil {
		debug.Error("pane status: load history: %v", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "PANE\tSESSION\tSTATUS\tFOLLOWING\tUPDATED_AT\tPANE_LAST_ACTIVE_AT\tSESSION_LAST_VISIT")
	for _, entry := range entries {
		lastActiveAt := "-"
		if !entry.LastActiveAt.IsZero() {
			lastActiveAt = entry.LastActiveAt.Format("2006-01-02 15:04:05")
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
			lastActiveAt,
			sessionVisit,
		)
	}
	return w.Flush()
}

// --- follow / unfollow ---

var paneFollowCmd = &cobra.Command{
	Use:   "follow <name|pane_id>",
	Short: "Mark a pane as followed",
	Long: `Mark a tracked pane as followed.

Followed panes show up in pop's "following" attention view (toggle with F
in the picker). If the argument starts with '%' it is treated as a tmux
pane_id; otherwise it is resolved as a pane name in the agent window of
the current session (or --project's session).

Untracked panes are auto-registered as clear.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPaneSetFollow(args[0], true)
	},
}

var paneUnfollowCmd = &cobra.Command{
	Use:   "unfollow <name|pane_id>",
	Short: "Clear the followed mark on a pane",
	Long: `Clear the followed mark on a tracked pane.

Also clears any user note attached to the pane, matching the picker's
behavior. Unfollowing an untracked pane is a no-op.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPaneSetFollow(args[0], false)
	},
}

func runPaneSetFollow(arg string, follow bool) error {
	cfg, err := config.Load(config.DefaultConfigPath())
	if err != nil {
		debug.Error("pane follow: load config: %v", err)
	}
	if cfg == nil {
		cfg = &config.Config{}
	}
	return runPaneSetFollowWith(defaultTmux, cfg, arg, follow)
}

func runPaneSetFollowWith(tmux deps.Tmux, cfg *config.Config, arg string, follow bool) error {
	debug.Init()
	defer debug.Close()

	paneID, err := resolvePaneArg(tmux, arg)
	if err != nil {
		return err
	}

	// Daemon path when TCP server is enabled — keeps writes serialized
	// with set-status under the daemon's mutex. Fall through to a direct
	// write on connect failure (cold start), the same pattern set-status
	// uses.
	if cfg.PaneMonitoringTCPServer() {
		resp, err := monitor.SendRequest(monitorAddr(cfg), monitor.Request{
			Cmd:       "set-following",
			PaneID:    paneID,
			Following: &follow,
		})
		if err == nil {
			if !resp.OK {
				return fmt.Errorf("%s", resp.Error)
			}
			return nil
		}
		debug.Error("pane follow: socket send failed, falling back to direct write: %v", err)
		paneOnSocketSendFailed()
	}

	return runPaneSetFollowDirect(tmux, paneID, follow)
}

// runPaneSetFollowDirect is the fallback path used when the daemon socket
// is unavailable (cold start).
func runPaneSetFollowDirect(tmux deps.Tmux, paneID string, follow bool) error {
	store := monitor.NewStore(monitor.DefaultStatePath(), nil)
	return store.SetFollowing(tmux, paneID, follow)
}

// --- visit ---

var paneVisitCmd = &cobra.Command{
	Use:   "visit [pane_id]",
	Short: "Record a visit to a tracked pane",
	Long: `Record that the user has visited a tracked pane.

Updates the pane's LastActiveAt timestamp in the monitor state.
Untracked panes are silently ignored (no auto-registration).

If pane_id is omitted, uses $TMUX_PANE from the environment.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runPaneVisit,
}

func runPaneVisit(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(config.DefaultConfigPath())
	if err != nil {
		debug.Error("pane visit: load config: %v", err)
	}
	if cfg == nil {
		cfg = &config.Config{}
	}
	return runPaneVisitWith(defaultTmux, cfg, args)
}

func runPaneVisitWith(tmux deps.Tmux, cfg *config.Config, args []string) error {
	debug.Init()
	defer debug.Close()

	paneID := os.Getenv("TMUX_PANE")
	if len(args) > 0 {
		paneID = args[0]
	}
	if paneID == "" {
		return nil
	}

	// TCP server is opt-in via [pane_monitoring] tcp_server.
	if !cfg.PaneMonitoringTCPServer() {
		return runPaneVisitDirect(paneID)
	}

	resp, err := monitor.SendRequest(monitorAddr(cfg), monitor.Request{
		Cmd:    "visit",
		PaneID: paneID,
	})
	if err != nil {
		debug.Error("pane visit: socket send failed, falling back to direct write: %v", err)
		paneOnSocketSendFailed()
		return runPaneVisitDirect(paneID)
	}

	if !resp.OK {
		debug.Error("pane visit: daemon error: %s", resp.Error)
	}
	return nil
}

// runPaneVisitDirect is the fallback path when the daemon socket is
// unavailable. Updates LastActiveAt only for already-tracked panes.
func runPaneVisitDirect(paneID string) error {
	store := monitor.DefaultStore()
	return store.RecordVisit(paneID)
}

// resolvePaneArg accepts a tmux pane_id ("%N") verbatim, or a pane name to
// look up in the current/--project session's agent window. Mirrors the
// kill/send/capture pattern but admits raw pane IDs for use from scripts
// that already know them.
func resolvePaneArg(tmux deps.Tmux, arg string) (string, error) {
	if strings.HasPrefix(arg, "%") {
		return arg, nil
	}
	session, err := resolveSessionWith(tmux)
	if err != nil {
		return "", err
	}
	return findPaneWith(tmux, session, arg)
}
