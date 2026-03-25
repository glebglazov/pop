package cmd

import (
	"os"
	"sort"
	"strings"

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
	panes := buildDashboardPanes()
	result, err := ui.RunAttention("dashboard", panes, attentionCallbacks(), buildDashboardPanes)
	if err != nil {
		return err
	}

	switch result.Action {
	case ui.ActionSwitchToPane:
		if result.Selected != nil {
			hist, _ := history.Load(history.DefaultHistoryPath())
			if hist == nil {
				hist = &history.History{}
			}
			hist.Record(sessionHistoryPath(result.Selected.Context, hist))
			hist.Save()
			return switchToTmuxTarget(result.Selected.Path)
		}
	case ui.ActionCancel:
		os.Exit(1)
	}

	return nil
}

func buildDashboardPanes() []ui.AttentionPane {
	state := loadMonitorState()
	if state == nil {
		return nil
	}

	entries := state.PanesAll()
	if len(entries) == 0 {
		return nil
	}

	paneCommands := tmuxPaneCommands()

	// Load history for sorting needs_attention panes by recency
	hist, _ := history.Load(history.DefaultHistoryPath())
	accessTimes := make(map[string]int64)
	if hist != nil {
		for _, e := range hist.Entries {
			accessTimes[e.Path] = e.LastAccess.Unix()
		}
	}

	// Build panes with status
	panes := make([]ui.AttentionPane, 0, len(entries))
	for _, entry := range entries {
		name := entry.Session + " " + entry.PaneID
		if cmd, ok := paneCommands[entry.PaneID]; ok {
			name = entry.Session + " " + entry.PaneID + " (" + cmd + ")"
		}

		var status ui.AttentionStatus
		switch entry.Status {
		case monitor.StatusNeedsAttention:
			status = ui.AttentionNeedsAttention
		case monitor.StatusWorking:
			status = ui.AttentionWorking
		default:
			status = ui.AttentionIdle
		}

		panes = append(panes, ui.AttentionPane{
			PaneID:  entry.PaneID,
			Session: entry.Session,
			Name:    name,
			Status:  status,
		})
	}

	// Sort: idle first (top/far from cursor), working in the middle,
	// needs_attention at bottom (closest to cursor).
	// Within each group: sort by history recency (most recent last = closest to cursor).
	statusOrder := map[ui.AttentionStatus]int{
		ui.AttentionIdle:            0,
		ui.AttentionWorking:         1,
		ui.AttentionNeedsAttention:  2,
	}
	sort.Slice(panes, func(i, j int) bool {
		oi, oj := statusOrder[panes[i].Status], statusOrder[panes[j].Status]
		if oi != oj {
			return oi < oj
		}
		// Within same status group: sort by history recency (ascending = oldest first)
		ti := sessionAccessTime(panes[i].Session, hist, accessTimes)
		tj := sessionAccessTime(panes[j].Session, hist, accessTimes)
		if ti != tj {
			return ti < tj
		}
		// Fallback: alphabetical by session name, then pane ID for full determinism
		if panes[i].Session != panes[j].Session {
			return panes[i].Session < panes[j].Session
		}
		return panes[i].PaneID < panes[j].PaneID
	})

	return panes
}

// sessionAccessTime returns the access timestamp for a session.
// It matches the session name against history entries by checking if the
// sanitized base name of a history path matches the session name.
// For worktree sessions (e.g. "game_server/worktrees-and-stuff"), the base
// only captures the last component, so we also try matching against the last
// slash-separated component of the session name as a fallback.
func sessionAccessTime(session string, hist *history.History, accessTimes map[string]int64) int64 {
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
