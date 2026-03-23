package monitor

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/glebglazov/pop/debug"
)

const pollInterval = 5 * time.Second

// RunDaemon runs the monitoring loop in the foreground.
// Writes PID file on start, removes on exit.
// The daemon only handles cleanup (dead panes, unknown sources)
// and active-pane auto-read. State transitions are driven by
// Claude Code hooks calling `pop monitor set-status`.
func RunDaemon(statePath, pidPath string) error {
	return RunDaemonWith(DefaultDeps(), statePath, pidPath)
}

// RunDaemonWith runs the monitoring loop using provided dependencies
func RunDaemonWith(d *Deps, statePath, pidPath string) error {
	if err := writePIDFile(d, pidPath); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}
	defer removePIDFile(d, pidPath)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	fmt.Printf("Monitor daemon started (PID %d, polling every %s)\n", os.Getpid(), pollInterval)

	// Run first tick immediately
	pollOnce(d, statePath)

	for {
		select {
		case <-ticker.C:
			pollOnce(d, statePath)
		case sig := <-sigCh:
			fmt.Printf("\nReceived %s, shutting down\n", sig)
			return nil
		}
	}
}

func pollOnce(d *Deps, statePath string) {
	state, err := LoadWith(d, statePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load state: %v\n", err)
		return
	}

	if len(state.Panes) == 0 {
		return
	}

	changed := false
	livePanes := liveTmuxPanes()

	for paneID, entry := range state.Panes {
		// Drop panes with unrecognized source
		if !IsKnownSource(entry.Source) {
			debug.Log("[monitor] %s: deregistered (unknown source %q)", paneID, entry.Source)
			delete(state.Panes, paneID)
			changed = true
			continue
		}

		// Check if pane still exists in tmux
		info, alive := livePanes[paneID]
		if !alive {
			debug.Log("[monitor] %s: deregistered (pane dead)", paneID)
			delete(state.Panes, paneID)
			changed = true
			continue
		}

		// If user is actively looking at this pane, treat as read
		if info.active && entry.Status == StatusNeedsAttention {
			debug.Log("[monitor] %s: %s → read (pane is active)", paneID, entry.Status)
			entry.Status = StatusRead
			entry.UpdatedAt = time.Now()
			changed = true
		}
	}

	if changed {
		if err := state.SaveWith(d); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to save state: %v\n", err)
		}
	}
}

// paneInfo holds liveness and activity state for a tmux pane
type paneInfo struct {
	alive  bool
	active bool
}

// liveTmuxPanes returns info about all panes across all sessions
func liveTmuxPanes() map[string]paneInfo {
	out, err := exec.Command("tmux", "list-panes", "-a", "-F", "#{pane_id} #{pane_active}").Output()
	if err != nil {
		return nil
	}
	panes := make(map[string]paneInfo)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			panes[parts[0]] = paneInfo{alive: true, active: parts[1] == "1"}
		}
	}
	return panes
}

// StopDaemon sends SIGTERM to the daemon process
func StopDaemon(pidPath string) error {
	return StopDaemonWith(DefaultDeps(), pidPath)
}

// StopDaemonWith sends SIGTERM using provided dependencies
func StopDaemonWith(d *Deps, pidPath string) error {
	data, err := d.FS.ReadFile(pidPath)
	if err != nil {
		return fmt.Errorf("daemon not running (no PID file)")
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return fmt.Errorf("invalid PID file")
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("process not found: %d", pid)
	}

	if err := process.Signal(syscall.SIGTERM); err != nil {
		removePIDFile(d, pidPath)
		return fmt.Errorf("failed to signal daemon (cleaned up stale PID file)")
	}

	fmt.Printf("Sent SIGTERM to daemon (PID %d)\n", pid)
	return nil
}

func writePIDFile(d *Deps, pidPath string) error {
	dir := filepath.Dir(pidPath)
	if err := d.FS.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return d.FS.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0644)
}

func removePIDFile(d *Deps, pidPath string) {
	os.Remove(pidPath)
}
