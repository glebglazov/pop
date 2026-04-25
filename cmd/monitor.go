package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	runtimedebug "runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/debug"
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

// tmuxPaneInfoWith returns the session name and the current foreground
// command running in the given pane in a single tmux round-trip. Used by
// auto-registration in pane set-status, which needs both to decide whether
// a pane is agentic (not a plain shell) before adding it to the dashboard.
func tmuxPaneInfoWith(tmux deps.Tmux, paneID string) (session, cmdName string, err error) {
	out, err := tmux.Command("display-message", "-t", paneID, "-p", "#{session_name}\t#{pane_current_command}")
	if err != nil {
		return "", "", err
	}
	parts := strings.SplitN(out, "\t", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("unexpected display-message output: %q", out)
	}
	return parts[0], parts[1], nil
}

// plainShellCommands lists the foreground process names that pop treats as
// "just a shell prompt" — panes running these are NOT auto-registered on
// an idle status update, since neither the tmux-global auto-read hook nor
// an agent-extension housekeeping idle should cause a bare shell to show
// up on the dashboard.
var plainShellCommands = map[string]bool{
	"zsh":  true,
	"bash": true,
	"fish": true,
	"sh":   true,
	"dash": true,
	"ksh":  true,
	"tcsh": true,
	"csh":  true,
}

// isPlainShellCommand reports whether the given tmux pane_current_command
// is a plain interactive shell (zsh, bash, fish, ...). Matching is done on
// the basename with any leading dash (login-shell marker) stripped.
func isPlainShellCommand(cmdName string) bool {
	cmdName = strings.TrimPrefix(cmdName, "-")
	return plainShellCommands[cmdName]
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
	"after-select-pane":      `run-shell "pop pane set-status --source tmux-global --no-register #{pane_id} read 2>/dev/null || true"`,
	"session-window-changed": `run-shell "pop pane set-status --source tmux-global --no-register #{pane_id} read 2>/dev/null || true"`,
	"client-session-changed": `run-shell "pop pane set-status --source tmux-global --no-register #{pane_id} read 2>/dev/null || true"`,
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
	cfg, err := config.Load(config.DefaultConfigPath())
	if err != nil {
		debug.Error("monitor-start: load config: %v", err)
	}
	if cfg == nil {
		cfg = &config.Config{}
	}
	addr := ""
	if cfg.PaneMonitoringTCPServer() {
		addr = monitor.DefaultAddr()
	}
	handler := buildMonitorHandler(defaultTmux, statePath)
	return monitor.RunDaemon(statePath, pidPath, addr, handler)
}

// buildMonitorHandler returns a RequestHandler that dispatches by req.Cmd.
// Each branch loads config and state from disk on every call — no in-memory
// cache for V1. An empty Cmd is treated as "set-status" for backward
// compatibility with older clients.
func buildMonitorHandler(tmux deps.Tmux, statePath string) monitor.RequestHandler {
	return func(req monitor.Request) monitor.Response {
		debug.Init()
		defer debug.Close()

		switch req.Cmd {
		case "", "set-status":
			return handleSetStatus(tmux, statePath, req)
		case "set-following":
			return handleSetFollowing(tmux, statePath, req)
		default:
			return monitor.Response{OK: false, Error: "unknown command: " + req.Cmd}
		}
	}
}

// handleSetStatus applies the set-status business logic. Extracted from
// buildMonitorHandler so each command is independently testable.
func handleSetStatus(tmux deps.Tmux, statePath string, req monitor.Request) monitor.Response {
	paneID := req.PaneID
	status := monitor.PaneStatus(req.Status)
	source := req.Source
	noRegister := req.NoRegister

	// Normalize deprecated aliases.
	if status == "read" {
		status = monitor.StatusIdle
	}
	if status == "needs_attention" {
		status = monitor.StatusUnread
	}

	cfg, err := config.Load(config.DefaultConfigPath())
	if err != nil {
		debug.Error("handler set-status: load config: %v", err)
	}
	if cfg == nil {
		cfg = &config.Config{}
	}

	if source != "" && cfg.ShouldIgnoreStatusFrom(source) {
		return monitor.Response{OK: true}
	}

	if paneID == "" {
		return monitor.Response{OK: true}
	}

	if status != monitor.StatusIdle {
		debug.Log("[set-status] %s: invoked with %s", paneID, status)
	}

	state, err := monitor.Load(statePath)
	if err != nil {
		debug.Error("handler set-status: load state: %v", err)
		return monitor.Response{OK: false, Error: "load state: " + err.Error()}
	}

	entry, ok := state.Panes[paneID]
	if !ok {
		if noRegister {
			return monitor.Response{OK: true}
		}
		session, cmdName, err := tmuxPaneInfoWith(tmux, paneID)
		if err != nil {
			debug.Error("[set-status] %s: failed to look up pane info, skipping: %v", paneID, err)
			return monitor.Response{OK: true}
		}
		if status == monitor.StatusIdle && isPlainShellCommand(cmdName) {
			return monitor.Response{OK: true}
		}
		debug.Log("[set-status] %s: auto-registering in session=%s (cmd=%s) with status=%s", paneID, session, cmdName, status)
		now := time.Now()
		state.Panes[paneID] = &monitor.PaneEntry{
			PaneID:      paneID,
			Session:     session,
			Status:      status,
			UpdatedAt:   now,
			LastVisited: now,
		}
		if err := state.Save(); err != nil {
			return monitor.Response{OK: false, Error: "save state: " + err.Error()}
		}
		return monitor.Response{OK: true}
	}

	visitedNow := false
	if status == monitor.StatusIdle {
		entry.LastVisited = time.Now()
		visitedNow = true
	}

	if cfg.DismissUnreadInActivePane() && status == monitor.StatusUnread && isActiveTmuxPaneWith(tmux, paneID) {
		debug.Log("[set-status] %s: unread on active pane — downgrading to idle", paneID)
		status = monitor.StatusIdle
	}

	if entry.Status == status {
		if visitedNow {
			if err := state.Save(); err != nil {
				return monitor.Response{OK: false, Error: "save state: " + err.Error()}
			}
		}
		return monitor.Response{OK: true}
	}

	debug.Log("[set-status] %s (session=%s): %s → %s", paneID, entry.Session, entry.Status, status)
	entry.Status = status
	entry.UpdatedAt = time.Now()
	if err := state.Save(); err != nil {
		return monitor.Response{OK: false, Error: "save state: " + err.Error()}
	}
	return monitor.Response{OK: true}
}

// handleSetFollowing toggles a pane's Following flag. Untracked panes are
// auto-registered (status=idle) so the user can mark a pane as followed from
// the CLI without having to set-status first; unfollowing an untracked pane
// is a no-op since the absence already implies "not followed". Unfollowing
// also clears any user note on the pane, mirroring the picker's behavior.
func handleSetFollowing(tmux deps.Tmux, statePath string, req monitor.Request) monitor.Response {
	if req.PaneID == "" {
		return monitor.Response{OK: false, Error: "missing pane_id"}
	}
	if req.Following == nil {
		return monitor.Response{OK: false, Error: "missing following field"}
	}
	follow := *req.Following

	state, err := monitor.Load(statePath)
	if err != nil {
		debug.Error("handler set-following: load state: %v", err)
		return monitor.Response{OK: false, Error: "load state: " + err.Error()}
	}

	entry, ok := state.Panes[req.PaneID]
	if !ok {
		if !follow {
			return monitor.Response{OK: true}
		}
		session, _, err := tmuxPaneInfoWith(tmux, req.PaneID)
		if err != nil {
			return monitor.Response{OK: false, Error: "look up pane: " + err.Error()}
		}
		debug.Log("[set-following] %s: auto-registering in session=%s with following=true", req.PaneID, session)
		now := time.Now()
		state.Panes[req.PaneID] = &monitor.PaneEntry{
			PaneID:      req.PaneID,
			Session:     session,
			Status:      monitor.StatusIdle,
			Following:   true,
			UpdatedAt:   now,
			LastVisited: now,
		}
		if err := state.Save(); err != nil {
			return monitor.Response{OK: false, Error: "save state: " + err.Error()}
		}
		return monitor.Response{OK: true}
	}

	if entry.Following == follow {
		return monitor.Response{OK: true}
	}
	debug.Log("[set-following] %s (session=%s): %v → %v", req.PaneID, entry.Session, entry.Following, follow)
	entry.Following = follow
	entry.UpdatedAt = time.Now()
	if !follow {
		entry.Note = ""
	}
	if err := state.Save(); err != nil {
		return monitor.Response{OK: false, Error: "save state: " + err.Error()}
	}
	return monitor.Response{OK: true}
}

// installTmuxAutoReadHooks removes any existing pop hooks and installs current ones.
func installTmuxAutoReadHooks() {
	installTmuxAutoReadHooksWith(defaultTmux)
}

func installTmuxAutoReadHooksWith(tmux deps.Tmux) {
	uninstallTmuxAutoReadHooksWith(tmux)
	for event, hookCmd := range tmuxAutoReadHooks {
		if _, err := tmux.Command("set-hook", "-ga", event, hookCmd); err != nil {
			debug.Error("installTmuxAutoReadHooks: set-hook %s: %v", event, err)
		}
	}
}

// uninstallTmuxAutoReadHooks removes all pop-related tmux hooks,
// leaving other hooks intact. Parses indexed entries like "event[0] cmd".
func uninstallTmuxAutoReadHooks() {
	uninstallTmuxAutoReadHooksWith(defaultTmux)
}

func uninstallTmuxAutoReadHooksWith(tmux deps.Tmux) {
	out, err := tmux.Command("show-hooks", "-g")
	if err != nil {
		debug.Error("uninstallTmuxAutoReadHooks: show-hooks: %v", err)
	}
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
		if _, err := tmux.Command("set-hook", "-gu", indexed); err != nil {
			debug.Error("uninstallTmuxAutoReadHooks: unset %s: %v", indexed, err)
		}
	}
}

// ensureSystemState runs the startup side-effects shared by the interactive
// TUI entry points (select, worktree, dashboard):
//
//  1. Synchronously updates any stale agent integrations, so warnings for
//     integration failures are visible on the very first picker render.
//  2. Kicks off the monitor daemon check in a background goroutine, because
//     it involves process management and does not need to block the picker.
//
// Returns warnings that the caller should surface through the picker's
// warnings slot. The function is called from the main goroutine; it should
// not be wrapped in `go`.
func ensureSystemState() []string {
	warnings := ensureIntegrations()
	go ensureMonitorDaemon()
	return warnings
}

// ensureMonitorDaemon ensures a monitor daemon is running with the current binary.
// Restarts if the binary is newer than the running daemon.
// Called automatically by `pop select`.
//
// Always invoked in a background goroutine, so panics here must not crash the
// parent process — a failed daemon startup is non-fatal for the picker flow.
func ensureMonitorDaemon() {
	defer func() {
		if r := recover(); r != nil {
			debug.Error("ensureMonitorDaemon: panic: %v\n%s", r, runtimedebug.Stack())
		}
	}()

	pidPath := monitor.DefaultPIDPath()
	exe, err := os.Executable()
	if err != nil {
		debug.Error("ensureMonitorDaemon: os.Executable: %v", err)
		return
	}

	if monitor.IsDaemonRunning(pidPath) {
		if !binaryNewerThanPID(exe, pidPath) {
			return // daemon is up to date
		}
		// Signal old daemon to stop; it will clean up its PID file on exit
		if err := monitor.StopDaemon(pidPath); err != nil {
			debug.Error("ensureMonitorDaemon: stop old daemon: %v", err)
		}
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
	if err := cmd.Start(); err != nil {
		debug.Error("ensureMonitorDaemon: start daemon: %v", err)
	}
	if cmd.Process != nil {
		if err := cmd.Process.Release(); err != nil {
			debug.Error("ensureMonitorDaemon: release process: %v", err)
		}
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
