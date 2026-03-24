package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/glebglazov/pop/monitor"
	"github.com/spf13/cobra"
)

func init() {
	paneCmd.AddCommand(paneMonitorStartCmd)
	paneCmd.AddCommand(paneMonitorStopCmd)
	paneCmd.AddCommand(paneMonitorStatusCmd)
}

// tmuxPaneSession returns the session name for a given pane ID
func tmuxPaneSession(paneID string) (string, error) {
	out, err := exec.Command("tmux", "display-message", "-t", paneID, "-p", "#{session_name}").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// isActiveTmuxPane returns true if the given pane is visible to the user:
// active in its window, the window is active in its session, and the session
// is attached to a client.
func isActiveTmuxPane(paneID string) bool {
	out, err := exec.Command("tmux", "display-message", "-t", paneID, "-p", "#{pane_active} #{window_active} #{session_attached}").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "1 1 1"
}

// --- monitor-start ---

// tmux hooks for auto-read: event name → hook command
var tmuxAutoReadHooks = map[string]string{
	"after-select-pane":      `run-shell "pop pane set-status #{pane_id} read 2>/dev/null || true"`,
	"session-window-changed": `run-shell "pop pane set-status #{pane_id} read 2>/dev/null || true"`,
	"client-session-changed": `run-shell "pop pane set-status #{pane_id} read 2>/dev/null || true"`,
}

var paneMonitorStartCmd = &cobra.Command{
	Use:    "monitor-start",
	Short:  "Start the pane monitoring daemon (foreground)",
	Args:   cobra.NoArgs,
	Hidden: true,
	RunE:   runPaneMonitorStart,
}

func runPaneMonitorStart(cmd *cobra.Command, args []string) error {
	pidPath := monitor.DefaultPIDPath()
	if monitor.IsDaemonRunning(pidPath) {
		return fmt.Errorf("daemon is already running (PID file: %s)", pidPath)
	}

	installTmuxAutoReadHooks()

	statePath := monitor.DefaultStatePath()
	return monitor.RunDaemon(statePath, pidPath)
}

// installTmuxAutoReadHooks removes any existing pop hooks and installs current ones.
func installTmuxAutoReadHooks() {
	uninstallTmuxAutoReadHooks()
	for event, hookCmd := range tmuxAutoReadHooks {
		exec.Command("tmux", "set-hook", "-ga", event, hookCmd).Run()
	}
}

// uninstallTmuxAutoReadHooks removes all pop-related tmux hooks,
// leaving other hooks intact. Parses indexed entries like "event[0] cmd".
func uninstallTmuxAutoReadHooks() {
	out, _ := exec.Command("tmux", "show-hooks", "-g").Output()
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "pop pane set-status") && !strings.Contains(line, "pop monitor") {
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

	cmd := exec.Command(exe, "pane", "monitor-start")
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

// --- monitor-stop ---

var paneMonitorStopCmd = &cobra.Command{
	Use:    "monitor-stop",
	Short:  "Stop the pane monitoring daemon",
	Args:   cobra.NoArgs,
	Hidden: true,
	RunE:   runPaneMonitorStop,
}

func runPaneMonitorStop(cmd *cobra.Command, args []string) error {
	pidPath := monitor.DefaultPIDPath()
	return monitor.StopDaemon(pidPath)
}

// --- monitor-status ---

var paneMonitorStatusCmd = &cobra.Command{
	Use:    "monitor-status",
	Short:  "Show pane monitor state (debug)",
	Args:   cobra.NoArgs,
	Hidden: true,
	RunE:   runPaneMonitorStatus,
}

func runPaneMonitorStatus(cmd *cobra.Command, args []string) error {
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
