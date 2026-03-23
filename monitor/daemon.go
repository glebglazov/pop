package monitor

import (
	"crypto/sha256"
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

	// Track previous pane content for change detection
	prevContent := make(map[string]string)

	// Run first tick immediately
	pollOnce(d, statePath, prevContent)

	for {
		select {
		case <-ticker.C:
			pollOnce(d, statePath, prevContent)
		case sig := <-sigCh:
			fmt.Printf("\nReceived %s, shutting down\n", sig)
			return nil
		}
	}
}

func pollOnce(d *Deps, statePath string, prevContent map[string]string) {
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
		// Drop panes with unrecognized source (e.g., renamed source constants)
		if !IsKnownSource(entry.Source) {
			delete(state.Panes, paneID)
			delete(prevContent, paneID)
			changed = true
			continue
		}

		// Check if pane still exists in tmux
		info, alive := livePanes[paneID]
		if !alive {
			delete(state.Panes, paneID)
			delete(prevContent, paneID)
			changed = true
			continue
		}

		// If user is actively looking at this pane, treat as read
		if info.active {
			if entry.Status == StatusNeedsAttention {
				debug.Log("[monitor] %s: %s → read (pane is active)", paneID, entry.Status)
				entry.Status = StatusRead
				entry.UpdatedAt = time.Now()
				changed = true
			}
			prevContent[paneID] = "" // reset so next poll after leaving doesn't see a stale diff
			continue
		}

		// Capture pane content
		content, err := capturePaneContent(paneID)
		if err != nil {
			delete(state.Panes, paneID)
			delete(prevContent, paneID)
			changed = true
			continue
		}

		// Detect state using current and previous content
		detector := DetectorForSource(entry.Source)
		prev := prevContent[paneID]
		detectedStatus := detector.Detect(content, prev)
		contentChanged := prev != "" && content != prev
		contentHash := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))[:8]
		prevHash := ""
		if prev != "" {
			prevHash = fmt.Sprintf("%x", sha256.Sum256([]byte(prev)))[:8]
		}
		prevContent[paneID] = content

		newStatus := detectedStatus

		// If it was working and now stopped producing output,
		// that means it finished — treat as needs attention
		if entry.Status == StatusWorking && newStatus == StatusUnknown {
			debug.Log("[monitor] %s: working→unknown promoted to needs_attention", paneID)
			newStatus = StatusNeedsAttention
		}

		// needs_attention is sticky — only exits when content starts
		// changing again (user interacted, agent started working)
		if entry.Status == StatusNeedsAttention && newStatus == StatusUnknown {
			newStatus = StatusNeedsAttention
		}

		// read is sticky — user acknowledged the pane, don't re-trigger
		// needs_attention until content changes (agent starts working again)
		if entry.Status == StatusRead && (newStatus == StatusUnknown || newStatus == StatusNeedsAttention) {
			newStatus = StatusRead
		}

		if newStatus != entry.Status {
			debug.Log("[monitor] %s: %s → %s (detected=%s, contentChanged=%v, hash=%s→%s)",
				paneID, entry.Status, newStatus, detectedStatus, contentChanged, prevHash, contentHash)
			entry.Status = newStatus
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

// capturePaneContent captures the last N lines of a pane
func capturePaneContent(paneID string) (string, error) {
	out, err := exec.Command("tmux", "capture-pane", "-p", "-S", "-20", "-t", paneID).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
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
