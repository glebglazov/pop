package cmd

import (
	"errors"
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

// --- monitor-start ---

// tmux hooks for auto-clear: event name → hook command
var tmuxAutoClearHooks = map[string]string{
	"after-select-pane":      `run-shell "pop pane set-status --source tmux-global --no-register #{pane_id} clear 2>/dev/null || true"`,
	"session-window-changed": `run-shell "pop pane set-status --source tmux-global --no-register #{pane_id} clear 2>/dev/null || true"`,
	"client-session-changed": `run-shell "pop pane set-status --source tmux-global --no-register #{pane_id} clear 2>/dev/null || true"`,
	"pane-focus-in":          `run-shell "pop pane visit #{pane_id} 2>/dev/null || true"`,
}

var paneMonitorStartCmd = &cobra.Command{
	Use:    "monitor-start",
	Short:  "Start the pane monitoring daemon (foreground)",
	Args:   cobra.NoArgs,
	Hidden: true,
	RunE:   runPaneMonitorStart,
}

func runPaneMonitorStart(cmd *cobra.Command, args []string) error {
	cfg := loadConfigQuietly()
	pidPath := monitor.DefaultPIDPath()
	statePath := monitor.DefaultStatePath()

	addr := ""
	if cfg.PaneMonitoringTCPServer() {
		addr = monitorAddr(cfg)
		// Liveness is the port, not the PID file: don't start a second daemon
		// if a current one already answers there.
		if id, err := monitor.Handshake(addr); err == nil && !id.Legacy {
			return fmt.Errorf("daemon already running (pid %d) at %s", id.PID, addr)
		}
	} else if monitor.IsDaemonRunning(pidPath) {
		return fmt.Errorf("daemon is already running (PID file: %s)", pidPath)
	}

	installTmuxAutoClearHooks()

	handler := buildMonitorHandler(defaultTmux, statePath)
	err := monitor.RunDaemon(statePath, pidPath, addr, handler)
	if errors.Is(err, monitor.ErrAddrInUse) {
		// Handshake above found no pop daemon, yet the bind failed: a non-pop
		// process holds the port. Surface it instead of dying silently.
		debug.Error("monitor-start: %s is held by a non-pop process", addr)
		return fmt.Errorf("monitor address %s is held by another process; "+
			"set [pane_monitoring] addr or POP_MONITOR_ADDR to relocate", addr)
	}
	return err
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
		case "set-topic":
			return handleSetTopic(tmux, statePath, req)
		case "visit":
			return handleVisit(statePath, req)
		case "identify":
			return handleIdentify()
		case "shutdown":
			return handleShutdown()
		default:
			return monitor.Response{OK: false, Error: "unknown command: " + req.Cmd}
		}
	}
}

// handleIdentify reports the running daemon's identity so a challenger can
// decide whether it is current. ExeMod (the running binary's mtime) is the
// comparison key; PID/ExePath/Version are diagnostic.
func handleIdentify() monitor.Response {
	resp := monitor.Response{OK: true, PID: os.Getpid(), Version: buildVersion()}
	if exe, err := os.Executable(); err == nil {
		resp.ExePath = exe
		if info, statErr := os.Stat(exe); statErr == nil {
			resp.ExeMod = info.ModTime().Unix()
		}
	}
	return resp
}

// handleShutdown signals the daemon to exit gracefully by sending SIGTERM to
// itself — the daemon's run loop catches it and shuts down cleanly, releasing
// the port and removing its PID file. Reusing the signal path keeps a single
// shutdown route.
func handleShutdown() monitor.Response {
	debug.Log("[monitor] shutdown requested via socket")
	if p, err := os.FindProcess(os.Getpid()); err == nil {
		if err := p.Signal(syscall.SIGTERM); err != nil {
			return monitor.Response{OK: false, Error: err.Error()}
		}
	}
	return monitor.Response{OK: true}
}

// handleSetStatus applies the set-status business logic. Extracted from
// buildMonitorHandler so each command is independently testable.
func handleSetStatus(tmux deps.Tmux, statePath string, req monitor.Request) monitor.Response {
	cfg, err := config.Load(config.DefaultConfigPath())
	if err != nil {
		debug.Error("handler set-status: load config: %v", err)
	}
	if cfg == nil {
		cfg = &config.Config{}
	}

	if req.Source != "" && cfg.ShouldIgnoreStatusFrom(req.Source) {
		return monitor.Response{OK: true}
	}

	if req.PaneID == "" {
		return monitor.Response{OK: true}
	}

	status := monitor.NormalizeStatus(req.Status)
	if status != monitor.StatusClear {
		debug.Log("[set-status] %s: invoked with %s", req.PaneID, status)
	}

	store := monitor.NewStore(statePath, nil)
	if err := store.ReportStatus(tmux, monitor.ReportStatusInput{
		PaneID:                req.PaneID,
		Status:                status,
		Label:                 req.Label,
		NoRegister:            req.NoRegister,
		DismissUnreadInActive: cfg.DismissUnreadInActivePane(),
	}); err != nil {
		debug.Error("handler set-status: %v", err)
		return monitor.Response{OK: false, Error: err.Error()}
	}
	return monitor.Response{OK: true}
}

// handleSetFollowing toggles a pane's Following flag via the monitor Store.
func handleSetFollowing(tmux deps.Tmux, statePath string, req monitor.Request) monitor.Response {
	if req.PaneID == "" {
		return monitor.Response{OK: false, Error: "missing pane_id"}
	}
	if req.Following == nil {
		return monitor.Response{OK: false, Error: "missing following field"}
	}

	store := monitor.NewStore(statePath, nil)
	if err := store.SetFollowing(tmux, req.PaneID, *req.Following); err != nil {
		debug.Error("handler set-following: %v", err)
		return monitor.Response{OK: false, Error: err.Error()}
	}
	return monitor.Response{OK: true}
}

// handleSetTopic sets (or clears) a pane's Topic via the monitor Store.
func handleSetTopic(tmux deps.Tmux, statePath string, req monitor.Request) monitor.Response {
	if req.PaneID == "" {
		return monitor.Response{OK: true}
	}

	store := monitor.NewStore(statePath, nil)
	if err := store.ReportTopic(tmux, monitor.ReportTopicInput{
		PaneID:     req.PaneID,
		Topic:      req.Topic,
		NoRegister: req.NoRegister,
	}); err != nil {
		debug.Error("handler set-topic: %v", err)
		return monitor.Response{OK: false, Error: err.Error()}
	}
	return monitor.Response{OK: true}
}

// handleVisit records that a pane was visited by the user. Only tracked panes
// are updated; untracked panes are silently ignored (no auto-registration).
func handleVisit(statePath string, req monitor.Request) monitor.Response {
	if req.PaneID == "" {
		return monitor.Response{OK: false, Error: "missing pane_id"}
	}

	store := monitor.NewStore(statePath, nil)
	if err := store.RecordVisit(req.PaneID); err != nil {
		debug.Error("handler visit: %v", err)
		return monitor.Response{OK: false, Error: err.Error()}
	}
	return monitor.Response{OK: true}
}

// installTmuxAutoClearHooks removes any existing pop hooks and installs current ones.
func installTmuxAutoClearHooks() {
	installTmuxAutoClearHooksWith(defaultTmux)
}

func installTmuxAutoClearHooksWith(tmux deps.Tmux) {
	uninstallTmuxAutoClearHooksWith(tmux)
	for event, hookCmd := range tmuxAutoClearHooks {
		if _, err := tmux.Command("set-hook", "-ga", event, hookCmd); err != nil {
			debug.Error("installTmuxAutoClearHooks: set-hook %s: %v", event, err)
		}
	}
}

// uninstallTmuxAutoClearHooks removes all pop-related tmux hooks,
// leaving other hooks intact. Parses indexed entries like "event[0] cmd".
func uninstallTmuxAutoClearHooks() {
	uninstallTmuxAutoClearHooksWith(defaultTmux)
}

func uninstallTmuxAutoClearHooksWith(tmux deps.Tmux) {
	out, err := tmux.Command("show-hooks", "-g")
	if err != nil {
		debug.Error("uninstallTmuxAutoClearHooks: show-hooks: %v", err)
	}
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "pop pane set-status") && !strings.Contains(line, "pop pane visit") && !strings.Contains(line, "pop monitor") {
			continue
		}
		// Line format: "event[N] command..."
		bracketEnd := strings.Index(line, "]")
		if bracketEnd == -1 {
			continue
		}
		indexed := line[:bracketEnd+1]
		if _, err := tmux.Command("set-hook", "-gu", indexed); err != nil {
			debug.Error("uninstallTmuxAutoClearHooks: unset %s: %v", indexed, err)
		}
	}
}

// ensureSystemState runs the startup side-effects shared by the interactive
// TUI entry points (project, worktree, dashboard):
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

// monitorAddr resolves the daemon's TCP address with precedence
// POP_MONITOR_ADDR env > [pane_monitoring] addr config > data-dir-derived
// default (ADR 0021). Daemon and all clients route through this so they agree.
func monitorAddr(cfg *config.Config) string {
	if v := os.Getenv("POP_MONITOR_ADDR"); v != "" {
		return v
	}
	if a := cfg.PaneMonitoringAddr(); a != "" {
		return a
	}
	return monitor.DerivedAddr()
}

// loadConfigQuietly loads config, logging (not surfacing) errors and never
// returning nil — callers in non-interactive paths just need defaults.
func loadConfigQuietly() *config.Config {
	cfg, err := config.Load(config.DefaultConfigPath())
	if err != nil {
		debug.Error("load config: %v", err)
	}
	if cfg == nil {
		cfg = &config.Config{}
	}
	return cfg
}

// ensureMonitorDaemon ensures a current monitor daemon is running, reaping a
// stale one if the running binary is newer (ADR 0021 version-restart).
// Called automatically by the interactive pickers and the socket-failure
// self-heal path.
//
// Always invoked where a panic must not crash the parent — a failed daemon
// startup is non-fatal for the picker flow.
func ensureMonitorDaemon() {
	defer func() {
		if r := recover(); r != nil {
			debug.Error("ensureMonitorDaemon: panic: %v\n%s", r, runtimedebug.Stack())
		}
	}()

	exe, err := os.Executable()
	if err != nil {
		debug.Error("ensureMonitorDaemon: os.Executable: %v", err)
		return
	}
	cfg := loadConfigQuietly()

	// TCP disabled: no port to handshake. Fall back to PID-file liveness +
	// binary-mtime version comparison (no bind, so no port-conflict hazard).
	if !cfg.PaneMonitoringTCPServer() {
		ensureMonitorDaemonViaPID(exe)
		return
	}

	addr := monitorAddr(cfg)
	id, err := monitor.Handshake(addr)
	if err == nil {
		// A pop daemon holds the port. Keep it if it is current; otherwise reap.
		if !id.Legacy && id.ExeMod >= exeModTime(exe) {
			return // up to date
		}
		debug.Log("ensureMonitorDaemon: reaping stale daemon (pid=%d exeMod=%d mine=%d legacy=%v)",
			id.PID, id.ExeMod, exeModTime(exe), id.Legacy)
		if err := monitor.SendShutdown(addr); err != nil {
			debug.Error("ensureMonitorDaemon: shutdown stale daemon: %v", err)
		}
		waitForPortFree(addr, 2*time.Second)
	}
	// err != nil ⇒ port free, or held by a non-pop process. Either way attempt
	// to start; the daemon surfaces ErrAddrInUse loudly if a foreign process
	// holds it (see runPaneMonitorStart).
	startMonitorDaemon(exe)
}

// ensureMonitorDaemonViaPID is the legacy PID-file path used only when the TCP
// server is disabled. With no listener there is no bind conflict, so the
// historical scheme is safe here.
func ensureMonitorDaemonViaPID(exe string) {
	pidPath := monitor.DefaultPIDPath()
	if monitor.IsDaemonRunning(pidPath) {
		if !binaryNewerThanPID(exe, pidPath) {
			return
		}
		if err := monitor.StopDaemon(pidPath); err != nil {
			debug.Error("ensureMonitorDaemon: stop old daemon: %v", err)
		}
	}
	for range 10 {
		if !monitor.IsDaemonRunning(pidPath) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	startMonitorDaemon(exe)
}

// startMonitorDaemon spawns a detached `pop pane monitor-start`.
func startMonitorDaemon(exe string) {
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

// exeModTime returns the unix mtime of exe, or 0 if it cannot be stat'd.
func exeModTime(exe string) int64 {
	info, err := os.Stat(exe)
	if err != nil {
		return 0
	}
	return info.ModTime().Unix()
}

// waitForPortFree polls until no pop daemon answers at addr, or the deadline.
func waitForPortFree(addr string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := monitor.Handshake(addr); err != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// waitForDaemon polls until a pop daemon answers at addr (returning its
// identity), or the deadline (returning nil).
func waitForDaemon(addr string, timeout time.Duration) *monitor.Identity {
	deadline := time.Now().Add(timeout)
	for {
		if id, err := monitor.Handshake(addr); err == nil {
			return id
		}
		if !time.Now().Before(deadline) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
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
