package cmd

import (
	"os"
	"sort"
	"strings"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/debug"
	"github.com/glebglazov/pop/history"
	"github.com/glebglazov/pop/monitor"
	"github.com/glebglazov/pop/ui"
	"github.com/spf13/cobra"
)

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Show all monitored agent panes",
	Args:  cobra.NoArgs,
	RunE:  runDashboard,
}

func init() {
	rootCmd.AddCommand(dashboardCmd)
}

func runDashboard(cmd *cobra.Command, args []string) error {
	go ensureMonitorDaemon()

	cfg, err := config.Load(config.DefaultConfigPath())
	if err != nil {
		debug.Error("dashboard: load config: %v", err)
	}
	if cfg == nil {
		cfg = &config.Config{}
	}

	var currentPaneID, currentPaneSession string
	if cfg.CurrentPaneAlwaysUnderCursor() {
		var err error
		currentPaneID, err = defaultTmux.Command("display-message", "-p", "#{pane_id}")
		if err != nil {
			debug.Error("dashboard: get current pane ID: %v", err)
		}
		if currentPaneID != "" {
			currentPaneSession, err = tmuxPaneSessionWith(defaultTmux, currentPaneID)
			if err != nil {
				debug.Error("dashboard: get pane session for %s: %v", currentPaneID, err)
			}
		}
	}

	sortCriteria := cfg.DashboardSortCriteria()

	buildPanes := func() []ui.AttentionPane {
		return buildDashboardPanesWithCurrentPane(currentPaneID, currentPaneSession, sortCriteria)
	}

	// Restore following mode from monitor state
	var opts []ui.PickerOption
	state := loadMonitorStateAlways()
	if state != nil && state.DashboardFollowing {
		opts = append(opts, ui.WithAttentionFollowing(true))
	}

	panes := buildPanes()
	result, err := ui.RunAttention("dashboard", panes, attentionCallbacks(), buildPanes, opts...)
	if err != nil {
		return err
	}

	// Persist following mode for next dashboard open
	saveDashboardFollowing(result.AttentionFollowing)

	switch result.Action {
	case ui.ActionSwitchToPane:
		if target := handleDashboardSwitch(result, true); target != "" {
			return switchToTmuxTargetAndZoom(target)
		}
	case ui.ActionSwitchToPaneKeepUnread:
		if target := handleDashboardSwitch(result, false); target != "" {
			return switchToTmuxTargetAndZoom(target)
		}
	case ui.ActionCancel:
		os.Exit(1)
	}

	return nil
}

// handleDashboardSwitch performs the post-picker bookkeeping for a dashboard
// switch result: records the session in pop's history, and — if dismissUnread
// is true — flips the pane's monitor status from unread to idle. Returns the
// pane ID the caller should switch into, or empty string when nothing should
// happen.
//
// dismissUnread distinguishes the two switch actions:
//   - true  (Enter / ActionSwitchToPane):         clear the unread flag.
//   - false (peek / ActionSwitchToPaneKeepUnread): leave monitor state
//     untouched so the pane remains unread in the dashboard even after
//     the user glances at it.
func handleDashboardSwitch(result ui.Result, dismissUnread bool) string {
	if result.Selected == nil {
		return ""
	}
	hist, err := history.Load(history.DefaultHistoryPath())
	if err != nil {
		debug.Error("dashboard: load history: %v", err)
	}
	if hist == nil {
		hist = &history.History{}
	}
	hist.Record(sessionHistoryPath(result.Selected.Context, hist))
	if err := hist.Save(); err != nil {
		debug.Error("dashboard: save history: %v", err)
	}
	if dismissUnread {
		dismissUnreadPane(result.Selected.Path)
	}
	return result.Selected.Path
}

func saveDashboardFollowing(following bool) {
	state := loadMonitorStateAlways()
	if state == nil {
		return
	}
	if state.DashboardFollowing == following {
		return
	}
	state.DashboardFollowing = following
	if err := state.SaveWith(monitor.DefaultDeps()); err != nil {
		debug.Error("saveDashboardFollowing: save: %v", err)
	}
}

func buildDashboardPanes() []ui.AttentionPane {
	return buildDashboardPanesWithCurrentPane("", "", config.DefaultSortCriteria)
}

func buildDashboardPanesWithCurrentPane(currentPaneID, currentPaneSession string, sortCriteria []string) []ui.AttentionPane {
	state := loadMonitorStateAlways()
	if state == nil {
		return nil
	}

	entries := state.PanesAll()
	if len(entries) == 0 && currentPaneID == "" {
		return nil
	}

	paneCommands := tmuxPaneCommands()

	// Build panes with status
	panes := make([]ui.AttentionPane, 0, len(entries))
	for _, entry := range entries {
		var name string
		if entry.Note != "" {
			name = entry.Session + " (" + entry.Note + ")"
		} else if cmd, ok := paneCommands[entry.PaneID]; ok {
			name = entry.Session + " (" + entry.PaneID + ", " + cmd + ")"
		} else {
			name = entry.Session + " (" + entry.PaneID + ")"
		}

		var status ui.AttentionStatus
		switch entry.Status {
		case monitor.StatusUnread:
			status = ui.AttentionUnread
		case monitor.StatusWorking:
			status = ui.AttentionWorking
		default:
			status = ui.AttentionIdle
		}

		panes = append(panes, ui.AttentionPane{
			PaneID:    entry.PaneID,
			Session:   entry.Session,
			Name:      name,
			Note:      entry.Note,
			Status:    status,
			Following: entry.Following,
		})
	}

	// Build per-pane last-visited timestamps from monitor state
	paneLastVisited := make(map[string]int64)
	for _, entry := range entries {
		if !entry.LastVisited.IsZero() {
			paneLastVisited[entry.PaneID] = entry.LastVisited.Unix()
		}
	}

	// Build per-session last-visit timestamps from pop history
	hist, err := history.Load(history.DefaultHistoryPath())
	if err != nil {
		debug.Error("buildDashboardPanes: load history: %v", err)
	}
	sessionLastVisit := make(map[string]int64)
	for _, p := range panes {
		if _, ok := sessionLastVisit[p.Session]; !ok {
			sessionLastVisit[p.Session] = sessionAccessTime(p.Session, hist)
		}
	}

	sortDashboardPanes(panes, paneLastVisited, sessionLastVisit, sortCriteria)

	if currentPaneID != "" {
		panes = positionCurrentPane(panes, currentPaneID, currentPaneSession, paneCommands)
	}

	return panes
}

// sortDashboardPanes sorts panes according to the configured criteria chain.
// Each criterion is applied in order; the first one that distinguishes two
// panes wins. All criteria sort ascending (oldest/lowest first), so the
// most-recent / highest-priority items end up at the bottom (closest to cursor).
func sortDashboardPanes(panes []ui.AttentionPane, paneLastVisited map[string]int64, sessionLastVisit map[string]int64, criteria []string) {
	statusOrder := map[ui.AttentionStatus]int{
		ui.AttentionIdle:    0,
		ui.AttentionWorking: 1,
		ui.AttentionUnread:  2,
	}

	sort.SliceStable(panes, func(i, j int) bool {
		for _, c := range criteria {
			switch c {
			case config.SortByStatus:
				oi, oj := statusOrder[panes[i].Status], statusOrder[panes[j].Status]
				if oi != oj {
					return oi < oj
				}
			case config.SortByPaneLastVisitAt:
				vi, vj := paneLastVisited[panes[i].PaneID], paneLastVisited[panes[j].PaneID]
				if vi != vj {
					return vi < vj
				}
			case config.SortBySessionLastVisitAt:
				si, sj := sessionLastVisit[panes[i].Session], sessionLastVisit[panes[j].Session]
				if si != sj {
					return si < sj
				}
			case config.SortByAlphabetical:
				if panes[i].Session != panes[j].Session {
					return panes[i].Session < panes[j].Session
				}
				if panes[i].PaneID != panes[j].PaneID {
					return panes[i].PaneID < panes[j].PaneID
				}
			}
		}
		// Implicit final tiebreaker: pane ID for deterministic order
		// (PanesAll iterates a map, so input order is non-deterministic)
		return panes[i].PaneID < panes[j].PaneID
	})
}

// positionCurrentPane ensures the current pane is at the end of the list
// (under the cursor). If the pane is already in the list, it is moved to the
// end. If not, it is injected as an idle (unmonitored) entry.
func positionCurrentPane(panes []ui.AttentionPane, currentPaneID, session string, paneCommands map[string]string) []ui.AttentionPane {
	idx := -1
	for i, p := range panes {
		if p.PaneID == currentPaneID {
			idx = i
			break
		}
	}

	if idx >= 0 {
		if idx == len(panes)-1 {
			return panes
		}
		current := panes[idx]
		result := make([]ui.AttentionPane, 0, len(panes))
		result = append(result, panes[:idx]...)
		result = append(result, panes[idx+1:]...)
		result = append(result, current)
		return result
	}

	// Not in list — inject as idle
	name := session + " (" + currentPaneID + ")"
	if cmd, ok := paneCommands[currentPaneID]; ok {
		name = session + " (" + currentPaneID + ", " + cmd + ")"
	}
	return append(panes, ui.AttentionPane{
		PaneID:  currentPaneID,
		Session: session,
		Name:    name,
		Status:  ui.AttentionIdle,
	})
}

// sessionAccessTime returns the pop history access timestamp for a session.
// It matches the session name against history entries by checking if the
// sanitized base name of a history path matches the session name.
func sessionAccessTime(session string, hist *history.History) int64 {
	if hist == nil {
		return 0
	}
	lastComponent := session
	if i := strings.LastIndex(session, "/"); i >= 0 {
		lastComponent = session[i+1:]
	}
	var partial int64
	for _, e := range hist.Entries {
		sanitizedBase := sanitizeSessionName(pathBase(e.Path))
		if sanitizedBase == session {
			return e.LastAccess.Unix()
		}
		if partial == 0 && sanitizedBase == lastComponent {
			partial = e.LastAccess.Unix()
		}
	}
	return partial
}

// pathBase returns the last element of a path, handling the tmux: prefix
func pathBase(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[i+1:]
		}
	}
	return path
}

