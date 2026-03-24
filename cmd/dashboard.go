package cmd

import (
	"os"
	"sort"

	"github.com/glebglazov/pop/history"
	"github.com/glebglazov/pop/monitor"
	"github.com/glebglazov/pop/ui"
	"github.com/spf13/cobra"
)

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Show active agent panes (working and needs attention)",
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

	entries := state.PanesActive()
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

		status := ui.AttentionWorking
		if entry.Status == monitor.StatusNeedsAttention {
			status = ui.AttentionNeedsAttention
		}

		panes = append(panes, ui.AttentionPane{
			PaneID:  entry.PaneID,
			Session: entry.Session,
			Name:    name,
			Status:  status,
		})
	}

	// Sort: working panes first (top), needs_attention at bottom (closer to cursor).
	// Within needs_attention: sort by history recency (most recent last = closest to cursor).
	// Within working: sort by session name for stability.
	sort.SliceStable(panes, func(i, j int) bool {
		si, sj := panes[i].Status, panes[j].Status
		if si != sj {
			// Working (1) before NeedsAttention (0) — working sorts to top
			return si > sj
		}
		if si == ui.AttentionNeedsAttention {
			// Within needs_attention: sort by session history recency (ascending = oldest first)
			ti := sessionAccessTime(panes[i].Session, hist, accessTimes)
			tj := sessionAccessTime(panes[j].Session, hist, accessTimes)
			if ti != tj {
				return ti < tj
			}
		}
		// Fallback: alphabetical by session name
		return panes[i].Session < panes[j].Session
	})

	return panes
}

// sessionAccessTime returns the access timestamp for a session.
// It matches the session name against history entries by checking if the
// sanitized base name of a history path matches the session name.
func sessionAccessTime(session string, hist *history.History, accessTimes map[string]int64) int64 {
	if hist == nil {
		return 0
	}
	for _, e := range hist.Entries {
		if sanitizeSessionName(pathBase(e.Path)) == session {
			return e.LastAccess.Unix()
		}
	}
	return 0
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
