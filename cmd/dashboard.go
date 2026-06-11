package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/debug"
	"github.com/glebglazov/pop/history"
	"github.com/glebglazov/pop/monitor"
	"github.com/glebglazov/pop/ui"
	"github.com/spf13/cobra"
)

var monitorCmd = &cobra.Command{
	Use:   "monitor",
	Short: "Monitor agent panes",
}

var monitorDashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Show all monitored agent panes",
	Long: `Show all monitored agent panes.

Use --pick to choose a session-local pane for scripts. Picker mode prints
the selected tmux pane ID and does not switch focus or mutate monitor state.`,
	Args: cobra.NoArgs,
	RunE: runDashboard,
}

// Deprecated: use `pop monitor dashboard` instead. Hidden compatibility alias
// for existing keybindings. TODO: remove at the next major CLI change.
var dashboardCmd = &cobra.Command{
	Use:    "dashboard",
	Short:  "Show all monitored agent panes (alias for monitor dashboard)",
	Args:   cobra.NoArgs,
	Hidden: true,
	RunE:   runDashboard,
}

var dashboardPick bool

func init() {
	rootCmd.AddCommand(monitorCmd)
	monitorCmd.AddCommand(monitorDashboardCmd)
	rootCmd.AddCommand(dashboardCmd)
	monitorDashboardCmd.Flags().BoolVar(&dashboardPick, "pick", false, "Pick a session-local pane and print its tmux pane ID")
	dashboardCmd.Flags().BoolVar(&dashboardPick, "pick", false, "Pick a session-local pane and print its tmux pane ID")
}

func runDashboard(cmd *cobra.Command, args []string) error {
	var systemWarnings []string
	if !dashboardPick {
		systemWarnings = ensureSystemState()
	}

	cfg, err := config.Load(config.DefaultConfigPath())
	if err != nil {
		debug.Error("dashboard: load config: %v", err)
	}
	if cfg == nil {
		cfg = &config.Config{}
	}

	cursorPosition := cfg.DashboardCursorPosition()
	var currentPaneID, currentPaneSession string
	if dashboardPick || cursorPosition == config.DashboardCursorCurrentRegistered || cursorPosition == config.DashboardCursorCurrentAny {
		var err error
		currentPaneID, err = defaultTmux.Command("display-message", "-p", "#{pane_id}")
		if err != nil {
			if dashboardPick {
				return fmt.Errorf("cannot determine current tmux session")
			}
			debug.Error("dashboard: get current pane ID: %v", err)
		}
		if currentPaneID != "" {
			currentPaneSession, err = tmuxPaneSessionWith(defaultTmux, currentPaneID)
			if err != nil {
				if dashboardPick {
					return fmt.Errorf("cannot determine current tmux session")
				}
				debug.Error("dashboard: get pane session for %s: %v", currentPaneID, err)
			}
		}
		if dashboardPick && currentPaneSession == "" {
			return fmt.Errorf("cannot determine current tmux session")
		}
	}

	sortCriteria := cfg.DashboardSortCriteria()

	buildPanes := func() []ui.AttentionPane {
		panes, _ := buildDashboardPanesWithCursor(currentPaneID, currentPaneSession, cursorPosition, sortCriteria)
		if dashboardPick {
			panes = sessionLocalDashboardPanes(panes, currentPaneSession, currentPaneID)
		}
		return panes
	}

	// Restore following mode from monitor state
	var opts []ui.DashboardOption
	state := loadMonitorStateAlways()
	if state != nil && state.DashboardFollowing {
		opts = append(opts, ui.WithFollowing(true))
	}
	if len(systemWarnings) > 0 {
		opts = append(opts, ui.WithDashboardWarnings(systemWarnings))
	}
	// Gating the call (not just the badge) also prevents the background Update
	// fetch when [updates] notice_enabled = false.
	if cfg.UpdateNoticeEnabled() {
		if notice := pickerUpdateNotice(); notice != "" {
			opts = append(opts, ui.WithDashboardUpdateNotice(notice))
		}
	}

	panes, initialPaneID := buildDashboardPanesWithCursor(currentPaneID, currentPaneSession, cursorPosition, sortCriteria)
	if dashboardPick {
		panes = sessionLocalDashboardPanes(panes, currentPaneSession, currentPaneID)
		switch len(panes) {
		case 0:
			return fmt.Errorf("no session-local panes")
		case 1:
			fmt.Println(panes[0].PaneID)
			return nil
		}
		initialPaneID = ""
		opts = append(opts, ui.WithDashboardPickerMode(cfg.GetQuickAccessModifier()))
	}
	if initialPaneID != "" {
		opts = append(opts, ui.WithInitialPaneID(initialPaneID))
	}
	result, err := ui.RunDashboard("dashboard", panes, attentionCallbacks(), buildPanes, opts...)
	if err != nil {
		return err
	}

	if dashboardPick {
		if result.Action == ui.DashboardActionConfirm && result.Selected != nil {
			fmt.Println(result.Selected.PaneID)
			return nil
		}
		os.Exit(1)
	}

	// Persist following mode for next dashboard open
	saveDashboardFollowing(result.Following)

	switch result.Action {
	case ui.DashboardActionConfirm:
		if target := handleDashboardSwitch(result, true); target != "" {
			return switchToTmuxTargetAndZoom(target)
		}
	case ui.DashboardActionPeek:
		if target := handleDashboardSwitch(result, false); target != "" {
			return switchToTmuxTargetAndZoom(target)
		}
	case ui.DashboardActionCancel:
		os.Exit(1)
	}

	return nil
}

func sessionLocalDashboardPanes(panes []ui.AttentionPane, session, currentPaneID string) []ui.AttentionPane {
	filtered := make([]ui.AttentionPane, 0, len(panes))
	for _, pane := range panes {
		if pane.Session != session || pane.PaneID == currentPaneID {
			continue
		}
		filtered = append(filtered, pane)
	}
	return filtered
}

// handleDashboardSwitch performs the post-picker bookkeeping for a dashboard
// switch result: records the session in pop's history, and — if dismissUnread
// is true — flips the pane's monitor status from unread to clear. Returns the
// pane ID the caller should switch into, or empty string when nothing should
// happen.
//
// dismissUnread distinguishes the two switch actions:
//   - true  (Enter / ActionSwitchToPane):         clear the unread flag.
//   - false (peek / ActionSwitchToPaneKeepUnread): leave monitor state
//     untouched so the pane remains unread in the dashboard even after
//     the user glances at it.
func handleDashboardSwitch(result ui.DashboardResult, dismissUnread bool) string {
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
	hist.Record(sessionHistoryPath(result.Selected.Session, hist))
	if err := hist.Save(); err != nil {
		debug.Error("dashboard: save history: %v", err)
	}
	if dismissUnread {
		store := monitor.DefaultStore()
		if err := store.DismissUnread(result.Selected.PaneID); err != nil {
			debug.Error("dashboard dismiss-unread %s: %v", result.Selected.PaneID, err)
		}
	}
	return result.Selected.PaneID
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
	panes, _ := buildDashboardPanesWithCursor("", "", config.DashboardCursorFirstActive, config.DefaultSortCriteria)
	return panes
}

func buildDashboardPanesWithCurrentPane(currentPaneID, currentPaneSession string, sortCriteria []string) []ui.AttentionPane {
	panes, _ := buildDashboardPanesWithCursor(currentPaneID, currentPaneSession, config.DashboardCursorCurrentAny, sortCriteria)
	return panes
}

func buildDashboardPanesWithCursor(currentPaneID, currentPaneSession, cursorPosition string, sortCriteria []string) ([]ui.AttentionPane, string) {
	state := loadMonitorStateAlways()
	if state == nil {
		return nil, ""
	}

	entries := state.PanesAll()
	if len(entries) == 0 && currentPaneID == "" {
		return nil, ""
	}

	paneCommands := tmuxPaneCommands()

	// Build panes with status
	panes := make([]ui.AttentionPane, 0, len(entries))
	for _, entry := range entries {
		name := paneAttentionName(entry, paneCommands)

		var status ui.AttentionStatus
		switch entry.Status {
		case monitor.StatusUnread:
			status = ui.AttentionUnread
		case monitor.StatusWorking:
			status = ui.AttentionWorking
		default:
			status = ui.AttentionClear
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

	// Build per-pane last-active timestamps from monitor state
	paneLastActiveAt := make(map[string]int64)
	registered := make(map[string]bool)
	for _, entry := range entries {
		registered[entry.PaneID] = true
		if !entry.LastActiveAt.IsZero() {
			paneLastActiveAt[entry.PaneID] = entry.LastActiveAt.Unix()
		}
	}

	if cursorPosition == config.DashboardCursorCurrentAny && currentPaneID != "" {
		paneLastActiveAt[currentPaneID] = time.Now().Unix()
		if !registered[currentPaneID] {
			panes = append(panes, virtualDashboardPane(currentPaneID, currentPaneSession, paneCommands))
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

	sortDashboardPanes(panes, paneLastActiveAt, sessionLastVisit, sortCriteria)

	initialPaneID := dashboardInitialPaneID(panes, paneLastActiveAt, currentPaneID, cursorPosition)
	if initialPaneID == "" && cursorPosition != config.DashboardCursorFirstActive {
		initialPaneID = dashboardInitialPaneID(panes, paneLastActiveAt, "", config.DashboardCursorFirstActive)
	}

	return panes, initialPaneID
}

// sortDashboardPanes sorts panes according to the configured criteria chain.
// Each criterion is applied in order; the first one that distinguishes two
// panes wins. All criteria sort ascending (oldest/lowest first), so the
// most-recent / highest-priority items end up at the bottom (closest to cursor).
func sortDashboardPanes(panes []ui.AttentionPane, paneLastActiveAt map[string]int64, sessionLastVisit map[string]int64, criteria []string) {
	statusOrder := map[ui.AttentionStatus]int{
		ui.AttentionClear:   0,
		ui.AttentionVirtual: 0,
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
			case config.SortByPaneLastActiveAt, config.SortByPaneLastVisitAt:
				vi, vj := paneLastActiveAt[panes[i].PaneID], paneLastActiveAt[panes[j].PaneID]
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
// end. If not, it is injected as a virtual (unmonitored) entry.
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

	// Not in list — inject as virtual
	name := session + " (" + currentPaneID + ")"
	if cmd, ok := paneCommands[currentPaneID]; ok {
		name = session + " (" + currentPaneID + ", " + cmd + ")"
	}
	return append(panes, ui.AttentionPane{
		PaneID:  currentPaneID,
		Session: session,
		Name:    name,
		Status:  ui.AttentionVirtual,
	})
}

func virtualDashboardPane(currentPaneID, session string, paneCommands map[string]string) ui.AttentionPane {
	name := session + " (" + currentPaneID + ")"
	if cmd, ok := paneCommands[currentPaneID]; ok {
		name = session + " (" + currentPaneID + ", " + cmd + ")"
	}
	return ui.AttentionPane{
		PaneID:  currentPaneID,
		Session: session,
		Name:    name,
		Status:  ui.AttentionVirtual,
	}
}

func dashboardInitialPaneID(panes []ui.AttentionPane, paneLastActiveAt map[string]int64, currentPaneID, cursorPosition string) string {
	switch cursorPosition {
	case config.DashboardCursorCurrentRegistered, config.DashboardCursorCurrentAny:
		if currentPaneID == "" {
			return ""
		}
		for _, pane := range panes {
			if pane.PaneID == currentPaneID {
				return currentPaneID
			}
		}
		return ""
	case config.DashboardCursorFirstActive:
		if paneID := lastPaneWithStatus(panes, ui.AttentionUnread); paneID != "" {
			return paneID
		}
		if paneID := lastPaneWithStatus(panes, ui.AttentionWorking); paneID != "" {
			return paneID
		}
		return mostRecentlyActivePane(panes, paneLastActiveAt)
	default:
		return ""
	}
}

func lastPaneWithStatus(panes []ui.AttentionPane, status ui.AttentionStatus) string {
	for i := len(panes) - 1; i >= 0; i-- {
		if panes[i].Status == status {
			return panes[i].PaneID
		}
	}
	return ""
}

func mostRecentlyActivePane(panes []ui.AttentionPane, paneLastActiveAt map[string]int64) string {
	var paneID string
	var lastActive int64
	for _, pane := range panes {
		activeAt := paneLastActiveAt[pane.PaneID]
		if activeAt > lastActive {
			paneID = pane.PaneID
			lastActive = activeAt
		}
	}
	return paneID
}

// sessionAccessTime returns the pop history access timestamp for a session.
// It matches the session name against history entries using the same Session
// name rules as project/worktree pickers.
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
		entrySession := historyEntrySessionName(e.Path)
		if entrySession == session {
			return e.LastAccess.Unix()
		}
		if partial == 0 && entrySession == lastComponent {
			partial = e.LastAccess.Unix()
		}
	}
	return partial
}

// applyPaneLabel stores an optional display label on a pane entry. Hooks pass
// --label so the dashboard shows "cursor" instead of tmux's pane_current_command
// (often "node" for Node-based agents).
func applyPaneLabel(entry *monitor.PaneEntry, label string) {
	if label != "" {
		entry.Label = label
	}
}

// paneProcessLabel returns the process label shown in dashboard names.
func paneProcessLabel(entry *monitor.PaneEntry, paneCommands map[string]string) string {
	if entry.Label != "" {
		return entry.Label
	}
	return paneCommands[entry.PaneID]
}

func paneAttentionName(entry *monitor.PaneEntry, paneCommands map[string]string) string {
	if entry.Note != "" {
		return entry.Session + " (" + entry.Note + ")"
	}
	if cmd := paneProcessLabel(entry, paneCommands); cmd != "" {
		return entry.Session + " (" + entry.PaneID + ", " + cmd + ")"
	}
	return entry.Session + " (" + entry.PaneID + ")"
}
