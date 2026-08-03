package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/glebglazov/pop/debug"
	"github.com/glebglazov/pop/history"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/monitor"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/ui"
	"github.com/glebglazov/pop/work/ref"
)

// defaultTmuxMod is the production tmux module handle used by all tmux
// side effects in cmd (ADR-0142).
var defaultTmuxMod tmuxmod.Tmux = tmuxmod.New()

const (
	tmuxSessionPathPrefix = "tmux:"
	iconDirSession        = "■"
	iconStandaloneSession = "□"
	iconAttention         = ui.IconAttention
	// The two managed-worktree markers are a pair: every pop-managed checkout
	// carries one of them, so a blank marker column means "human worktree"
	// rather than "not classified yet" (ADR-0152).
	iconUnboundManaged = "U"
	iconBoundManaged   = "M"
	// Work-session badges, one per Work kind. A session hosting a Work container
	// is a different animal from a project's session — you are in it to decide,
	// drain or fire something, not to sit in a checkout — so it gets its own
	// marker column entry.
	iconMapSession     = "◆"
	iconTaskSetSession = "▲"
	iconRoutineSession = "●"
)

// checkoutSessionName is the naming call for surfaces that open a session for a
// checkout with a human watching. It is project.SessionName plus the diagnosis:
// when git cannot answer for the checkout the session name loses its <project>/
// prefix, which silently makes one checkout reachable under two names, so the
// cause is printed to stderr instead of only reaching the debug log. The
// best-effort name is still returned — a broken trunk must not stop the operator
// getting into their worktree.
func checkoutSessionName(path string) string {
	name, err := project.SessionNameFor(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pop: %v\n", err)
		debug.Error("session name: %v", err)
	}
	return name
}

// workSessionBadge maps a session's stamped Work kind to its picker marker. An
// unknown kind badges nothing rather than guessing: the vocabulary is closed and
// a fourth kind will arrive with its own badge.
func workSessionBadge(kind string) string {
	switch ref.Kind(kind) {
	case ref.KindMap:
		return iconMapSession
	case ref.KindTaskSet:
		return iconTaskSetSession
	case ref.KindRoutine:
		return iconRoutineSession
	default:
		return ""
	}
}

// tmuxWorkSessions maps live session name -> its Work stamp. The stamp, not the
// session name, is what says a session hosts Work: names are pop's convention
// and would go stale the moment one changed, while the option travels with the
// session it describes.
func tmuxWorkSessions() map[string]tmuxmod.WorkSession {
	return tmuxWorkSessionsWith(defaultTmuxMod)
}

func tmuxWorkSessionsWith(mod tmuxmod.Tmux) map[string]tmuxmod.WorkSession {
	sessions, err := mod.WorkSessions()
	if err != nil {
		debug.Error("tmuxWorkSessions: %v", err)
		return nil
	}
	out := make(map[string]tmuxmod.WorkSession, len(sessions))
	for _, s := range sessions {
		out[s.Session] = s
	}
	return out
}

func currentTmuxSession() string {
	return currentTmuxSessionWith(defaultTmuxMod)
}

func currentTmuxSessionWith(mod tmuxmod.Tmux) string {
	out, err := mod.CurrentSession()
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
	return switchToTmuxTargetWith(defaultTmuxMod, target)
}

func switchToTmuxTargetWith(mod tmuxmod.Tmux, target string) error {
	return tmuxmod.SwitchTarget(mod, target)
}

// switchToTmuxTargetAndZoom switches to a tmux pane and zooms it
func switchToTmuxTargetAndZoom(target string) error {
	return switchToTmuxTargetAndZoomWith(defaultTmuxMod, target)
}

func switchToTmuxTargetAndZoomWith(mod tmuxmod.Tmux, target string) error {
	return tmuxmod.SwitchAndZoom(mod, target)
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
	statePath := cmdMonitorStatePath()
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
	return tmuxPaneCommandsWith(defaultTmuxMod)
}

func tmuxPaneCommandsWith(mod tmuxmod.Tmux) map[string]string {
	commands, err := mod.PaneCommands()
	if err != nil {
		return nil
	}
	return commands
}

// tmuxPaneTopics returns a map of pane ID → Topic for all panes. The Topic is
// a per-pane tmux user-option owned by pop's tmux module (ADR 0058), so the
// dashboard reads it straight off tmux rather than from monitor state. Panes
// with no (or an empty) Topic are omitted from the map.
func tmuxPaneTopics() map[string]string {
	return tmuxPaneTopicsWith(defaultTmuxMod)
}

func tmuxPaneTopicsWith(tmux tmuxmod.Tmux) map[string]string {
	topics, err := tmux.PaneTopics()
	if err != nil {
		return nil
	}
	return topics
}

// capturePanePreview captures the last 50 lines of a tmux pane for preview display
// sessionHistoryPath returns the history path to record for a given tmux session name.
// It searches existing history entries for one whose Session name matches,
// falling back to tmux:<sessionName> for standalone sessions.
//
// Bare-repo sessions use "repo/worktree" names; we first try an exact
// SessionName match, then fall back to matching the last slash-separated
// component of the session name (e.g. "worktrees-and-stuff" from
// "game_server/worktrees-and-stuff").
func sessionHistoryPath(sessionName string, hist *history.History) string {
	lastComponent := sessionName
	if i := strings.LastIndex(sessionName, "/"); i >= 0 {
		lastComponent = sessionName[i+1:]
	}

	var partialMatch string
	for _, e := range hist.Entries {
		entrySession := historyEntrySessionName(e.Path)
		if entrySession == sessionName {
			return e.Path
		}
		if partialMatch == "" && entrySession == lastComponent {
			partialMatch = e.Path
		}
	}
	if partialMatch != "" {
		return partialMatch
	}
	return tmuxSessionPathPrefix + sessionName
}

func historyEntrySessionName(path string) string {
	if strings.HasPrefix(path, tmuxSessionPathPrefix) {
		return strings.TrimPrefix(path, tmuxSessionPathPrefix)
	}
	// FastSessionName avoids git commands; see ADR 0005.
	return project.FastSessionName(path)
}

func capturePanePreview(paneID string) string {
	return capturePanePreviewWith(defaultTmuxMod, paneID)
}

func capturePanePreviewWith(mod tmuxmod.Tmux, paneID string) string {
	out, err := mod.CapturePreview(paneID)
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
	}
}

func killTmuxSessionByName(sessionName string) {
	killTmuxSessionByNameWith(defaultTmuxMod, sessionName)
}

func killTmuxSessionByNameWith(mod tmuxmod.Tmux, sessionName string) {
	err := mod.KillSession(sessionName)
	if err != nil {
		debug.Error("killTmuxSessionByName %s: %v", sessionName, err)
		fmt.Fprintf(os.Stderr, "Failed to kill session: %s\n", sessionName)
	} else {
		fmt.Fprintf(os.Stderr, "Killed session: %s\n", sessionName)
	}
}
