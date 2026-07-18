package ui

import (
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// spinnerFrames are the animation frames for working panes
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// spinnerTickMsg is sent periodically to advance the spinner animation
type spinnerTickMsg struct{}

func spinnerTick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
		return spinnerTickMsg{}
	})
}

// reloadTickMsg triggers a periodic reload of attention panes
type reloadTickMsg struct{}

func reloadTick() tea.Cmd {
	return tea.Tick(1*time.Second, func(time.Time) tea.Msg {
		return reloadTickMsg{}
	})
}

// AttentionStatus indicates why a pane appears in the attention view
type AttentionStatus int

const (
	AttentionClear AttentionStatus = iota
	AttentionWorking
	AttentionUnread
	AttentionVirtual
)

// AttentionPane represents a pane that needs user attention
type AttentionPane struct {
	PaneID    string
	Session   string
	Name      string
	Status    AttentionStatus
	Following bool
	// TopicDerived marks Name's parenthetical as a machine-derived Topic.
	// When set, the name is rendered dimmed to signal it was set by an agent.
	TopicDerived bool
}

// AttentionCallbacks holds callback functions for the attention sub-view.
type AttentionCallbacks struct {
	Preview      func(paneID string) string // returns pane content for preview
	MarkClear    func(paneID string)        // marks a pane as clear
	MarkUnread   func(paneID string)        // marks a pane as unread
	ToggleFollow func(paneID string)        // toggles following flag
	Unmonitor    func(paneID string)        // removes a pane from monitor state
}

// MonitorDashboardAction represents what action the user wants to take in the dashboard
type MonitorDashboardAction int

const (
	MonitorDashboardActionCancel MonitorDashboardAction = iota
	MonitorDashboardActionConfirm
	MonitorDashboardActionPeek
	MonitorDashboardActionRefresh
)

// MonitorDashboardResult holds the dashboard result
type MonitorDashboardResult struct {
	Selected    *AttentionPane
	Action      MonitorDashboardAction
	Following   bool
	CursorIndex int
}

// MonitorDashboard is a tea.Model for browsing monitored panes
type MonitorDashboard struct {
	panes    []AttentionPane
	allPanes []AttentionPane // full list (source of truth)
	list     *List[AttentionPane]
	cursor   int // synced from list; kept for test access
	width    int
	height   int
	result   MonitorDashboardResult

	following    bool
	dirty        bool
	preview      string
	title        string
	emptyNote    string
	spinnerFrame int

	showHelp bool

	previewFunc      func(paneID string) string
	reloadFunc       func() []AttentionPane
	markClearFunc    func(paneID string)
	markUnreadFunc   func(paneID string)
	toggleFollowFunc func(paneID string)
	unmonitorFunc    func(paneID string)

	warnings []string

	// updateNotice is the dimmed top-right Update notice text (empty = none).
	updateNotice string

	initialPaneID        string
	protectedPaneID      string
	protectedCursorIndex int

	pickerMode          bool
	quickAccessModifier string
	quickAccess         *QuickAccess
}

// MonitorDashboardOption configures the dashboard
type MonitorDashboardOption func(*MonitorDashboard)

// WithFollowing sets the initial following mode for the dashboard.
func WithFollowing(following bool) MonitorDashboardOption {
	return func(d *MonitorDashboard) {
		d.following = following
	}
}

// WithInitialPaneID selects the initial dashboard cursor by pane ID.
func WithInitialPaneID(paneID string) MonitorDashboardOption {
	return func(d *MonitorDashboard) {
		d.initialPaneID = paneID
	}
}

// WithMonitorDashboardWarnings adds warning messages to display in the dashboard.
func WithMonitorDashboardWarnings(warnings []string) MonitorDashboardOption {
	return func(d *MonitorDashboard) {
		d.warnings = warnings
	}
}

// WithMonitorDashboardUpdateNotice sets the dimmed top-right Update notice text. Empty
// text shows nothing. The notice occupies a reserved top line so it never
// shifts the pane list or preview.
func WithMonitorDashboardUpdateNotice(text string) MonitorDashboardOption {
	return func(d *MonitorDashboard) {
		d.updateNotice = text
	}
}

// WithEmptyNote sets a note line shown below the "No panes need attention" message.
func WithEmptyNote(note string) MonitorDashboardOption {
	return func(d *MonitorDashboard) {
		d.emptyNote = note
	}
}

// WithMonitorDashboardPickerMode makes the dashboard a pure selection UI.
func WithMonitorDashboardPickerMode(quickAccessModifier string) MonitorDashboardOption {
	return func(d *MonitorDashboard) {
		d.pickerMode = true
		d.quickAccessModifier = quickAccessModifier
	}
}

// NewMonitorDashboard creates a new dashboard with the given panes and callbacks
func NewMonitorDashboard(panes []AttentionPane, cb AttentionCallbacks, reloadFn func() []AttentionPane, opts ...MonitorDashboardOption) *MonitorDashboard {
	d := &MonitorDashboard{
		allPanes:         panes,
		panes:            make([]AttentionPane, len(panes)),
		height:           10,
		previewFunc:      cb.Preview,
		reloadFunc:       reloadFn,
		markClearFunc:    cb.MarkClear,
		markUnreadFunc:   cb.MarkUnread,
		toggleFollowFunc: cb.ToggleFollow,
		unmonitorFunc:    cb.Unmonitor,
	}
	copy(d.panes, panes)
	for _, opt := range opts {
		opt(d)
	}
	d.initList()
	return d
}

func (d *MonitorDashboard) initList() {
	modifier := d.quickAccessModifier
	if !d.pickerMode {
		modifier = "disabled"
	}
	d.quickAccess = NewQuickAccess(modifier)
	scrollMargin := 0
	if d.quickAccess.Enabled() {
		scrollMargin = 9
	}
	d.list = NewList(d.panes, Opts[AttentionPane]{
		Key:          func(p AttentionPane) string { return p.PaneID },
		Wrap:         true,
		Anchor:       AnchorBottom,
		ScrollMargin: scrollMargin,
		QuickLabel:   d.quickAccess.LabelFunc(),
	})
	d.list.opts.Cell = d.dashboardCell
}

func (d *MonitorDashboard) syncFromList() {
	d.cursor = d.list.Cursor()
}

func (d *MonitorDashboard) syncToList() {
	if d.cursor != d.list.Cursor() {
		d.list.SetCursor(d.cursor)
	}
}

func (d *MonitorDashboard) listBodyHeight() int {
	return d.height + 2
}

func (d *MonitorDashboard) leftWidth() int {
	leftWidth := d.width * 3 / 10
	if leftWidth < 15 {
		leftWidth = 15
	}
	return leftWidth
}

func (d *MonitorDashboard) syncPanesToList() {
	if d.protectedPaneID != "" {
		d.list.SetItems(d.panes)
		d.list.SetCursor(d.cursor)
	} else {
		d.list.ReplaceItems(d.panes)
	}
	d.syncFromList()
}

func (d *MonitorDashboard) hasWorkingPanes() bool {
	for _, pane := range d.panes {
		if pane.Status == AttentionWorking {
			return true
		}
	}
	return false
}

// Init implements tea.Model
func (d *MonitorDashboard) Init() tea.Cmd {
	if len(d.panes) > 0 {
		d.list.SetCursor(len(d.panes) - 1)
		if d.initialPaneID != "" {
			d.list.SetCursorToKey(d.initialPaneID)
		}
	}
	d.syncFromList()
	var cmds []tea.Cmd
	if d.hasWorkingPanes() {
		cmds = append(cmds, spinnerTick())
	}
	if d.reloadFunc != nil {
		cmds = append(cmds, reloadTick())
	}
	return tea.Batch(cmds...)
}

// Update implements tea.Model
func (d *MonitorDashboard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	d.syncToList()

	switch msg.(type) {
	case spinnerTickMsg:
		d.spinnerFrame = (d.spinnerFrame + 1) % len(spinnerFrames)
		if d.hasWorkingPanes() {
			return d, spinnerTick()
		}
		return d, nil
	case reloadTickMsg:
		if d.reloadFunc != nil {
			hadWorking := d.hasWorkingPanes()
			d.reloadPanes()
			cmds := []tea.Cmd{reloadTick()}
			if !hadWorking && d.hasWorkingPanes() {
				cmds = append(cmds, spinnerTick())
			}
			return d, tea.Batch(cmds...)
		}
		return d, nil
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		// Help overlay: toggle, dismiss, or swallow keys while open.
		if ToggleHelp(&d.showHelp, msg) {
			return d, nil
		}

		switch {
		case key.Matches(msg, dashboardKeys.Back):
			if d.dirty {
				d.result = MonitorDashboardResult{Action: MonitorDashboardActionRefresh}
				return d, tea.Quit
			}
			return d, tea.Quit

		case key.Matches(msg, dashboardKeys.Quit):
			if msg.Code == 0x1b { // esc
				if d.dirty {
					d.result = MonitorDashboardResult{Action: MonitorDashboardActionRefresh}
					return d, tea.Quit
				}
				return d, tea.Quit
			}
			// ctrl+c — quit
			d.result = MonitorDashboardResult{Action: MonitorDashboardActionCancel}
			return d, tea.Quit

		case key.Matches(msg, dashboardKeys.Enter):
			if len(d.panes) == 0 {
				d.result = MonitorDashboardResult{Action: MonitorDashboardActionCancel}
				return d, tea.Quit
			}
			pane := d.panes[d.cursor]
			d.result = MonitorDashboardResult{
				Selected: &pane,
				Action:   MonitorDashboardActionConfirm,
			}
			return d, tea.Quit

		case key.Matches(msg, dashboardKeys.PeekPane):
			if d.pickerMode {
				return d, nil
			}
			// "Peek" — open the pane without mutating its monitor state.
			if len(d.panes) == 0 {
				return d, nil
			}
			pane := d.panes[d.cursor]
			d.result = MonitorDashboardResult{
				Selected: &pane,
				Action:   MonitorDashboardActionPeek,
			}
			return d, tea.Quit

		case key.Matches(msg, dashboardKeys.Up):
			if len(d.panes) > 0 {
				d.clearProtectedPane()
				d.list.MoveUp()
				d.syncFromList()
				d.fetchPreview()
			}
			return d, nil

		case key.Matches(msg, dashboardKeys.Down):
			if len(d.panes) > 0 {
				d.clearProtectedPane()
				d.list.MoveDown()
				d.syncFromList()
				d.fetchPreview()
			}
			return d, nil

		case key.Matches(msg, dashboardKeys.ToggleClearUnread):
			if d.pickerMode {
				return d, nil
			}
			if len(d.panes) > 0 && d.markClearFunc != nil && d.markUnreadFunc != nil {
				pane := &d.panes[d.cursor]
				if pane.Status == AttentionVirtual {
					return d, nil
				}
				d.protectSelectedPane()
				if pane.Status == AttentionClear {
					d.markUnreadFunc(pane.PaneID)
					pane.Status = AttentionUnread
					d.updateAllPanesStatus(pane.PaneID, AttentionUnread)
				} else {
					d.markClearFunc(pane.PaneID)
					pane.Status = AttentionClear
					d.updateAllPanesStatus(pane.PaneID, AttentionClear)
				}
				d.sortPanes()
				if d.cursor >= len(d.panes) {
					d.cursor = len(d.panes) - 1
					d.list.SetCursor(d.cursor)
				}
				d.dirty = true
				d.fetchPreview()
			}
			return d, nil

		case key.Matches(msg, dashboardKeys.MarkUnread):
			if d.pickerMode {
				return d, nil
			}
			if len(d.panes) > 0 && d.markUnreadFunc != nil {
				pane := &d.panes[d.cursor]
				if pane.Status == AttentionVirtual {
					return d, nil
				}
				d.protectSelectedPane()
				d.markUnreadFunc(pane.PaneID)
				pane.Status = AttentionUnread
				d.updateAllPanesStatus(pane.PaneID, AttentionUnread)
				d.sortPanes()
				if d.cursor >= len(d.panes) {
					d.cursor = len(d.panes) - 1
					d.list.SetCursor(d.cursor)
				}
				d.dirty = true
				d.fetchPreview()
			}
			return d, nil

		case key.Matches(msg, dashboardKeys.FollowPane):
			if d.pickerMode {
				return d, nil
			}
			if len(d.panes) > 0 && d.toggleFollowFunc != nil {
				pane := &d.panes[d.cursor]
				if pane.Status == AttentionVirtual {
					return d, nil
				}
				d.toggleFollowFunc(pane.PaneID)
				pane.Following = !pane.Following
				// Update source-of-truth list
				for i := range d.allPanes {
					if d.allPanes[i].PaneID == pane.PaneID {
						d.allPanes[i].Following = pane.Following
						break
					}
				}
				d.dirty = true
				// If in following view and we just unfollowed, rebuild to remove it
				if d.following && !pane.Following {
					d.rebuildView()
				}
			}
			return d, nil

		case key.Matches(msg, dashboardKeys.ToggleFollowView):
			d.following = !d.following
			d.rebuildView()
			if d.hasWorkingPanes() {
				return d, spinnerTick()
			}
			return d, nil

		case key.Matches(msg, dashboardKeys.Unmonitor):
			if d.pickerMode {
				return d, nil
			}
			if len(d.panes) > 0 && d.unmonitorFunc != nil {
				pane := d.panes[d.cursor]
				if pane.Status == AttentionVirtual {
					return d, nil
				}
				d.unmonitorFunc(pane.PaneID)
				d.dirty = true
				// Remove from source-of-truth list
				for i := range d.allPanes {
					if d.allPanes[i].PaneID == pane.PaneID {
						d.allPanes = append(d.allPanes[:i], d.allPanes[i+1:]...)
						break
					}
				}
				d.panes = append(d.panes[:d.cursor], d.panes[d.cursor+1:]...)
				if len(d.panes) == 0 {
					d.result = MonitorDashboardResult{Action: MonitorDashboardActionCancel}
					return d, tea.Quit
				}
				if d.cursor >= len(d.panes) {
					d.cursor = 0
				}
				d.list.SetItems(d.panes)
				d.list.SetCursor(d.cursor)
				d.syncFromList()
				d.fetchPreview()
			}
			return d, nil

		case d.isQuickAccessKey(msg):
			targetIdx := d.list.Cursor() - d.quickAccessDigit(msg)
			if targetIdx >= 0 && targetIdx < len(d.panes) {
				pane := d.panes[targetIdx]
				d.result = MonitorDashboardResult{
					Selected: &pane,
					Action:   MonitorDashboardActionConfirm,
				}
				return d, tea.Quit
			}
			return d, nil
		}

	case tea.WindowSizeMsg:
		d.width = msg.Width
		// frameSpec's BodyHeight covers the header row, the per-row list, and
		// (with this fix) warnings; back out those 3 lines to get the raw list
		// budget that listBodyHeight() re-adds.
		d.height = d.frameSpec().BodyHeight(msg.Height) - 3
		if d.height < 3 {
			d.height = 3
		}
		d.list.Resize(d.listBodyHeight())
		d.syncFromList()
	}

	return d, nil
}

func (d *MonitorDashboard) isQuickAccessKey(msg tea.KeyPressMsg) bool {
	return d.quickAccess.Digit(pickerKeyPress(msg)) >= 1
}

func (d *MonitorDashboard) quickAccessDigit(msg tea.KeyPressMsg) int {
	return d.quickAccess.Digit(pickerKeyPress(msg))
}

// reloadPanes refreshes the pane list from the reload function,
// preserving the cursor on the same pane when possible.
func (d *MonitorDashboard) reloadPanes() {
	if d.reloadFunc == nil {
		return
	}
	d.allPanes = d.reloadFunc()
	d.rebuildView()
}

// sortPanes performs a stable sort of panes by status group:
// clear (top) → working (middle) → unread (bottom, closest to cursor).
func (d *MonitorDashboard) sortPanes() {
	sort.SliceStable(d.allPanes, func(i, j int) bool {
		return attentionStatusOrder(d.allPanes[i].Status) < attentionStatusOrder(d.allPanes[j].Status)
	})
	sort.SliceStable(d.panes, func(i, j int) bool {
		return attentionStatusOrder(d.panes[i].Status) < attentionStatusOrder(d.panes[j].Status)
	})
	d.pinProtectedPane()
	d.syncPanesToList()
}

func attentionStatusOrder(s AttentionStatus) int {
	switch s {
	case AttentionClear, AttentionVirtual:
		return 0
	case AttentionWorking:
		return 1
	case AttentionUnread:
		return 2
	default:
		return 0
	}
}

// updateAllPanesStatus syncs a status change to the allPanes source-of-truth list.
func (d *MonitorDashboard) updateAllPanesStatus(paneID string, status AttentionStatus) {
	for i := range d.allPanes {
		if d.allPanes[i].PaneID == paneID {
			d.allPanes[i].Status = status
			break
		}
	}
}

// protectSelectedPane anchors a row mutated in place until the user navigates
// away. Reloads may continue to reorder the surrounding rows.
func (d *MonitorDashboard) protectSelectedPane() {
	if d.cursor < 0 || d.cursor >= len(d.panes) {
		return
	}
	d.protectedPaneID = d.panes[d.cursor].PaneID
	d.protectedCursorIndex = d.cursor
}

func (d *MonitorDashboard) clearProtectedPane() {
	d.protectedPaneID = ""
	d.protectedCursorIndex = 0
}

// pinProtectedPane moves the protected pane back to its anchored row after a
// sort or reload. It returns false when no protected pane remains visible.
func (d *MonitorDashboard) pinProtectedPane() bool {
	if d.protectedPaneID == "" {
		return false
	}

	protectedIndex := -1
	for i, pane := range d.panes {
		if pane.PaneID == d.protectedPaneID {
			protectedIndex = i
			break
		}
	}
	if protectedIndex < 0 {
		d.clearProtectedPane()
		return false
	}

	protected := d.panes[protectedIndex]
	d.panes = append(d.panes[:protectedIndex], d.panes[protectedIndex+1:]...)
	anchor := d.protectedCursorIndex
	if anchor > len(d.panes) {
		anchor = len(d.panes)
	}
	d.panes = append(d.panes, AttentionPane{})
	copy(d.panes[anchor+1:], d.panes[anchor:])
	d.panes[anchor] = protected
	d.cursor = anchor
	return true
}

// rebuildView filters allPanes into panes based on the current view mode.
func (d *MonitorDashboard) rebuildView() {
	var selectedPaneID string
	if pane, ok := d.list.Selected(); ok {
		selectedPaneID = pane.PaneID
	}

	if d.following {
		filtered := make([]AttentionPane, 0)
		for _, pane := range d.allPanes {
			if pane.Following {
				filtered = append(filtered, pane)
			}
		}
		d.panes = filtered
	} else {
		d.panes = make([]AttentionPane, len(d.allPanes))
		copy(d.panes, d.allPanes)
	}

	if d.pinProtectedPane() {
		d.list.SetItems(d.panes)
		d.list.SetCursor(d.cursor)
		d.syncFromList()
		d.fetchPreview()
		return
	}

	d.list.SetItems(d.panes)
	if selectedPaneID != "" {
		if !d.list.SetCursorToKey(selectedPaneID) {
			if len(d.panes) > 0 {
				d.list.SetCursor(len(d.panes) - 1)
			}
		}
	} else if d.cursor >= len(d.panes) {
		if len(d.panes) > 0 {
			d.list.SetCursor(len(d.panes) - 1)
		} else {
			d.list.SetCursor(0)
		}
	} else if d.cursor < 0 {
		d.list.SetCursor(0)
	}
	d.syncFromList()
	d.fetchPreview()
}

// fetchPreview calls the preview function for the currently selected pane.
func (d *MonitorDashboard) fetchPreview() {
	if d.previewFunc == nil || len(d.panes) == 0 {
		d.preview = ""
		return
	}
	d.preview = d.previewFunc(d.panes[d.cursor].PaneID)
}

// frameSpec builds the Frame describing the dashboard's screen chrome: the
// update notice, warnings, and hints. No Header — the two-column header row
// is part of the body composition itself, not a separate Frame region.
func (d *MonitorDashboard) frameSpec() Frame {
	return Frame{
		Width:    d.width,
		Notice:   d.updateNotice,
		Warnings: d.warnings,
		Hints:    d.buildHints(),
	}
}

// buildHints returns the hints string based on the current mode.
func (d *MonitorDashboard) buildHints() string {
	hints := "  Enter open and clear · Shift+Enter open · r toggle unread/clear · f follow · x unmonitor · F follow view · ← back · Esc cancel · C-h help"
	if d.pickerMode {
		hints = "  Enter select · F follow view · Esc cancel · C-h help"
		switch d.quickAccessModifier {
		case "alt":
			hints += " · A-1..9 quick select"
		case "ctrl":
			hints += " · C-1..9 quick select"
		}
	}
	return hints
}

func (d *MonitorDashboard) dashboardCell(pane AttentionPane, rs RowState) string {
	leftWidth := d.leftWidth()
	if rs.Width > 0 {
		leftWidth = rs.Width
	}

	prefixWidth := 2
	cellWidth := leftWidth - prefixWidth

	attentionIconStyle := lipgloss.NewStyle().Foreground(colorAttention)
	workingIconStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	clearIconStyle := lipgloss.NewStyle().Foreground(colorClear)

	var icon string
	switch pane.Status {
	case AttentionVirtual:
		icon = clearIconStyle.Render("○")
	case AttentionWorking:
		icon = workingIconStyle.Render(spinnerFrames[d.spinnerFrame])
	case AttentionUnread:
		icon = attentionIconStyle.Render("●")
	case AttentionClear:
		icon = clearIconStyle.Render("●")
	}
	iconWidth := 2
	if pane.Following {
		icon += "📌"
		iconWidth = 4
	}
	icon += " "

	nameWidth := cellWidth - iconWidth
	if nameWidth < 0 {
		nameWidth = 0
	}
	name := truncateString(pane.Name, nameWidth)
	displayName := name
	if pane.TopicDerived {
		displayName = dimStyle.Render(name)
	}

	contentWidth := iconWidth + len([]rune(name))
	padding := cellWidth - contentWidth
	if padding < 0 {
		padding = 0
	}
	return icon + displayName + strings.Repeat(" ", padding)
}

// View implements tea.Model
func (d *MonitorDashboard) View() tea.View {
	var content string
	if d.showHelp {
		content = d.viewHelp()
	} else {
		content = d.viewDashboard()
	}
	v := tea.NewView(content)
	v.AltScreen = true
	v.KeyboardEnhancements = tea.KeyboardEnhancements{}
	return v
}

func (d *MonitorDashboard) helpEntries() []HelpEntry {
	if d.pickerMode {
		entries := []HelpEntry{
			{"↑/↓ C-p/C-n", "Navigate"},
			{"Enter", "Select"},
			{"F", "Toggle follow view"},
			{"← / h", "Back / quit"},
			{"Esc / C-c", "Cancel"},
		}
		switch d.quickAccessModifier {
		case "alt":
			entries = append(entries, HelpEntry{"A-1..9", "Quick select"})
		case "ctrl":
			entries = append(entries, HelpEntry{"C-1..9", "Quick select"})
		}
		return entries
	}

	return []HelpEntry{
		{"↑/↓ C-p/C-n", "Navigate"},
		{"Enter", "Open and clear unread"},
		{"Shift+Enter / p", "Peek (open without clearing)"},
		{"r", "Toggle unread/clear"},
		{"C-a", "Mark unread"},
		{"f", "Follow pane"},
		{"F", "Toggle follow view"},
		{"x", "Unmonitor pane"},
		{"← / h", "Back / quit"},
		{"Esc / C-c", "Cancel"},
	}
}

func (d *MonitorDashboard) viewHelp() string {
	return RenderHelpOverlay("Help", d.helpEntries(), d.width, d.height)
}

func (d *MonitorDashboard) viewDashboard() string {
	var b strings.Builder

	sepStyle := lipgloss.NewStyle().Foreground(colorSeparator)

	leftWidth := d.leftWidth()
	rightWidth := d.width - leftWidth - 1
	if rightWidth < 10 {
		rightWidth = 10
	}

	// Empty panes
	if len(d.panes) == 0 {
		msgStyle := lipgloss.NewStyle().Foreground(colorDim)
		var eb strings.Builder
		if d.updateNotice != "" {
			eb.WriteString(renderUpdateNotice(d.width, d.updateNotice))
			eb.WriteString("\n")
		}
		headerText := d.title
		if d.following {
			headerText += " · following"
		} else {
			headerText += " · normal"
		}
		eb.WriteString(headerStyle.Render(" " + headerText))
		eb.WriteString("\n")
		for i := 0; i < d.height-1; i++ {
			eb.WriteString("\n")
		}
		if d.following {
			eb.WriteString(msgStyle.Render("  No followed panes"))
		} else {
			eb.WriteString(msgStyle.Render("  No active panes"))
		}
		if d.emptyNote != "" {
			eb.WriteString("\n")
			eb.WriteString(hintStyle.Render("  " + d.emptyNote))
		}
		eb.WriteString("\n")
		hint := "  F toggle view · Enter or Esc to dismiss"
		if d.reloadFunc != nil {
			hint += " · r to reload"
		}
		eb.WriteString(hintStyle.Render(hint))
		return eb.String()
	}

	// Header in left panel
	headerText := d.panes[d.cursor].Session
	if d.title != "" {
		headerText = d.title
	}
	if d.following {
		headerText += " · following"
	} else {
		headerText += " · normal"
	}
	headerText = truncateString(headerText, leftWidth-1)
	headerPadding := leftWidth - len([]rune(headerText)) - 1
	if headerPadding < 0 {
		headerPadding = 0
	}
	b.WriteString(headerStyle.Render(" " + headerText))
	b.WriteString(strings.Repeat(" ", headerPadding))
	b.WriteString(sepStyle.Render("│"))

	// Right header: pane name anchored to top-right, pin after name
	pane := d.panes[d.cursor]
	paneName := pane.Name
	pinSuffix := ""
	pinVisualWidth := 0
	if pane.Following {
		pinSuffix = " 📌"
		pinVisualWidth = 3
	}
	maxNameWidth := rightWidth - pinVisualWidth
	if maxNameWidth < 0 {
		maxNameWidth = 0
	}
	paneName = truncateString(paneName, maxNameWidth)
	rightHeaderVisualLen := len([]rune(paneName)) + pinVisualWidth
	rightPadding := rightWidth - rightHeaderVisualLen
	if rightPadding < 0 {
		rightPadding = 0
	}
	// A machine-derived Topic name is dimmed; otherwise it uses the header
	// style. The pin always keeps the header style.
	nameStyle := headerStyle
	if pane.TopicDerived {
		nameStyle = dimStyle
	}
	b.WriteString(strings.Repeat(" ", rightPadding))
	b.WriteString(nameStyle.Render(paneName))
	if pinSuffix != "" {
		b.WriteString(headerStyle.Render(pinSuffix))
	}
	b.WriteString("\n")

	// Build preview lines
	previewLines := strings.Split(d.preview, "\n")
	for len(previewLines) > 0 && strings.TrimSpace(previewLines[len(previewLines)-1]) == "" {
		previewLines = previewLines[:len(previewLines)-1]
	}

	listHeight := d.listBodyHeight()
	previewStart := 0
	if len(previewLines) > listHeight {
		previewStart = len(previewLines) - listHeight
	}

	for i, left := range d.list.VisibleRows() {
		previewIdx := previewStart + i
		rightContent := ""
		if previewIdx < len(previewLines) {
			rightContent = truncateString(previewLines[previewIdx], rightWidth)
		}
		if left == "" {
			b.WriteString(strings.Repeat(" ", leftWidth))
		} else {
			b.WriteString("\x1b[0m")
			b.WriteString(left)
		}
		b.WriteString(sepStyle.Render("│"))
		b.WriteString(rightContent)
		b.WriteString("\x1b[0m\n")
	}

	return d.frameSpec().Render(strings.TrimSuffix(b.String(), "\n"))
}

// Result returns the dashboard result after running
func (d *MonitorDashboard) Result() MonitorDashboardResult {
	d.result.CursorIndex = d.cursor
	d.result.Following = d.following
	return d.result
}

// RunMonitorDashboard starts the dashboard and returns the result
func RunMonitorDashboard(title string, panes []AttentionPane, cb AttentionCallbacks, reloadFn func() []AttentionPane, opts ...MonitorDashboardOption) (MonitorDashboardResult, error) {
	d := NewMonitorDashboard(panes, cb, reloadFn, opts...)
	d.title = title
	if d.following {
		d.rebuildView()
	}
	if len(d.panes) > 0 {
		d.list.SetCursor(len(d.panes) - 1)
		if d.initialPaneID != "" {
			d.list.SetCursorToKey(d.initialPaneID)
		}
	}
	d.syncFromList()
	d.fetchPreview()
	program := tea.NewProgram(d)
	m, err := program.Run()
	if err != nil {
		return MonitorDashboardResult{Action: MonitorDashboardActionCancel}, err
	}
	return m.(*MonitorDashboard).Result(), nil
}

// dashboardKeys holds key bindings for the dashboard
type dashboardKeyMap struct {
	Up                key.Binding
	Down              key.Binding
	Enter             key.Binding
	Quit              key.Binding
	PeekPane          key.Binding
	FollowPane        key.Binding
	ToggleFollowView  key.Binding
	Unmonitor         key.Binding
	ToggleClearUnread key.Binding
	Back              key.Binding
	MarkUnread        key.Binding
}

var dashboardKeys = dashboardKeyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k", "ctrl+p"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j", "ctrl+n"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
	),
	Quit: key.NewBinding(
		key.WithKeys("esc", "ctrl+c"),
	),
	PeekPane: key.NewBinding(
		key.WithKeys("shift+enter", "p"),
	),
	FollowPane: key.NewBinding(
		key.WithKeys("f"),
	),
	ToggleFollowView: key.NewBinding(
		key.WithKeys("F"),
	),
	Unmonitor: key.NewBinding(
		key.WithKeys("x"),
	),
	ToggleClearUnread: key.NewBinding(
		key.WithKeys("r"),
	),
	Back: key.NewBinding(
		key.WithKeys("left", "h"),
	),
	MarkUnread: key.NewBinding(
		key.WithKeys("ctrl+a"),
	),
}
