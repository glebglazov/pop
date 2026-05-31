package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/glebglazov/pop/debug"
	"github.com/glebglazov/pop/history"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/monitor"
	"github.com/glebglazov/pop/session"
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
		debug.Error("currentTmuxSession: %v", err)
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
	return session.SwitchTargetWith(sessionDeps(tmux), target)
}

// switchToTmuxTargetAndZoom switches to a tmux pane and zooms it
func switchToTmuxTargetAndZoom(target string) error {
	return switchToTmuxTargetAndZoomWith(defaultTmux, target)
}

func switchToTmuxTargetAndZoomWith(tmux deps.Tmux, target string) error {
	d := sessionDeps(tmux)
	if d.InTmux() {
		if err := session.SwitchTargetWith(d, target); err != nil {
			return err
		}
		_, err := tmux.Command(
			"if-shell", "-F", "#{!=:#{window_zoomed_flag},1}",
			"resize-pane -Z",
		)
		return err
	}
	// Outside tmux: zoom before attaching since attach takes over stdio
	if _, err := tmux.Command(
		"if-shell", "-t", target, "-F", "#{!=:#{window_zoomed_flag},1}",
		"resize-pane -Z",
	); err != nil {
		debug.Error("switchToTmuxTargetAndZoom: pre-attach zoom: %v", err)
	}
	return session.SwitchTargetWith(d, target)
}

func sessionDeps(tmux deps.Tmux) *session.Deps {
	return &session.Deps{
		Tmux:   tmux,
		InTmux: func() bool { return os.Getenv("TMUX") != "" },
	}
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
		debug.Error("loadMonitorState: %v", err)
		return nil
	}
	return state
}

// loadMonitorStateAlways loads the monitor state from disk regardless of daemon status.
// Used by the dashboard which needs state even during daemon restarts.
func loadMonitorStateAlways() *monitor.State {
	statePath := monitor.DefaultStatePath()
	state, err := monitor.Load(statePath)
	if err != nil {
		debug.Error("loadMonitorStateAlways: %v", err)
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
	return state.SessionsWithUnread()
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
		debug.Error("capturePanePreview %s: %v", paneID, err)
		return ""
	}
	return out
}

// attentionCallbacks returns the standard callbacks for attention sub-views.
// All monitor mutations are concentrated through a single Store to eliminate
// the duplicated load-find-mutate-save pattern.
func attentionCallbacks() ui.AttentionCallbacks {
	store := monitor.DefaultStore()
	return ui.AttentionCallbacks{
		Preview: capturePanePreview,
		MarkClear: func(paneID string) {
			if err := store.MarkClear(paneID); err != nil {
				debug.Error("markPaneClear %s: %v", paneID, err)
			}
		},
		MarkUnread: func(paneID string) {
			if err := store.MarkUnread(paneID); err != nil {
				debug.Error("markPaneUnread %s: %v", paneID, err)
			}
		},
		ToggleFollow: func(paneID string) {
			if err := store.ToggleFollow(paneID); err != nil {
				debug.Error("togglePaneFollow %s: %v", paneID, err)
			}
		},
		Unmonitor: func(paneID string) {
			if err := store.Remove(paneID); err != nil {
				debug.Error("unmonitorPane %s: %v", paneID, err)
			}
		},
		SetNote: func(paneID, note string) {
			if err := store.SetNote(paneID, note); err != nil {
				debug.Error("setPaneNote %s: %v", paneID, err)
			}
		},
	}
}

func killTmuxSessionByName(sessionName string) {
	killTmuxSessionByNameWith(defaultTmux, sessionName)
}

func killTmuxSessionByNameWith(tmux deps.Tmux, sessionName string) {
	_, err := tmux.Command("kill-session", "-t", sessionName)
	if err != nil {
		debug.Error("killTmuxSessionByName %s: %v", sessionName, err)
		fmt.Fprintf(os.Stderr, "Failed to kill session: %s\n", sessionName)
	} else {
		fmt.Fprintf(os.Stderr, "Killed session: %s\n", sessionName)
	}
}
