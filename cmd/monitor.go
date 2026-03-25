package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/glebglazov/pop/internal/deps"
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
	return tmuxPaneSessionWith(defaultTmux, paneID)
}

func tmuxPaneSessionWith(tmux deps.Tmux, paneID string) (string, error) {
	return tmux.Command("display-message", "-t", paneID, "-p", "#{session_name}")
}

// isActiveTmuxPane returns true if the given pane is visible to the user:
// active in its window, the window is active in its session, and the session
// is attached to a client.
func isActiveTmuxPane(paneID string) bool {
	return isActiveTmuxPaneWith(defaultTmux, paneID)
}

func isActiveTmuxPaneWith(tmux deps.Tmux, paneID string) bool {
	out, err := tmux.Command("display-message", "-t", paneID, "-p", "#{pane_active} #{window_active} #{session_attached}")
	if err != nil {
		return false
	}
	return out == "1 1 1"
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
	installTmuxAutoReadHooksWith(defaultTmux)
}

func installTmuxAutoReadHooksWith(tmux deps.Tmux) {
	uninstallTmuxAutoReadHooksWith(tmux)
	for event, hookCmd := range tmuxAutoReadHooks {
		tmux.Command("set-hook", "-ga", event, hookCmd)
	}
}

// uninstallTmuxAutoReadHooks removes all pop-related tmux hooks,
// leaving other hooks intact. Parses indexed entries like "event[0] cmd".
func uninstallTmuxAutoReadHooks() {
	uninstallTmuxAutoReadHooksWith(defaultTmux)
}

func uninstallTmuxAutoReadHooksWith(tmux deps.Tmux) {
	out, _ := tmux.Command("show-hooks", "-g")
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "pop pane set-status") && !strings.Contains(line, "pop monitor") {
			continue
		}
		// Line format: "event[N] command..."
		bracketEnd := strings.Index(line, "]")
		if bracketEnd == -1 {
			continue
		}
		indexed := line[:bracketEnd+1]
		tmux.Command("set-hook", "-gu", indexed)
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
	return binaryNewerThanPIDWith(deps.NewRealFileSystem(), exePath, pidPath)
}

func binaryNewerThanPIDWith(fs deps.FileSystem, exePath, pidPath string) bool {
	exeInfo, err := fs.Stat(exePath)
	if err != nil {
		return true
	}
	pidInfo, err := fs.Stat(pidPath)
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
	return runPaneMonitorStatusWith(monitor.DefaultDeps(), os.Stdout)
}

func runPaneMonitorStatusWith(d *monitor.Deps, w io.Writer) error {
	pidPath := monitor.DefaultPIDPathWith(d)
	running := monitor.IsDaemonRunningWith(d, pidPath)
	if running {
		fmt.Fprintln(w, "Daemon: running")
	} else {
		fmt.Fprintln(w, "Daemon: stopped")
	}

	statePath := monitor.DefaultStatePathWith(d)
	state, err := monitor.LoadWith(d, statePath)
	if err != nil {
		return err
	}

	if len(state.Panes) == 0 {
		fmt.Fprintln(w, "No monitored panes")
		return nil
	}

	fmt.Fprintf(w, "\nMonitored panes (%d):\n", len(state.Panes))
	for _, entry := range state.Panes {
		fmt.Fprintf(w, "  %s  session=%s  status=%s  updated=%s\n",
			entry.PaneID, entry.Session, entry.Status,
			entry.UpdatedAt.Format("15:04:05"))
	}
	return nil
}
