package monitor

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/glebglazov/pop/debug"
)

const pollInterval = 5 * time.Second

// RunDaemon runs the monitoring loop in the foreground.
// Writes PID file on start, removes on exit.
// The daemon only handles cleanup (dead panes) and active-pane
// auto-read. State transitions are driven by hooks calling
// `pop pane set-status`.
func RunDaemon(statePath, pidPath, addr string, handler RequestHandler) error {
	return RunDaemonWith(DefaultDeps(), statePath, pidPath, addr, handler)
}

// RunDaemonWith runs the monitoring loop using provided dependencies.
// It starts a TCP listener for incoming status commands and runs the
// dead-pane poll loop. A mutex serializes all state mutations
// (socket handler + poll cleanup).
func RunDaemonWith(d *Deps, statePath, pidPath, addr string, handler RequestHandler) error {
	if err := writePIDFile(d, pidPath); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}
	defer removePIDFile(d, pidPath)

	var mu sync.Mutex

	// Wrap handler so socket requests and poll are serialized.
	guardedHandler := func(req Request) Response {
		mu.Lock()
		defer mu.Unlock()
		return handler(req)
	}

	var ln net.Listener
	if addr != "" {
		var err error
		ln, err = ListenAndServe(addr, guardedHandler)
		if err != nil {
			return fmt.Errorf("failed to start TCP server: %w", err)
		}
		defer ln.Close()
		fmt.Printf("Monitor listening on %s\n", ln.Addr())
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	fmt.Printf("Monitor daemon started (PID %d, polling every %s)\n", os.Getpid(), pollInterval)

	// Run first tick immediately
	guardedPollOnce := func() {
		mu.Lock()
		defer mu.Unlock()
		pollOnce(d, statePath)
	}

	guardedPollOnce()

	for {
		select {
		case <-ticker.C:
			guardedPollOnce()
		case sig := <-sigCh:
			fmt.Printf("\nReceived %s, shutting down\n", sig)
			return nil
		}
	}
}

func pollOnce(d *Deps, statePath string) {
	pollOnceWith(d, statePath, liveTmuxPanes)
}

func pollOnceWith(d *Deps, statePath string, livePanesFunc func() map[string]bool) {
	state, err := LoadWith(d, statePath)
	if err != nil {
		debug.Error("pollOnce: load state: %v", err)
		fmt.Fprintf(os.Stderr, "Failed to load state: %v\n", err)
		return
	}

	if len(state.Panes) == 0 {
		return
	}

	changed := false
	livePanes := livePanesFunc()
	if livePanes == nil {
		// tmux list-panes failed — can't determine which panes are alive.
		// Bail out rather than treating every registered pane as dead.
		return
	}

	for paneID, entry := range state.Panes {
		if !livePanes[paneID] {
			debug.Log("[monitor] %s (session=%s): deregistered (pane dead)", paneID, entry.Session)
			delete(state.Panes, paneID)
			changed = true
		}
	}

	if changed {
		if err := state.SaveWith(d); err != nil {
			debug.Error("pollOnce: save state: %v", err)
			fmt.Fprintf(os.Stderr, "Failed to save state: %v\n", err)
		}
	}
}

// liveTmuxPanes returns the set of pane IDs that exist across all sessions
func liveTmuxPanes() map[string]bool {
	out, err := exec.Command("tmux", "list-panes", "-a", "-F", "#{pane_id}").Output()
	if err != nil {
		debug.Error("liveTmuxPanes: %v", err)
		return nil
	}
	panes := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			panes[line] = true
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
