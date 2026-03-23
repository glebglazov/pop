package cmd

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/glebglazov/pop/debug"
	"github.com/glebglazov/pop/monitor"
	"github.com/spf13/cobra"
)

var monitorSource string
var deregisterAll bool

var monitorCmd = &cobra.Command{
	Use:   "monitor",
	Short: "Monitor agent panes for attention",
}

func init() {
	rootCmd.AddCommand(monitorCmd)
	monitorCmd.AddCommand(monitorRegisterCmd)
	monitorCmd.AddCommand(monitorStartCmd)
	monitorCmd.AddCommand(monitorStopCmd)
	monitorCmd.AddCommand(monitorStatusCmd)
	monitorCmd.AddCommand(monitorDeregisterCmd)
	monitorCmd.AddCommand(monitorMarkReadCmd)
	monitorCmd.AddCommand(monitorHookSetupCmd)
	monitorRegisterCmd.Flags().StringVar(&monitorSource, "source", "", "Source tool type (e.g., claude-code)")
	monitorRegisterCmd.MarkFlagRequired("source")
	monitorDeregisterCmd.Flags().BoolVar(&deregisterAll, "all", false, "Deregister all panes")
}

// --- hook-setup ---

var monitorHookSetupCmd = &cobra.Command{
	Use:   "hook-setup",
	Short: "Print Claude Code hook configuration for auto-registration",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println(`Add the following to ~/.claude/settings.json:

{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "startup",
        "hooks": [
          {
            "type": "command",
            "command": "pop monitor register $TMUX_PANE --source claude-code 2>/dev/null || true"
          }
        ]
      }
    ]
  }
}`)
		return nil
	},
}

// --- register ---

var monitorRegisterCmd = &cobra.Command{
	Use:   "register <pane_id>",
	Short: "Register a tmux pane for monitoring",
	Args:  cobra.ExactArgs(1),
	RunE:  runMonitorRegister,
}

func runMonitorRegister(cmd *cobra.Command, args []string) error {
	paneID := args[0]
	if paneID == "" {
		return nil
	}

	source := monitor.Source(monitorSource)

	session, err := tmuxPaneSession(paneID)
	if err != nil {
		return fmt.Errorf("failed to determine session for pane %s: %w", paneID, err)
	}

	statePath := monitor.DefaultStatePath()
	state, err := monitor.Load(statePath)
	if err != nil {
		return err
	}

	state.Register(paneID, session, source)
	return state.Save()
}

// tmuxPaneSession returns the session name for a given pane ID
func tmuxPaneSession(paneID string) (string, error) {
	out, err := exec.Command("tmux", "display-message", "-t", paneID, "-p", "#{session_name}").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// --- deregister ---

var monitorDeregisterCmd = &cobra.Command{
	Use:   "deregister [pane_id]",
	Short: "Deregister a pane from monitoring",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runMonitorDeregister,
}

func runMonitorDeregister(cmd *cobra.Command, args []string) error {
	statePath := monitor.DefaultStatePath()
	state, err := monitor.Load(statePath)
	if err != nil {
		return err
	}

	if deregisterAll {
		count := len(state.Panes)
		state.Panes = make(map[string]*monitor.PaneEntry)
		if err := state.Save(); err != nil {
			return err
		}
		fmt.Printf("Deregistered %d pane(s)\n", count)
		return nil
	}

	if len(args) == 0 {
		return fmt.Errorf("provide a pane_id or use --all")
	}

	paneID := args[0]
	if _, ok := state.Panes[paneID]; !ok {
		return fmt.Errorf("pane %s not registered", paneID)
	}

	state.Deregister(paneID)
	if err := state.Save(); err != nil {
		return err
	}
	fmt.Printf("Deregistered %s\n", paneID)
	return nil
}

// --- mark-read ---

var markReadSession string

var monitorMarkReadCmd = &cobra.Command{
	Use:    "mark-read [pane_id]",
	Short:  "Mark a pane or session as read (called by tmux hook)",
	Args:   cobra.MaximumNArgs(1),
	Hidden: true,
	RunE:   runMonitorMarkRead,
}

func init() {
	monitorMarkReadCmd.Flags().StringVar(&markReadSession, "session", "", "Mark all panes in this session as read")
}

func runMonitorMarkRead(cmd *cobra.Command, args []string) error {
	debug.Init()
	defer debug.Close()

	statePath := monitor.DefaultStatePath()
	state, err := monitor.Load(statePath)
	if err != nil {
		return nil // silently ignore — called from tmux hook
	}

	changed := false

	if markReadSession != "" {
		// Mark all panes in the session as read
		for _, entry := range state.Panes {
			if entry.Session == markReadSession && entry.Status == monitor.StatusNeedsAttention {
				debug.Log("[mark-read] session=%s pane=%s: needs_attention → unknown", markReadSession, entry.PaneID)
				entry.Status = monitor.StatusRead
				changed = true
			}
		}
	} else if len(args) > 0 {
		// Mark a specific pane as read
		entry, ok := state.Panes[args[0]]
		if !ok {
			return nil
		}
		if entry.Status == monitor.StatusNeedsAttention {
			debug.Log("[mark-read] pane=%s: needs_attention → unknown", args[0])
			entry.Status = monitor.StatusUnknown
			changed = true
		}
	}

	if changed {
		return state.Save()
	}
	return nil
}

// --- start ---

// tmux hooks for mark-read: event name → hook command
var tmuxMarkReadHooks = map[string]string{
	"after-select-pane":      `run-shell "pop monitor mark-read #{pane_id} 2>/dev/null || true"`,
	"session-window-changed": `run-shell "pop monitor mark-read #{pane_id} 2>/dev/null || true"`,
	"client-session-changed": `run-shell "pop monitor mark-read --session #{session_name} 2>/dev/null || true"`,
}

var monitorStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the monitoring daemon (foreground)",
	Args:  cobra.NoArgs,
	RunE:  runMonitorStart,
}

func runMonitorStart(cmd *cobra.Command, args []string) error {
	pidPath := monitor.DefaultPIDPath()
	if monitor.IsDaemonRunning(pidPath) {
		return fmt.Errorf("daemon is already running (PID file: %s)", pidPath)
	}

	installTmuxMarkReadHooks()

	statePath := monitor.DefaultStatePath()
	return monitor.RunDaemon(statePath, pidPath)
}

// installTmuxMarkReadHooks idempotently adds mark-read hooks to tmux
func installTmuxMarkReadHooks() {
	for event, hookCmd := range tmuxMarkReadHooks {
		out, _ := exec.Command("tmux", "show-hooks", "-g", event).Output()
		if strings.Contains(string(out), "pop monitor mark-read") {
			continue
		}
		exec.Command("tmux", "set-hook", "-ga", event, hookCmd).Run()
	}
}

// --- stop ---

var monitorStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the monitoring daemon",
	Args:  cobra.NoArgs,
	RunE:  runMonitorStop,
}

func runMonitorStop(cmd *cobra.Command, args []string) error {
	pidPath := monitor.DefaultPIDPath()
	return monitor.StopDaemon(pidPath)
}

// --- status ---

var monitorStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show monitor state (debug)",
	Args:  cobra.NoArgs,
	RunE:  runMonitorStatus,
}

func runMonitorStatus(cmd *cobra.Command, args []string) error {
	pidPath := monitor.DefaultPIDPath()
	running := monitor.IsDaemonRunning(pidPath)
	if running {
		fmt.Println("Daemon: running")
	} else {
		fmt.Println("Daemon: stopped")
	}

	statePath := monitor.DefaultStatePath()
	state, err := monitor.Load(statePath)
	if err != nil {
		return err
	}

	if len(state.Panes) == 0 {
		fmt.Println("No monitored panes")
		return nil
	}

	fmt.Printf("\nMonitored panes (%d):\n", len(state.Panes))
	for _, entry := range state.Panes {
		fmt.Printf("  %s  session=%s  source=%s  status=%s  updated=%s\n",
			entry.PaneID, entry.Session, entry.Source, entry.Status,
			entry.UpdatedAt.Format("15:04:05"))
	}
	return nil
}
