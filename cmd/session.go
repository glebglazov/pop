package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebglazov/pop/history"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/monitor"
	"github.com/glebglazov/pop/ui"
)

var defaultTmux deps.Tmux = deps.NewRealTmux()

const (
	tmuxSessionPathPrefix = "tmux:"
	iconDirSession        = "■"
	iconStandaloneSession = "□"
	iconAttention         = ui.IconAttention
)

func currentTmuxSession() string {
	return currentTmuxSessionWith(defaultTmux)
}

func currentTmuxSessionWith(tmux deps.Tmux) string {
	out, err := tmux.Command("display-message", "-p", "#S")
	if err != nil {
		return ""
	}
	return out
}

func isStandaloneSession(item ui.Item) bool {
	return strings.HasPrefix(item.Path, tmuxSessionPathPrefix)
}

func standaloneSessionName(item ui.Item) string {
	return strings.TrimPrefix(item.Path, tmuxSessionPathPrefix)
}

// switchToTmuxTarget switches to or attaches to a tmux target (session name or pane ID)
func switchToTmuxTarget(target string) error {
	return switchToTmuxTargetWith(defaultTmux, target)
}

func switchToTmuxTargetWith(tmux deps.Tmux, target string) error {
	inTmux := os.Getenv("TMUX") != ""
	if inTmux {
		_, err := tmux.Command("switch-client", "-t", target)
		return err
	}
	// attach-session needs stdio wired — cannot go through the generic Command
	cmd := exec.Command("tmux", "attach-session", "-t", target)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// switchToTmuxTargetAndZoom switches to a tmux pane and zooms it
func switchToTmuxTargetAndZoom(target string) error {
	return switchToTmuxTargetAndZoomWith(defaultTmux, target)
}

func switchToTmuxTargetAndZoomWith(tmux deps.Tmux, target string) error {
	inTmux := os.Getenv("TMUX") != ""
	if inTmux {
		// Single tmux invocation: switch to pane and zoom it if not already zoomed
		_, err := tmux.Command(
			"switch-client", "-t", target, ";",
			"if-shell", "-F", "#{!=:#{window_zoomed_flag},1}",
			"resize-pane -Z",
		)
		return err
	}
	// Outside tmux: zoom before attaching since attach takes over stdio
	tmux.Command(
		"if-shell", "-t", target, "-F", "#{!=:#{window_zoomed_flag},1}",
		"resize-pane -Z",
	)
	cmd := exec.Command("tmux", "attach-session", "-t", target)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// loadMonitorState returns the monitor state if the daemon is running, or nil otherwise
func loadMonitorState() *monitor.State {
	return loadMonitorStateWith(monitor.DefaultDeps())
}

func loadMonitorStateWith(d *monitor.Deps) *monitor.State {
	pidPath := monitor.DefaultPIDPathWith(d)
	if !monitor.IsDaemonRunningWith(d, pidPath) {
		return nil
	}
	statePath := monitor.DefaultStatePathWith(d)
	state, err := monitor.LoadWith(d, statePath)
	if err != nil {
		return nil
	}
	return state
}

// monitorAttentionSessions returns sessions needing attention,
// or nil if the daemon is not running
func monitorAttentionSessions() map[string]bool {
	return monitorAttentionSessionsWith(monitor.DefaultDeps())
}

func monitorAttentionSessionsWith(d *monitor.Deps) map[string]bool {
	state := loadMonitorStateWith(d)
	if state == nil {
		return nil
	}
	return state.SessionsNeedingAttention()
}

// buildAttentionPanes returns attention panes for the picker sub-view
func buildAttentionPanes() []ui.AttentionPane {
	state := loadMonitorState()
	if state == nil {
		return nil
	}

	entries := state.PanesNeedingAttention()
	paneCommands := tmuxPaneCommands()
	panes := make([]ui.AttentionPane, 0, len(entries))
	for _, entry := range entries {
		name := entry.PaneID
		if cmd, ok := paneCommands[entry.PaneID]; ok {
			name = entry.PaneID + " (" + cmd + ")"
		}
		panes = append(panes, ui.AttentionPane{
			PaneID:  entry.PaneID,
			Session: entry.Session,
			Name:    name,
			Status:  ui.AttentionNeedsAttention,
		})
	}
	return panes
}

// tmuxPaneCommands returns a map of pane ID → current command for all panes
func tmuxPaneCommands() map[string]string {
	return tmuxPaneCommandsWith(defaultTmux)
}

func tmuxPaneCommandsWith(tmux deps.Tmux) map[string]string {
	out, err := tmux.Command("list-panes", "-a", "-F", "#{pane_id} #{pane_current_command}")
	if err != nil {
		return nil
	}
	result := make(map[string]string)
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, " ", 2)
		if len(parts) == 2 {
			result[parts[0]] = parts[1]
		}
	}
	return result
}

// capturePanePreview captures the last 50 lines of a tmux pane for preview display
// sessionHistoryPath returns the history path to record for a given tmux session name.
// It searches existing history entries for one whose sanitized base name matches,
// falling back to tmux:<sessionName> for standalone sessions.
//
// Session names for worktrees are formed as "<displayName>/<worktreeName>", so
// filepath.Base alone only captures the last component. We first try an exact
// base match, then fall back to matching the base against the last slash-separated
// component of the session name (e.g. "worktrees-and-stuff" from
// "game_server/worktrees-and-stuff").
func sessionHistoryPath(sessionName string, hist *history.History) string {
	// Last component of the session name (after the final slash, if any)
	lastComponent := sessionName
	if i := strings.LastIndex(sessionName, "/"); i >= 0 {
		lastComponent = sessionName[i+1:]
	}

	var partialMatch string
	for _, e := range hist.Entries {
		sanitizedBase := sanitizeSessionName(filepath.Base(e.Path))
		if sanitizedBase == sessionName {
			return e.Path // exact match
		}
		if partialMatch == "" && sanitizedBase == lastComponent {
			partialMatch = e.Path // partial match — keep scanning for an exact one
		}
	}
	if partialMatch != "" {
		return partialMatch
	}
	return tmuxSessionPathPrefix + sessionName
}

func capturePanePreview(paneID string) string {
	return capturePanePreviewWith(defaultTmux, paneID)
}

func capturePanePreviewWith(tmux deps.Tmux, paneID string) string {
	out, err := tmux.Command("capture-pane", "-p", "-e", "-S", "-50", "-t", paneID)
	if err != nil {
		return ""
	}
	return out
}

// dismissAttentionPane transitions a pane from needs_attention to read.
// Unlike markPaneRead, this is a no-op for panes in other states.
func dismissAttentionPane(paneID string) {
	dismissAttentionPaneWith(monitor.DefaultDeps(), paneID)
}

func dismissAttentionPaneWith(d *monitor.Deps, paneID string) {
	state := loadMonitorStateWith(d)
	if state == nil {
		return
	}
	entry, ok := state.Panes[paneID]
	if !ok {
		return
	}
	entry.LastVisited = time.Now()
	if entry.Status == monitor.StatusNeedsAttention {
		entry.Status = monitor.StatusRead
	}
	state.SaveWith(d)
}

// markPaneRead marks a pane as read in the monitor state
func markPaneRead(paneID string) {
	markPaneReadWith(monitor.DefaultDeps(), paneID)
}

func markPaneReadWith(d *monitor.Deps, paneID string) {
	state := loadMonitorStateWith(d)
	if state == nil {
		return
	}
	entry, ok := state.Panes[paneID]
	if !ok {
		return
	}
	entry.Status = monitor.StatusRead
	state.SaveWith(d)
}

// markPaneAttention marks a pane as needs-attention in the monitor state
func markPaneAttention(paneID string) {
	markPaneAttentionWith(monitor.DefaultDeps(), paneID)
}

func markPaneAttentionWith(d *monitor.Deps, paneID string) {
	state := loadMonitorStateWith(d)
	if state == nil {
		return
	}
	entry, ok := state.Panes[paneID]
	if !ok {
		return
	}
	entry.Status = monitor.StatusNeedsAttention
	state.SaveWith(d)
}

// togglePaneFollow toggles the following flag on a pane
func togglePaneFollow(paneID string) {
	togglePaneFollowWith(monitor.DefaultDeps(), paneID)
}

func togglePaneFollowWith(d *monitor.Deps, paneID string) {
	state := loadMonitorStateWith(d)
	if state == nil {
		return
	}
	entry, ok := state.Panes[paneID]
	if !ok {
		return
	}
	entry.Following = !entry.Following
	state.SaveWith(d)
}

// unmonitorPane removes a pane from the monitor state entirely
func unmonitorPane(paneID string) {
	unmonitorPaneWith(monitor.DefaultDeps(), paneID)
}

func unmonitorPaneWith(d *monitor.Deps, paneID string) {
	state := loadMonitorStateWith(d)
	if state == nil {
		return
	}
	delete(state.Panes, paneID)
	state.SaveWith(d)
}

// attentionCallbacks returns the standard callbacks for attention sub-views
func attentionCallbacks() ui.AttentionCallbacks {
	return ui.AttentionCallbacks{
		Preview:       capturePanePreview,
		MarkRead:      markPaneRead,
		MarkAttention: markPaneAttention,
		ToggleFollow:  togglePaneFollow,
		Unmonitor:     unmonitorPane,
	}
}

func killTmuxSessionByName(sessionName string) {
	killTmuxSessionByNameWith(defaultTmux, sessionName)
}

func killTmuxSessionByNameWith(tmux deps.Tmux, sessionName string) {
	_, err := tmux.Command("kill-session", "-t", sessionName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to kill session: %s\n", sessionName)
	} else {
		fmt.Fprintf(os.Stderr, "Killed session: %s\n", sessionName)
	}
}
