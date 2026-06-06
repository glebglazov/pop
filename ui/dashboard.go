package ui

import (
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
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
	Note      string
}

// AttentionCallbacks holds callback functions for the attention sub-view.
type AttentionCallbacks struct {
	Preview      func(paneID string) string // returns pane content for preview
	MarkClear    func(paneID string)        // marks a pane as clear
	MarkUnread   func(paneID string)        // marks a pane as unread
	ToggleFollow func(paneID string)        // toggles following flag
	Unmonitor    func(paneID string)        // removes a pane from monitor state
	SetNote      func(paneID, note string)  // sets note on a pane
}

// DashboardAction represents what action the user wants to take in the dashboard
type DashboardAction int

const (
	DashboardActionCancel DashboardAction = iota
	DashboardActionConfirm
	DashboardActionPeek
	DashboardActionRefresh
)

// DashboardResult holds the dashboard result
type DashboardResult struct {
	Selected    *AttentionPane
	Action      DashboardAction
	Following   bool
	CursorIndex int
}

// Dashboard is a tea.Model for browsing monitored panes
type Dashboard struct {
	panes    []AttentionPane
	allPanes []AttentionPane // full list (source of truth)
	cursor   int
	scroll   int
	width    int
	height   int
	result   DashboardResult

	following    bool
	dirty        bool
	preview      string
	title        string
	emptyNote    string
	spinnerFrame int

	editingNote bool
	noteInput   textinput.Model

	showHelp bool

	previewFunc      func(paneID string) string
	reloadFunc       func() []AttentionPane
	markClearFunc    func(paneID string)
	markUnreadFunc   func(paneID string)
	toggleFollowFunc func(paneID string)
	unmonitorFunc    func(paneID string)
	setNoteFunc      func(paneID, note string)

	warnings []string

	// updateNotice is the dimmed top-right Update notice text (empty = none).
	updateNotice string

	initialPaneID        string
	protectedPaneID      string
	protectedCursorIndex int
}

// DashboardOption configures the dashboard
type DashboardOption func(*Dashboard)

// WithFollowing sets the initial following mode for the dashboard.
func WithFollowing(following bool) DashboardOption {
	return func(d *Dashboard) {
		d.following = following
	}
}

// WithInitialPaneID selects the initial dashboard cursor by pane ID.
func WithInitialPaneID(paneID string) DashboardOption {
	return func(d *Dashboard) {
		d.initialPaneID = paneID
	}
}

// WithDashboardWarnings adds warning messages to display in the dashboard.
func WithDashboardWarnings(warnings []string) DashboardOption {
	return func(d *Dashboard) {
		d.warnings = warnings
	}
}

// WithDashboardUpdateNotice sets the dimmed top-right Update notice text. Empty
// text shows nothing. The notice occupies a reserved top line so it never
// shifts the pane list or preview.
func WithDashboardUpdateNotice(text string) DashboardOption {
	return func(d *Dashboard) {
		d.updateNotice = text
	}
}

// WithEmptyNote sets a note line shown below the "No panes need attention" message.
func WithEmptyNote(note string) DashboardOption {
	return func(d *Dashboard) {
		d.emptyNote = note
	}
}

// NewDashboard creates a new dashboard with the given panes and callbacks
func NewDashboard(panes []AttentionPane, cb AttentionCallbacks, reloadFn func() []AttentionPane, opts ...DashboardOption) *Dashboard {
	d := &Dashboard{
		allPanes:         panes,
		panes:            make([]AttentionPane, len(panes)),
		height:           10,
		previewFunc:      cb.Preview,
		reloadFunc:       reloadFn,
		markClearFunc:    cb.MarkClear,
		markUnreadFunc:   cb.MarkUnread,
		toggleFollowFunc: cb.ToggleFollow,
		unmonitorFunc:    cb.Unmonitor,
		setNoteFunc:      cb.SetNote,
	}
	copy(d.panes, panes)
	for _, opt := range opts {
		opt(d)
	}
	return d
}

func (d *Dashboard) hasWorkingPanes() bool {
	for _, pane := range d.panes {
		if pane.Status == AttentionWorking {
			return true
		}
	}
	return false
}

// Init implements tea.Model
func (d *Dashboard) Init() tea.Cmd {
	if len(d.panes) > 0 {
		d.cursor = len(d.panes) - 1
		if d.initialPaneID != "" {
			for i, pane := range d.panes {
				if pane.PaneID == d.initialPaneID {
					d.cursor = i
					break
				}
			}
		}
	}
	d.adjustScroll()
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
func (d *Dashboard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		// Help overlay: esc dismisses, all other keys are swallowed
		if d.showHelp {
			if key.Matches(msg, dashboardKeys.Quit) {
				d.showHelp = false
			}
			return d, nil
		}

		// Toggle help overlay
		if key.Matches(msg, dashboardKeys.Help) {
			d.showHelp = true
			return d, nil
		}

		// Note editing mode: capture all keys
		if d.editingNote {
			switch {
			case key.Matches(msg, dashboardKeys.Quit): // Esc/Ctrl+C
				d.editingNote = false
				return d, nil
			case key.Matches(msg, dashboardKeys.Enter):
				note := strings.TrimSpace(d.noteInput.Value())
				pane := &d.panes[d.cursor]
				// Auto-follow on save if not already
				if !pane.Following && d.toggleFollowFunc != nil {
					d.toggleFollowFunc(pane.PaneID)
					pane.Following = true
					for i := range d.allPanes {
						if d.allPanes[i].PaneID == pane.PaneID {
							d.allPanes[i].Following = true
							break
						}
					}
				}
				d.setNoteFunc(pane.PaneID, note)
				pane.Note = note
				for i := range d.allPanes {
					if d.allPanes[i].PaneID == pane.PaneID {
						d.allPanes[i].Note = note
						break
					}
				}
				d.dirty = true
				d.editingNote = false
				return d, nil
			default:
				var cmd tea.Cmd
				d.noteInput, cmd = d.noteInput.Update(msg)
				return d, cmd
			}
		}

		switch {
		case key.Matches(msg, dashboardKeys.Back):
			if d.dirty {
				d.result = DashboardResult{Action: DashboardActionRefresh}
				return d, tea.Quit
			}
			return d, tea.Quit

		case key.Matches(msg, dashboardKeys.Quit):
			if msg.Code == 0x1b { // esc
				if d.dirty {
					d.result = DashboardResult{Action: DashboardActionRefresh}
					return d, tea.Quit
				}
				return d, tea.Quit
			}
			// ctrl+c — quit
			d.result = DashboardResult{Action: DashboardActionCancel}
			return d, tea.Quit

		case key.Matches(msg, dashboardKeys.Enter):
			if len(d.panes) == 0 {
				d.result = DashboardResult{Action: DashboardActionCancel}
				return d, tea.Quit
			}
			pane := d.panes[d.cursor]
			d.result = DashboardResult{
				Selected: &pane,
				Action:   DashboardActionConfirm,
			}
			return d, tea.Quit

		case key.Matches(msg, dashboardKeys.PeekPane):
			// "Peek" — open the pane without mutating its monitor state.
			if len(d.panes) == 0 {
				return d, nil
			}
			pane := d.panes[d.cursor]
			d.result = DashboardResult{
				Selected: &pane,
				Action:   DashboardActionPeek,
			}
			return d, tea.Quit

		case key.Matches(msg, dashboardKeys.Up):
			if len(d.panes) > 0 {
				d.clearProtectedPane()
				if d.cursor > 0 {
					d.cursor--
				} else {
					d.cursor = len(d.panes) - 1
				}
				d.adjustScroll()
				d.fetchPreview()
			}
			return d, nil

		case key.Matches(msg, dashboardKeys.Down):
			if len(d.panes) > 0 {
				d.clearProtectedPane()
				if d.cursor < len(d.panes)-1 {
					d.cursor++
				} else {
					d.cursor = 0
				}
				d.adjustScroll()
				d.fetchPreview()
			}
			return d, nil

		case key.Matches(msg, dashboardKeys.ToggleClearUnread):
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
				}
				d.dirty = true
				d.adjustScroll()
				d.fetchPreview()
			}
			return d, nil

		case key.Matches(msg, dashboardKeys.MarkUnread):
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
				}
				d.dirty = true
				d.adjustScroll()
				d.fetchPreview()
			}
			return d, nil

		case key.Matches(msg, dashboardKeys.FollowPane):
			if len(d.panes) > 0 && d.toggleFollowFunc != nil {
				pane := &d.panes[d.cursor]
				if pane.Status == AttentionVirtual {
					return d, nil
				}
				d.toggleFollowFunc(pane.PaneID)
				pane.Following = !pane.Following
				// Clear note when unfollowing
				if !pane.Following && pane.Note != "" && d.setNoteFunc != nil {
					d.setNoteFunc(pane.PaneID, "")
					pane.Note = ""
				}
				// Update source-of-truth list
				for i := range d.allPanes {
					if d.allPanes[i].PaneID == pane.PaneID {
						d.allPanes[i].Following = pane.Following
						d.allPanes[i].Note = pane.Note
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

		case key.Matches(msg, dashboardKeys.EditNote):
			if len(d.panes) > 0 && d.setNoteFunc != nil {
				pane := d.panes[d.cursor]
				if pane.Status == AttentionVirtual {
					return d, nil
				}
				d.editingNote = true
				d.noteInput = textinput.New()
				d.noteInput.Prompt = "note: "
				d.noteInput.SetValue(pane.Note)
				d.noteInput.Focus()
			}
			return d, nil

		case key.Matches(msg, dashboardKeys.Unmonitor):
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
					d.result = DashboardResult{Action: DashboardActionCancel}
					return d, tea.Quit
				}
				if d.cursor >= len(d.panes) {
					d.cursor = 0
				}
				d.adjustScroll()
				d.fetchPreview()
			}
			return d, nil
		}

	case tea.WindowSizeMsg:
		d.width = msg.Width
		d.height = msg.Height - 4 // Reserve space for hints (1 line) + input box (3 lines)
		if d.updateNotice != "" {
			d.height-- // reserve the top line for the dimmed Update notice
		}
		if d.height < 3 {
			d.height = 3
		}
		d.adjustScroll()
	}

	return d, nil
}

// reloadPanes refreshes the pane list from the reload function,
// preserving the cursor on the same pane when possible.
func (d *Dashboard) reloadPanes() {
	if d.reloadFunc == nil {
		return
	}
	var selectedPaneID string
	if d.cursor < len(d.panes) {
		selectedPaneID = d.panes[d.cursor].PaneID
	}

	d.allPanes = d.reloadFunc()
	d.rebuildView()

	restored := false
	if selectedPaneID != "" {
		for i, pane := range d.panes {
			if pane.PaneID == selectedPaneID {
				d.cursor = i
				restored = true
				break
			}
		}
	}
	if !restored {
		if len(d.panes) > 0 {
			d.cursor = len(d.panes) - 1
		} else {
			d.cursor = 0
		}
	}
	d.adjustScroll()
	d.fetchPreview()
}

// sortPanes performs a stable sort of panes by status group:
// clear (top) → working (middle) → unread (bottom, closest to cursor).
func (d *Dashboard) sortPanes() {
	sort.SliceStable(d.allPanes, func(i, j int) bool {
		return attentionStatusOrder(d.allPanes[i].Status) < attentionStatusOrder(d.allPanes[j].Status)
	})
	sort.SliceStable(d.panes, func(i, j int) bool {
		return attentionStatusOrder(d.panes[i].Status) < attentionStatusOrder(d.panes[j].Status)
	})
	d.pinProtectedPane()
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
func (d *Dashboard) updateAllPanesStatus(paneID string, status AttentionStatus) {
	for i := range d.allPanes {
		if d.allPanes[i].PaneID == paneID {
			d.allPanes[i].Status = status
			break
		}
	}
}

// protectSelectedPane anchors a row mutated in place until the user navigates
// away. Reloads may continue to reorder the surrounding rows.
func (d *Dashboard) protectSelectedPane() {
	if d.cursor < 0 || d.cursor >= len(d.panes) {
		return
	}
	d.protectedPaneID = d.panes[d.cursor].PaneID
	d.protectedCursorIndex = d.cursor
}

func (d *Dashboard) clearProtectedPane() {
	d.protectedPaneID = ""
	d.protectedCursorIndex = 0
}

// pinProtectedPane moves the protected pane back to its anchored row after a
// sort or reload. It returns false when no protected pane remains visible.
func (d *Dashboard) pinProtectedPane() bool {
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
func (d *Dashboard) rebuildView() {
	var selectedPaneID string
	if d.cursor >= 0 && d.cursor < len(d.panes) {
		selectedPaneID = d.panes[d.cursor].PaneID
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
		d.adjustScroll()
		d.fetchPreview()
		return
	}

	restored := false
	if selectedPaneID != "" {
		for i, pane := range d.panes {
			if pane.PaneID == selectedPaneID {
				d.cursor = i
				restored = true
				break
			}
		}
	}
	if !restored {
		if d.cursor >= len(d.panes) {
			d.cursor = len(d.panes) - 1
		}
		if d.cursor < 0 {
			d.cursor = 0
		}
	}
	d.adjustScroll()
	d.fetchPreview()
}

// fetchPreview calls the preview function for the currently selected pane.
func (d *Dashboard) fetchPreview() {
	if d.previewFunc == nil || len(d.panes) == 0 {
		d.preview = ""
		return
	}
	d.preview = d.previewFunc(d.panes[d.cursor].PaneID)
}

// adjustAttentionScroll ensures the attention cursor is visible.
func (d *Dashboard) adjustScroll() {
	listHeight := d.height + 2
	if listHeight <= 0 {
		listHeight = 1
	}
	maxScroll := len(d.panes) - listHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	if d.scroll > maxScroll {
		d.scroll = maxScroll
	}
	if d.cursor < d.scroll {
		d.scroll = d.cursor
	}
	if d.cursor >= d.scroll+listHeight {
		d.scroll = d.cursor - listHeight + 1
	}
}

// View implements tea.Model
func (d *Dashboard) View() tea.View {
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

func (d *Dashboard) viewHelp() string {
	var b strings.Builder

	type helpEntry struct {
		key  string
		desc string
	}

	entries := []helpEntry{
		{"↑/↓ C-p/C-n", "Navigate"},
		{"Enter", "Open and clear unread"},
		{"Shift+Enter / p", "Peek (open without clearing)"},
		{"r", "Toggle unread/clear"},
		{"f", "Follow pane"},
		{"F", "Toggle follow view"},
		{"N", "Edit note"},
		{"x", "Unmonitor pane"},
		{"← / Esc", "Back / quit"},
		{"F1", "Help"},
	}

	maxKeyWidth := 0
	for _, e := range entries {
		if w := lipgloss.Width(e.key); w > maxKeyWidth {
			maxKeyWidth = w
		}
	}

	var helpLines []string
	for _, e := range entries {
		padding := maxKeyWidth - lipgloss.Width(e.key)
		helpLines = append(helpLines, "  "+e.key+strings.Repeat(" ", padding)+"   "+e.desc)
	}

	emptyLines := d.height - len(helpLines)
	for i := 0; i < emptyLines; i++ {
		b.WriteString("\n")
	}

	for _, line := range helpLines {
		b.WriteString(line)
		b.WriteString("\n")
	}

	writeInputBox(&b, d.width, " Help")
	b.WriteString(hintStyle.Render("  Esc back"))

	return b.String()
}

func (d *Dashboard) viewDashboard() string {
	var b strings.Builder

	// Dimmed Update notice on a reserved top line, anchored top-right. The line
	// is accounted for in d.height (see WindowSizeMsg), so it never shifts the
	// pane list, preview, or hints.
	if d.updateNotice != "" {
		b.WriteString(renderUpdateNotice(d.width, d.updateNotice))
		b.WriteString("\n")
	}

	sepStyle := lipgloss.NewStyle().Foreground(colorSeparator)

	leftWidth := d.width * 3 / 10
	if leftWidth < 15 {
		leftWidth = 15
	}
	rightWidth := d.width - leftWidth - 1
	if rightWidth < 10 {
		rightWidth = 10
	}

	listHeight := d.height + 2

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
	rightHeader := paneName + pinSuffix
	rightHeaderVisualLen := len([]rune(paneName)) + pinVisualWidth
	rightPadding := rightWidth - rightHeaderVisualLen
	if rightPadding < 0 {
		rightPadding = 0
	}
	b.WriteString(strings.Repeat(" ", rightPadding))
	b.WriteString(headerStyle.Render(rightHeader))
	b.WriteString("\n")

	// Build preview lines
	previewLines := strings.Split(d.preview, "\n")
	for len(previewLines) > 0 && strings.TrimSpace(previewLines[len(previewLines)-1]) == "" {
		previewLines = previewLines[:len(previewLines)-1]
	}

	start := d.scroll
	if start > len(d.panes) {
		start = len(d.panes)
	}
	visible := listHeight
	if visible > len(d.panes)-start {
		visible = len(d.panes) - start
	}

	previewStart := 0
	if len(previewLines) > listHeight {
		previewStart = len(previewLines) - listHeight
	}

	emptyLines := listHeight - visible
	for i := 0; i < emptyLines; i++ {
		previewIdx := previewStart + i
		rightContent := ""
		if previewIdx < len(previewLines) {
			rightContent = truncateString(previewLines[previewIdx], rightWidth)
		}
		b.WriteString(strings.Repeat(" ", leftWidth))
		b.WriteString(sepStyle.Render("│"))
		b.WriteString(rightContent)
		b.WriteString("\x1b[0m\n")
	}

	attentionIconStyle := lipgloss.NewStyle().Foreground(colorAttention)
	workingIconStyle := lipgloss.NewStyle().Foreground(colorWorking)
	clearIconStyle := lipgloss.NewStyle().Foreground(colorClear)

	for i := 0; i < visible; i++ {
		listIdx := start + i
		previewIdx := previewStart + emptyLines + i

		var left string
		pane := d.panes[listIdx]

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

		nameWidth := leftWidth - iconWidth - 3
		name := truncateString(pane.Name, nameWidth)

		if listIdx == d.cursor {
			padding := leftWidth - len([]rune(name)) - iconWidth - 3
			if padding < 0 {
				padding = 0
			}
			left = indicatorStyle.Render("█") + " " + icon + name + strings.Repeat(" ", padding)
		} else {
			padding := leftWidth - len([]rune(name)) - iconWidth - 2
			if padding < 0 {
				padding = 0
			}
			left = "  " + icon + name + strings.Repeat(" ", padding)
		}

		rightContent := ""
		if previewIdx < len(previewLines) {
			rightContent = truncateString(previewLines[previewIdx], rightWidth)
		}

		b.WriteString("\x1b[0m")
		b.WriteString(left)
		b.WriteString(sepStyle.Render("│"))
		b.WriteString(rightContent)
		b.WriteString("\x1b[0m\n")
	}

	// Warnings
	if len(d.warnings) > 0 {
		warnStyle := lipgloss.NewStyle().Foreground(colorWorking)
		for _, w := range d.warnings {
			b.WriteString(warnStyle.Render("  ⚠ " + w))
			b.WriteString("\n")
		}
	}

	// Hints or note input
	if d.editingNote {
		b.WriteString("  " + d.noteInput.View())
	} else {
		hints := "  Enter open and clear · Shift+Enter open · r toggle unread/clear · f follow · x unmonitor · F follow view · ← back · Esc cancel"
		b.WriteString(hintStyle.Render(hints))
	}

	return b.String()
}

// Result returns the dashboard result after running
func (d *Dashboard) Result() DashboardResult {
	d.result.CursorIndex = d.cursor
	d.result.Following = d.following
	return d.result
}

// RunDashboard starts the dashboard and returns the result
func RunDashboard(title string, panes []AttentionPane, cb AttentionCallbacks, reloadFn func() []AttentionPane, opts ...DashboardOption) (DashboardResult, error) {
	d := NewDashboard(panes, cb, reloadFn, opts...)
	d.title = title
	if d.following {
		d.rebuildView()
	}
	if len(d.panes) > 0 {
		d.cursor = len(d.panes) - 1
		if d.initialPaneID != "" {
			for i, pane := range d.panes {
				if pane.PaneID == d.initialPaneID {
					d.cursor = i
					break
				}
			}
		}
	}
	d.adjustScroll()
	d.fetchPreview()
	program := tea.NewProgram(d)
	m, err := program.Run()
	if err != nil {
		return DashboardResult{Action: DashboardActionCancel}, err
	}
	return m.(*Dashboard).Result(), nil
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
	EditNote          key.Binding
	Help              key.Binding
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
	EditNote: key.NewBinding(
		key.WithKeys("N"),
	),
	Help: key.NewBinding(
		key.WithKeys("f1"),
	),
}
