package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/glebglazov/pop/debug"
	"github.com/glebglazov/pop/monitor"
	"github.com/spf13/cobra"
)

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
	monitorCmd.AddCommand(monitorSetStatusCmd)
	monitorCmd.AddCommand(monitorMarkReadCmd)
	monitorCmd.AddCommand(monitorHookSetupCmd)
	monitorDeregisterCmd.Flags().BoolVar(&deregisterAll, "all", false, "Deregister all panes")
}

// --- hook-setup ---

var monitorHookSetupCmd = &cobra.Command{
	Use:   "hook-setup",
	Short: "Print Claude Code hook configuration for monitoring",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println(`Add the following to ~/.claude/settings.json:

{
  "hooks": {
    "PreToolUse": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "pop monitor set-status $TMUX_PANE working 2>/dev/null || true"
          }
        ]
      }
    ],
    "Notification": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "pop monitor set-status $TMUX_PANE needs_attention 2>/dev/null || true"
          }
        ]
      }
    ],
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "pop monitor set-status $TMUX_PANE needs_attention 2>/dev/null || true"
          }
        ]
      }
    ],
    "UserPromptSubmit": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "pop monitor set-status $TMUX_PANE working 2>/dev/null || true"
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
	// No-op: kept for backward compatibility with existing hook configurations.
	// Registration now happens lazily in set-status when a pane is first seen.
	return nil
}

// tmuxPaneSession returns the session name for a given pane ID
func tmuxPaneSession(paneID string) (string, error) {
	out, err := exec.Command("tmux", "display-message", "-t", paneID, "-p", "#{session_name}").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// isActiveTmuxPane returns true if the given pane is the currently active pane
// in the currently attached client.
func isActiveTmuxPane(paneID string) bool {
	out, err := exec.Command("tmux", "display-message", "-p", "#{pane_id}").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == paneID
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

// --- set-status ---

var monitorSetStatusCmd = &cobra.Command{
	Use:    "set-status <pane_id> <status>",
	Short:  "Set pane status (called by Claude Code hooks)",
	Args:   cobra.ExactArgs(2),
	Hidden: true,
	RunE:   runMonitorSetStatus,
}

func runMonitorSetStatus(cmd *cobra.Command, args []string) error {
	debug.Init()
	defer debug.Close()

	paneID := args[0]
	if paneID == "" {
		return nil
	}

	status := monitor.PaneStatus(args[1])
	debug.Log("[set-status] %s: hook invoked with %s", paneID, status)

	// If the pane is currently active, don't mark it as needs_attention —
	// the user is already looking at it.
	if status == monitor.StatusNeedsAttention && isActiveTmuxPane(paneID) {
		debug.Log("[set-status] %s: skipping needs_attention — pane is active", paneID)
		return nil
	}

	statePath := monitor.DefaultStatePath()
	state, err := monitor.Load(statePath)
	if err != nil {
		return nil // silently ignore — called from hook
	}

	entry, ok := state.Panes[paneID]
	if !ok {
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

	if entry.Status == status {
		return nil // no change
	}

	debug.Log("[set-status] %s (session=%s): %s → %s", paneID, entry.Session, entry.Status, status)
	entry.Status = status
	entry.UpdatedAt = time.Now()
	return state.Save()
}


// --- mark-read ---

var monitorMarkReadCmd = &cobra.Command{
	Use:    "mark-read <pane_id>",
	Short:  "Mark a pane as read (called by tmux hook)",
	Args:   cobra.ExactArgs(1),
	Hidden: true,
	RunE:   runMonitorMarkRead,
}

func runMonitorMarkRead(cmd *cobra.Command, args []string) error {
	debug.Init()
	defer debug.Close()

	statePath := monitor.DefaultStatePath()
	state, err := monitor.Load(statePath)
	if err != nil {
		return nil // silently ignore — called from tmux hook
	}

	entry, ok := state.Panes[args[0]]
	if !ok {
		return nil
	}

	if entry.Status == monitor.StatusNeedsAttention {
		debug.Log("[mark-read] %s (session=%s): needs_attention → read", args[0], entry.Session)
		entry.Status = monitor.StatusRead
		return state.Save()
	}
	return nil
}

// --- start ---

// tmux hooks for mark-read: event name → hook command
var tmuxMarkReadHooks = map[string]string{
	"after-select-pane":      `run-shell "pop monitor mark-read #{pane_id} 2>/dev/null || true"`,
	"session-window-changed": `run-shell "pop monitor mark-read #{pane_id} 2>/dev/null || true"`,
	"client-session-changed": `run-shell "pop monitor mark-read #{pane_id} 2>/dev/null || true"`,
}

var monitorStartCmd = &cobra.Command{
	Use:    "start",
	Short:  "Start the monitoring daemon (foreground)",
	Args:   cobra.NoArgs,
	Hidden: true,
	RunE:   runMonitorStart,
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

// installTmuxMarkReadHooks removes any existing pop hooks and installs current ones.
func installTmuxMarkReadHooks() {
	uninstallTmuxMarkReadHooks()
	for event, hookCmd := range tmuxMarkReadHooks {
		exec.Command("tmux", "set-hook", "-ga", event, hookCmd).Run()
	}
}

// uninstallTmuxMarkReadHooks removes all pop-related tmux hooks,
// leaving other hooks intact. Parses indexed entries like "event[0] cmd".
func uninstallTmuxMarkReadHooks() {
	out, _ := exec.Command("tmux", "show-hooks", "-g").Output()
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "pop monitor") {
			continue
		}
		// Line format: "event[N] command..."
		bracketEnd := strings.Index(line, "]")
		if bracketEnd == -1 {
			continue
		}
		indexed := line[:bracketEnd+1]
		exec.Command("tmux", "set-hook", "-gu", indexed).Run()
	}
}

// ensureMonitorDaemon ensures a monitor daemon is running with the current binary.
// Restarts if the binary is newer than the running daemon.
// Called automatically by `pop select`.
func ensureMonitorDaemon() {
	pidPath := monitor.DefaultPIDPath()
	exe, err := os.Executable()
	if err != nil {
		return
	}

	if monitor.IsDaemonRunning(pidPath) {
		if !binaryNewerThanPID(exe, pidPath) {
			return // daemon is up to date
		}
		// Signal old daemon to stop; it will clean up its PID file on exit
		_ = monitor.StopDaemon(pidPath)
	}

	// Wait for old PID file to be released (up to 500ms)
	for range 10 {
		if !monitor.IsDaemonRunning(pidPath) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	cmd := exec.Command(exe, "monitor", "start")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	_ = cmd.Start()
	if cmd.Process != nil {
		_ = cmd.Process.Release()
	}
}

// binaryNewerThanPID returns true if the binary was modified after the PID file was written
func binaryNewerThanPID(exePath, pidPath string) bool {
	exeInfo, err := os.Stat(exePath)
	if err != nil {
		return true
	}
	pidInfo, err := os.Stat(pidPath)
	if err != nil {
		return true
	}
	return exeInfo.ModTime().After(pidInfo.ModTime())
}

// --- stop ---

var monitorStopCmd = &cobra.Command{
	Use:    "stop",
	Short:  "Stop the monitoring daemon",
	Args:   cobra.NoArgs,
	Hidden: true,
	RunE:   runMonitorStop,
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
		fmt.Printf("  %s  session=%s  status=%s  updated=%s\n",
			entry.PaneID, entry.Session, entry.Status,
			entry.UpdatedAt.Format("15:04:05"))
	}
	return nil
}
