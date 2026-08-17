package ui

import (
	"fmt"
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

// SpinnerFrames exposes the Monitor working-spinner frames so other dashboards
// (e.g. the Work dashboard live-drain dot) reuse the exact same shape.
var SpinnerFrames = spinnerFrames

// SpinnerTickMsg is the exported alias of the working-spinner tick, letting other
// packages match on it in their own Update loops.
type SpinnerTickMsg = spinnerTickMsg

// SpinnerTick returns the 100ms working-spinner tick command; exported so other
// dashboards drive the animation at the same cadence as the Monitor.
func SpinnerTick() tea.Cmd { return spinnerTick() }

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
	// KillPane destroys a pane's tmux pane and drops it from the monitored set
	// in one action (ADR-0205). The error is what the dashboard reports when the
	// kill fails, and is the signal that the row must stay.
	KillPane func(paneID string) error
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

	// pendingG remembers a half-typed `gg`: the first `g` arms it, the second
	// jumps to the top, and any other key disarms it.
	pendingG bool

	// selection is the human's mark on rows, keyed by pane id (ADR-0215). The
	// dashboard's whole notion of selection mode is derived from it — the mode
	// word, the reserved region and every verb's refusal read this one set, so
	// there is no second flag that could disagree with the marks.
	selection Selection

	previewFunc      func(paneID string) string
	reloadFunc       func() []AttentionPane
	markClearFunc    func(paneID string)
	markUnreadFunc   func(paneID string)
	toggleFollowFunc func(paneID string)
	unmonitorFunc    func(paneID string)
	killPaneFunc     func(paneID string) error

	// flash reports what a verb just did on the bottom line. The kill is the
	// only verb with an outcome — above all a failure — that has nowhere else to
	// appear (ADR-0204, ADR-0205).
	flash Flash
	// currentPaneID is the pane the dashboard itself runs in. The kill key
	// refuses it, so the guard never depends on the cursor-position setting.
	currentPaneID string
	// killPromptEnabled gates the y/N confirmation before a kill; true unless a
	// caller passes WithMonitorDashboardKillPrompt(false).
	killPromptEnabled bool
	killPrompt        *killPanePrompt
	// writePrompt is the y/N confirmation for a non-destructive bulk write —
	// unmonitor or follow — which is never gated by killPromptEnabled: neither
	// verb touches a process, so ADR-0205's standing exception for kills does
	// not apply (ADR-0215 decision 7).
	writePrompt *writePrompt

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

// killPanePrompt is the y/N confirmation the kill key opens by default. It
// remembers the panes it named, and while it is open every key outside its own
// grammar is inert — so `y` can only ever destroy what the prompt asked about,
// never a row the cursor moved to nor a Selection a poll changed in between.
type killPanePrompt struct {
	paneIDs []string
	label   string
	// plural marks a prompt opened over a Selection. A Selection of one is still
	// plural: its answer consumes the marks, which the single-pane path knows
	// nothing about.
	plural bool
}

// writePrompt is the y/N confirmation a bulk unmonitor or follow opens. It has
// no singular counterpart and no config gate — those verbs never prompt for
// one pane at all — so unlike killPanePrompt it always fires over a Selection.
// apply is the verb to run on `y`, already closed over any direction the
// verb's label depends on.
type writePrompt struct {
	paneIDs []string
	label   string
	apply   func(d *MonitorDashboard, paneIDs []string) tea.Cmd
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

// WithMonitorDashboardCurrentPaneID names the pane the dashboard is running in,
// which the kill key refuses to destroy.
func WithMonitorDashboardCurrentPaneID(paneID string) MonitorDashboardOption {
	return func(d *MonitorDashboard) {
		d.currentPaneID = paneID
	}
}

// WithMonitorDashboardKillPrompt turns the kill key's y/N confirmation off.
// Without it the prompt is on, matching the config default.
func WithMonitorDashboardKillPrompt(enabled bool) MonitorDashboardOption {
	return func(d *MonitorDashboard) {
		d.killPromptEnabled = enabled
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
		killPaneFunc:     cb.KillPane,
		// Options may turn the prompt off; nothing turns it on, so the safe
		// answer is the one a caller that says nothing gets.
		killPromptEnabled: true,
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

// Update implements tea.Model. It wraps the dashboard's own update loop with
// the single place a flash's expiry reaches bubbletea: a verb deep in the key
// switch only has to set the message, and the timer that takes it away again is
// armed here (ADR-0204).
func (d *MonitorDashboard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if expired, ok := msg.(FlashExpiredMsg); ok {
		d.flash.Expired(expired)
		return d, nil
	}
	model, cmd := d.update(msg)
	if timer := d.flash.Timer(); timer != nil {
		return model, tea.Batch(cmd, timer)
	}
	return model, cmd
}

func (d *MonitorDashboard) update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		// The kill prompt swallows the whole keyboard, help included: the pane
		// it names must not move under the answer.
		if d.killPrompt != nil {
			d.pendingG = false
			return d.updateKillPrompt(msg)
		}
		if d.writePrompt != nil {
			d.pendingG = false
			return d.updateWritePrompt(msg)
		}

		// Help overlay: toggle, dismiss, or swallow keys while open.
		if ToggleHelp(&d.showHelp, msg) {
			d.pendingG = false
			return d, nil
		}

		// gg/G are the same list vocabulary the Work dashboard speaks. The chord
		// is resolved ahead of the flat bindings so a first `g` can arm it, and
		// every other key below disarms it.
		if key.Matches(msg, dashboardKeys.Top) {
			if d.pendingG {
				d.pendingG = false
				d.jumpCursorTo(d.regionTop())
			} else {
				d.pendingG = true
			}
			return d, nil
		}
		d.pendingG = false

		switch {
		case key.Matches(msg, dashboardKeys.Bottom):
			d.jumpCursorTo(d.regionBottom())
			return d, nil

		case key.Matches(msg, dashboardKeys.ToggleSelect):
			// Picker mode promises the caller it mutates nothing, and a Selection
			// is the human marking the rows a verb is about to change.
			if d.pickerMode || len(d.panes) == 0 {
				return d, nil
			}
			d.toggleSelected()
			return d, nil

		case key.Matches(msg, dashboardKeys.ClearSelection):
			if d.pickerMode || !d.selection.Active() {
				return d, nil
			}
			d.selection.Clear()
			d.rebuildView()
			return d, nil

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
			if d.refuseSingular(&dashboardKeys.Enter) {
				return d, nil
			}
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
			if d.refuseSingular(&dashboardKeys.PeekPane) {
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
			if d.refuseSingular(&dashboardKeys.ToggleClearUnread) {
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
			if d.refuseSingular(&dashboardKeys.MarkUnread) {
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
			if d.refuseSingular(&dashboardKeys.FollowPane) {
				return d, nil
			}
			if d.selection.Active() {
				return d, d.startFollowSelected()
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
			if d.refuseSingular(&dashboardKeys.Unmonitor) {
				return d, nil
			}
			if d.selection.Active() {
				return d, d.startUnmonitorSelected()
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

		case key.Matches(msg, dashboardKeys.KillPane):
			// Picker mode promises callers it mutates nothing, and destroying a
			// pane is the largest mutation the view has.
			if d.pickerMode {
				return d, nil
			}
			// The capability table grants the kill both modes, so this asks the
			// marks which one applies rather than carrying a flag of its own.
			if d.refuseSingular(&dashboardKeys.KillPane) {
				return d, nil
			}
			if d.selection.Active() {
				return d, d.startKillSelected()
			}
			return d, d.startKillPane()

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
		d.list.SetWidth(d.leftWidth())
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

// startKillPane answers the kill key: it refuses the dashboard's own pane, opens
// the confirmation when the prompt is enabled, and otherwise kills at once.
func (d *MonitorDashboard) startKillPane() tea.Cmd {
	if len(d.panes) == 0 || d.killPaneFunc == nil {
		return nil
	}
	pane := d.panes[d.cursor]
	if d.currentPaneID != "" && pane.PaneID == d.currentPaneID {
		d.flash.Set("cannot kill the pane the dashboard is running in")
		return nil
	}
	if d.killPromptEnabled {
		// A prompt that a stale message covered would be answered blind.
		d.flash.Set("")
		d.killPrompt = &killPanePrompt{paneIDs: []string{pane.PaneID}, label: killPaneLabel(pane)}
		return nil
	}
	return d.killPane(pane.PaneID)
}

// startKillSelected answers the kill key in selection mode. It captures the marked
// panes now rather than reading the Selection again when the answer arrives, so
// the set the human agreed to is the set the kill runs over even though the poll
// keeps rebuilding underneath the prompt. The prompt setting governs here exactly
// as it does for one pane: someone who turned it off made a standing decision
// about this risk, and asking anyway would overrule it (ADR-0215 decision 7).
func (d *MonitorDashboard) startKillSelected() tea.Cmd {
	if d.killPaneFunc == nil {
		return nil
	}
	paneIDs := d.selectedPaneIDs()
	if len(paneIDs) == 0 {
		return nil
	}
	if d.killPromptEnabled {
		d.flash.Set("")
		d.killPrompt = &killPanePrompt{paneIDs: paneIDs, label: paneCount(len(paneIDs)), plural: true}
		return nil
	}
	return d.killPanes(paneIDs)
}

// startUnmonitorSelected answers the unmonitor key in selection mode. It opens
// the standard bulk y/N — never the kill prompt's config gate, since forgetting
// a pane touches no process (ADR-0215 decision 7) — over the marked set
// captured now, the same staleness rule startKillSelected uses.
func (d *MonitorDashboard) startUnmonitorSelected() tea.Cmd {
	if d.unmonitorFunc == nil {
		return nil
	}
	paneIDs := d.selectedPaneIDs()
	if len(paneIDs) == 0 {
		return nil
	}
	d.flash.Set("")
	d.writePrompt = &writePrompt{
		paneIDs: paneIDs,
		label:   fmt.Sprintf("unmonitor %d", len(paneIDs)),
		apply:   (*MonitorDashboard).unmonitorPanes,
	}
	return nil
}

// startFollowSelected answers the follow key in selection mode. A toggle over a
// mixed set is ambiguous, so the direction is decided once, here, rather than
// per pane: if any marked pane is not followed, the run follows all of them;
// otherwise it unfollows all of them (ADR-0215 decision 5). The label names
// that decision rather than a static word, since "follow" would otherwise read
// as a promise the run might actually break.
func (d *MonitorDashboard) startFollowSelected() tea.Cmd {
	if d.toggleFollowFunc == nil {
		return nil
	}
	paneIDs := d.selectedPaneIDs()
	if len(paneIDs) == 0 {
		return nil
	}
	follow := false
	for _, pane := range d.panes {
		if d.selection.Has(pane.PaneID) && !pane.Following {
			follow = true
			break
		}
	}
	verb := "unfollow"
	if follow {
		verb = "follow"
	}
	d.flash.Set("")
	d.writePrompt = &writePrompt{
		paneIDs: paneIDs,
		label:   fmt.Sprintf("%s %d", verb, len(paneIDs)),
		apply: func(dd *MonitorDashboard, ids []string) tea.Cmd {
			return dd.followPanes(ids, follow)
		},
	}
	return nil
}

// selectedPaneIDs lists the marked panes in the order the view holds them, which
// is the order the region draws and therefore the order failures are reported in.
func (d *MonitorDashboard) selectedPaneIDs() []string {
	var ids []string
	for _, pane := range d.panes {
		if d.selection.Has(pane.PaneID) {
			ids = append(ids, pane.PaneID)
		}
	}
	return ids
}

// updateKillPrompt runs the confirmation's grammar — y kills, enter/n/esc/C-c
// cancel, everything else is ignored — and returns the dashboard to normal. A
// cancel leaves the Selection whole: nothing was killed, so nothing is consumed.
func (d *MonitorDashboard) updateKillPrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, dashboardKeys.KillConfirm):
		prompt := d.killPrompt
		d.killPrompt = nil
		if prompt.plural {
			return d, d.killPanes(prompt.paneIDs)
		}
		return d, d.killPane(prompt.paneIDs[0])
	case key.Matches(msg, dashboardKeys.KillCancel):
		d.killPrompt = nil
	}
	return d, nil
}

// updateWritePrompt runs the same y/N grammar the kill prompt uses — `y` runs
// the verb it named, everything else in KillCancel's set backs out — over a
// bulk unmonitor or follow instead. A cancel leaves the Selection whole:
// nothing changed, so nothing is consumed.
func (d *MonitorDashboard) updateWritePrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, dashboardKeys.KillConfirm):
		prompt := d.writePrompt
		d.writePrompt = nil
		return d, prompt.apply(d, prompt.paneIDs)
	case key.Matches(msg, dashboardKeys.KillCancel):
		d.writePrompt = nil
	}
	return d, nil
}

// killPanes kills a whole captured set in one pass. A failure does not abort the
// run — every pane gets its turn — and the Selection afterwards is exactly the
// panes that failed, so they stay marked at the top of the list and a retry needs
// no re-marking (ADR-0215 decision 6). ADR-0205's narrower rules are inherited
// unchanged: the dashboard's own pane is skipped rather than failing the batch,
// each kill drops the pane's monitor entry in the same breath, and a run that
// empties the monitored set quits the dashboard.
func (d *MonitorDashboard) killPanes(paneIDs []string) tea.Cmd {
	killed := 0
	skipped := false
	var failed []string
	var reasons []string

	for _, paneID := range paneIDs {
		if d.currentPaneID != "" && paneID == d.currentPaneID {
			skipped = true
			continue
		}
		pane, ok := d.paneByID(paneID)
		if !ok {
			// The pane left the monitored set between the prompt and the answer.
			// The set was agreed to, so its absence is reported rather than
			// silently shrinking what the human said yes to.
			failed = append(failed, paneID)
			reasons = append(reasons, paneID+": pane is gone")
			continue
		}
		if err := d.killPaneFunc(paneID); err != nil {
			failed = append(failed, paneID)
			reasons = append(reasons, fmt.Sprintf("%s: %v", killPaneLabel(pane), err))
			continue
		}
		killed++
		d.dropPaneEntry(paneID)
	}

	if killed > 0 {
		d.dirty = true
	}
	// A skipped pane is not a failure, so it is consumed with the successes: the
	// mode is left unless something is still worth retrying.
	d.selection.Clear()
	for _, paneID := range failed {
		d.selection.Toggle(paneID)
	}
	d.flash.Set(killOutcome(killed, reasons, skipped))
	d.clearProtectedPane()
	d.rebuildView()
	if len(d.panes) == 0 {
		d.result = MonitorDashboardResult{Action: MonitorDashboardActionCancel}
		return tea.Quit
	}
	return nil
}

// killOutcome words what a bulk kill did on the one line it has: what it
// destroyed, then the single reason when exactly one pane failed or a bare count
// when several did, then the skip ADR-0205 mandates — because a pane the human
// marked and did not lose is otherwise an unexplained survivor.
func killOutcome(killed int, reasons []string, skipped bool) string {
	var parts []string
	if killed > 0 {
		parts = append(parts, "killed "+paneCount(killed))
	}
	switch len(reasons) {
	case 0:
	case 1:
		parts = append(parts, "1 failed: "+reasons[0])
	default:
		parts = append(parts, fmt.Sprintf("%d failed", len(reasons)))
	}
	if skipped {
		parts = append(parts, "skipped the pane the dashboard is running in")
	}
	return strings.Join(parts, " · ")
}

// unmonitorPanes forgets a whole captured set in one pass, inheriting the same
// failure rule killPanes uses: a pane that left the monitored set between the
// prompt and the answer is reported rather than silently dropped from what the
// human agreed to. Unmonitoring never touches a process, so nothing else can
// fail. The Selection collapses to exactly the failures, and a run that empties
// the monitored set quits the dashboard, exactly as a bulk kill does.
func (d *MonitorDashboard) unmonitorPanes(paneIDs []string) tea.Cmd {
	unmonitored := 0
	var failed []string
	var reasons []string

	for _, paneID := range paneIDs {
		if _, ok := d.paneByID(paneID); !ok {
			failed = append(failed, paneID)
			reasons = append(reasons, paneID+": pane is gone")
			continue
		}
		d.unmonitorFunc(paneID)
		d.dropPaneEntry(paneID)
		unmonitored++
	}

	if unmonitored > 0 {
		d.dirty = true
	}
	d.selection.Clear()
	for _, paneID := range failed {
		d.selection.Toggle(paneID)
	}
	d.flash.Set(unmonitorOutcome(unmonitored, reasons))
	d.clearProtectedPane()
	d.rebuildView()
	if len(d.panes) == 0 {
		d.result = MonitorDashboardResult{Action: MonitorDashboardActionCancel}
		return tea.Quit
	}
	return nil
}

// unmonitorOutcome words a bulk unmonitor's result on the flash line, matching
// killOutcome's grammar: what succeeded, then the single reason when exactly
// one pane failed or a bare count when several did.
func unmonitorOutcome(unmonitored int, reasons []string) string {
	var parts []string
	if unmonitored > 0 {
		parts = append(parts, "unmonitored "+paneCount(unmonitored))
	}
	switch len(reasons) {
	case 0:
	case 1:
		parts = append(parts, "1 failed: "+reasons[0])
	default:
		parts = append(parts, fmt.Sprintf("%d failed", len(reasons)))
	}
	return strings.Join(parts, " · ")
}

// followPanes drives every captured pane to one following value in one pass.
// The direction was decided once when the prompt opened, not re-decided per
// pane — that decision is the whole point of driving a mixed set to one value
// (ADR-0215 decision 5) — so a pane already at the target value is left alone
// rather than toggled past it. Following never touches a process, so the only
// failure is the same staleness gap unmonitorPanes and killPanes both guard: a
// pane gone from the monitored set between the prompt and the answer.
func (d *MonitorDashboard) followPanes(paneIDs []string, follow bool) tea.Cmd {
	changed := 0
	var failed []string
	var reasons []string

	for _, paneID := range paneIDs {
		pane, ok := d.paneByID(paneID)
		if !ok {
			failed = append(failed, paneID)
			reasons = append(reasons, paneID+": pane is gone")
			continue
		}
		if pane.Following == follow {
			continue
		}
		d.toggleFollowFunc(paneID)
		for i := range d.allPanes {
			if d.allPanes[i].PaneID == paneID {
				d.allPanes[i].Following = follow
				break
			}
		}
		changed++
	}

	if changed > 0 {
		d.dirty = true
	}
	d.selection.Clear()
	for _, paneID := range failed {
		d.selection.Toggle(paneID)
	}
	d.flash.Set(followOutcome(follow, changed, reasons))
	d.clearProtectedPane()
	d.rebuildView()
	return nil
}

// followOutcome words a bulk follow or unfollow's result on the flash line,
// matching killOutcome's grammar.
func followOutcome(follow bool, changed int, reasons []string) string {
	verb := "followed"
	if !follow {
		verb = "unfollowed"
	}
	var parts []string
	if changed > 0 {
		parts = append(parts, verb+" "+paneCount(changed))
	}
	switch len(reasons) {
	case 0:
	case 1:
		parts = append(parts, "1 failed: "+reasons[0])
	default:
		parts = append(parts, fmt.Sprintf("%d failed", len(reasons)))
	}
	return strings.Join(parts, " · ")
}

// paneCount words a number of panes for a prompt or an outcome.
func paneCount(n int) string {
	if n == 1 {
		return "1 pane"
	}
	return fmt.Sprintf("%d panes", n)
}

// paneByID finds a pane in the monitored set, which is also the test of whether
// it is still there at all.
func (d *MonitorDashboard) paneByID(paneID string) (AttentionPane, bool) {
	for _, pane := range d.allPanes {
		if pane.PaneID == paneID {
			return pane, true
		}
	}
	return AttentionPane{}, false
}

// dropPaneEntry takes a pane out of the monitored set the dashboard holds. The
// callback already removed the store's entry; this is the view catching up
// immediately rather than waiting for the daemon's five-second sweep, which does
// not run at all when the daemon is down (ADR-0205).
func (d *MonitorDashboard) dropPaneEntry(paneID string) {
	for i := range d.allPanes {
		if d.allPanes[i].PaneID == paneID {
			d.allPanes = append(d.allPanes[:i], d.allPanes[i+1:]...)
			return
		}
	}
}

// killPane destroys a pane and takes its row out of the view. A failed kill
// leaves the row alone and says so; a successful one drops the row immediately
// because the only other cleanup is the daemon's five-second sweep, which does
// not run at all when the daemon is down (ADR-0205).
func (d *MonitorDashboard) killPane(paneID string) tea.Cmd {
	idx := -1
	for i, pane := range d.panes {
		if pane.PaneID == paneID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil
	}
	label := killPaneLabel(d.panes[idx])

	if err := d.killPaneFunc(paneID); err != nil {
		d.flash.Set(fmt.Sprintf("kill failed: %v", err))
		return nil
	}
	d.flash.Set("killed " + label)
	d.dirty = true

	d.dropPaneEntry(paneID)
	d.panes = append(d.panes[:idx], d.panes[idx+1:]...)
	if len(d.panes) == 0 {
		d.result = MonitorDashboardResult{Action: MonitorDashboardActionCancel}
		return tea.Quit
	}
	if d.cursor >= len(d.panes) {
		d.cursor = len(d.panes) - 1
	}
	d.list.SetItems(d.panes)
	d.list.SetCursor(d.cursor)
	d.syncFromList()
	d.fetchPreview()
	return nil
}

// killPromptStyle paints the y/N question in the house accent, the colour the
// mode word and the region separator use: the question is chrome that is on for
// as long as it goes unanswered, not a message that expires like a flash. Every
// bulk confirmation shares it, kill included, since they are one grammar.
var killPromptStyle = lipgloss.NewStyle().Foreground(colorAccent)

// killPaneLabel names a pane in a prompt or a flash, falling back to the tmux
// pane id for a row whose name has not been derived yet.
func killPaneLabel(pane AttentionPane) string {
	if pane.Name != "" {
		return pane.Name
	}
	return pane.PaneID
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
	d.liftSelected()
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

// jumpCursorTo lands the cursor on row i, doing the same bookkeeping a j/k
// step does: the anchored row is released because this is the user navigating
// away, and the preview follows the new selection.
func (d *MonitorDashboard) jumpCursorTo(i int) {
	if len(d.panes) == 0 {
		return
	}
	d.clearProtectedPane()
	d.list.SetCursor(i)
	d.syncFromList()
	d.fetchPreview()
}

// regionTop is where `gg` lands: the top of the region the cursor is in, and the
// top of the whole list on a second press. A cursor already sitting on the first
// row below the region has nowhere nearer to go, so it goes all the way.
func (d *MonitorDashboard) regionTop() int {
	region := d.list.RegionCount()
	if region > 0 && d.cursor > region {
		return region
	}
	return 0
}

// regionBottom is where `G` lands: the bottom of the region the cursor is in, and
// the bottom of the whole list on a second press. For a cursor below the region
// the two are the same row, so one press reaches it.
func (d *MonitorDashboard) regionBottom() int {
	region := d.list.RegionCount()
	if region > 0 && d.cursor < region-1 {
		return region - 1
	}
	return len(d.panes) - 1
}

// toggleSelected answers tab: it marks or unmarks the cursored pane and lands the
// cursor on the row that followed it, which is the outcome tab guarantees
// whichever way the row itself moved (ADR-0215 decision 8). The view is rebuilt
// from the monitored set rather than reordered in place, so an unmarked row goes
// back to its own sorted position instead of to the head of the rest.
func (d *MonitorDashboard) toggleSelected() {
	if d.cursor < 0 || d.cursor >= len(d.panes) {
		return
	}
	next := ""
	if d.cursor+1 < len(d.panes) {
		next = d.panes[d.cursor+1].PaneID
	}
	d.selection.Toggle(d.panes[d.cursor].PaneID)
	// A mark is the human ordering rows deliberately, so it releases a row an
	// earlier in-place mutation anchored: both orderings cannot hold at once.
	d.clearProtectedPane()
	d.rebuildView()
	if next == "" || !d.list.SetCursorToKey(next) {
		// The marked row was last, so there is no next row: stay at the bottom,
		// which is where the row the human just marked used to be.
		d.list.SetCursor(len(d.panes) - 1)
	}
	d.syncFromList()
	d.fetchPreview()
}

// liftSelected moves every marked pane to the top of the view and tells the list
// how many rows its reserved region holds. It is the one place the Selection
// area's ordering is applied, so the region is a property of the view rather than
// of the key that made it.
func (d *MonitorDashboard) liftSelected() {
	// A pane that left the monitored set takes its mark with it, and says
	// nothing: a row that no longer exists cannot be a target.
	d.selection.Retain(d.monitored)
	if !d.selection.Active() {
		d.list.SetRegion(Region{})
		return
	}
	marked, rest := SplitSelected(&d.selection, d.panes, func(p AttentionPane) string { return p.PaneID })
	d.panes = append(marked, rest...)
	d.list.SetRegion(SelectionRegion(d.selection.Len()))
}

// monitored reports whether a pane id is still in the monitored set.
func (d *MonitorDashboard) monitored(paneID string) bool {
	for _, pane := range d.allPanes {
		if pane.PaneID == paneID {
			return true
		}
	}
	return false
}

// refuseSingular answers a binding whose verb acts on one pane while rows are
// marked: it does nothing and says so on the bottom line, because a key that goes
// silently inert is indistinguishable from a bug (ADR-0215 decision 4). The
// capability table decides — a binding it grants `plural` passes straight through
// — so a handler consults it in one line and nothing else has to know the modes.
func (d *MonitorDashboard) refuseSingular(binding *key.Binding) bool {
	if !d.selection.Active() {
		return false
	}
	verb := dashboardVerbs[binding]
	if verb.plural {
		return false
	}
	name := verb.name
	if name == "" {
		// A binding with no entry is singular and unnamed, so the refusal names
		// the key instead — the human still learns which press did nothing.
		name = strings.Join(binding.Keys(), "/")
	}
	d.flash.Set(fmt.Sprintf("%s acts on one pane — shift+tab clears the %d selected", name, d.selection.Len()))
	return true
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
			// A mark outranks the view filter: the human named this row, later
			// than and at least as deliberately as the filter, so no pane is ever
			// selected and invisible (ADR-0215 decision 2).
			if pane.Following || d.selection.Has(pane.PaneID) {
				filtered = append(filtered, pane)
			}
		}
		d.panes = filtered
	} else {
		d.panes = make([]AttentionPane, len(d.allPanes))
		copy(d.panes, d.allPanes)
	}
	d.liftSelected()

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
		Flash:    d.flash,
		Mode:     d.modeWord(),
	}
}

// modeWord names the mode the surface is in, which today is selection mode and
// nothing else. It is derived from the marks themselves, so it appears with the
// first one and goes away with the last.
func (d *MonitorDashboard) modeWord() string {
	if !d.selection.Active() {
		return ""
	}
	return SelectionMode
}

// buildHints returns the hints string based on the current mode.
func (d *MonitorDashboard) buildHints() string {
	// The prompt takes the bottom line the same way a flash does — one line
	// either way, and the question sits where the answer's report will land.
	if d.killPrompt != nil {
		return ConfirmPrompt("Kill " + d.killPrompt.label)
	}
	if d.writePrompt != nil {
		return ConfirmPrompt(d.writePrompt.label)
	}
	hints := "  j/k move · Tab select · Enter open and clear · Shift+Enter open · r toggle unread/clear · f follow · x unmonitor · C-x kill · F follow view · ← back · Esc cancel · C-h help"
	if d.selection.Active() {
		// Only the keys that still do something in the mode, so the line does not
		// advertise the verbs it is refusing.
		hints = "  j/k move · Tab select · S-Tab clear · f follow · x unmonitor · C-x kill · F follow view · ← back · Esc cancel · C-h help"
	}
	if d.pickerMode {
		hints = "  j/k move · Enter select · F follow view · Esc cancel · C-h help"
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
	workingIconStyle := lipgloss.NewStyle().Foreground(colorWorkingSpinner)
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
			{"↑/↓ j/k C-p/C-n", "Navigate"},
			{"gg / G", "Top / bottom"},
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
		{"↑/↓ j/k C-p/C-n", "Navigate"},
		{"gg / G", "Top / bottom"},
		{"Tab", "Select pane"},
		{"Shift+Tab", "Clear selection"},
		{"Enter", "Open and clear unread"},
		{"Shift+Enter / p", "Peek (open without clearing)"},
		{"r", "Toggle unread/clear"},
		{"C-a", "Mark unread"},
		{"f", "Follow pane or selection"},
		{"F", "Toggle follow view"},
		{"x", "Unmonitor pane or selection"},
		{"C-x", "Kill pane or selection"},
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
		// This footer is built by hand rather than by Frame, so it repeats
		// Frame's rule: a live flash takes the bottom line from the hints.
		if line := d.flash.Line(); line != "" {
			eb.WriteString(line)
			return eb.String()
		}
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

	d.list.SetWidth(leftWidth)
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
			// A pane row fills the column by itself; the Selection area's own
			// lines are text of their own length, so the column separator is held
			// straight here rather than in every line that could sit in the list.
			if pad := leftWidth - lipgloss.Width(left); pad > 0 {
				b.WriteString(strings.Repeat(" ", pad))
			}
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
	// Top is the second half of the `gg` chord, so it is read before the flat
	// bindings and only jumps once a first `g` is already pending.
	Top      key.Binding
	Bottom   key.Binding
	KillPane key.Binding
	// ToggleSelect and ClearSelection are the Selection's whole grammar: tab
	// marks the cursored pane, shift+tab drops every mark and thereby leaves
	// selection mode (ADR-0215 decision 8).
	ToggleSelect   key.Binding
	ClearSelection key.Binding
	// KillConfirm and KillCancel are the confirmation's own grammar, matching
	// the Work dashboard's abandon modal. They are read only while the prompt is
	// open, which is why they may reuse keys the dashboard binds elsewhere.
	KillConfirm key.Binding
	KillCancel  key.Binding
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
	Top: key.NewBinding(
		key.WithKeys("g"),
	),
	Bottom: key.NewBinding(
		key.WithKeys("G"),
	),
	Back: key.NewBinding(
		key.WithKeys("left", "h"),
	),
	MarkUnread: key.NewBinding(
		key.WithKeys("ctrl+a"),
	),
	KillPane: key.NewBinding(
		key.WithKeys("ctrl+x"),
	),
	ToggleSelect: key.NewBinding(
		key.WithKeys("tab"),
	),
	ClearSelection: key.NewBinding(
		key.WithKeys("shift+tab"),
	),
	KillConfirm: key.NewBinding(
		key.WithKeys("y"),
	),
	KillCancel: key.NewBinding(
		key.WithKeys("esc", "ctrl+c", "n", "enter"),
	),
}

// monitorVerb is one row of the monitor's capability table: the name a refusal
// calls a binding's verb by, and whether that verb works in selection mode.
type monitorVerb struct {
	name   string
	plural bool
}

// dashboardVerbs declares, per binding, the modes that binding's verb works in.
// It is the monitor's mirror of the capability field work.Action carries: the
// monitor's verbs are callbacks over a pane id with no kind and no Perform to
// hang one on, so the declaration lives beside the keymap instead (ADR-0215
// decision 5).
//
// Silence means singular — a binding absent from this table, or present without
// `plural`, acts on one pane — so bulk is granted one verb at a time by someone
// writing it down here, and the grant list is a reviewable audit rather than an
// invisible default.
var dashboardVerbs = map[*key.Binding]monitorVerb{
	&dashboardKeys.Enter:             {name: "open and clear"},
	&dashboardKeys.PeekPane:          {name: "peek"},
	&dashboardKeys.ToggleClearUnread: {name: "toggle unread"},
	&dashboardKeys.MarkUnread:        {name: "mark unread"},
	&dashboardKeys.FollowPane:        {name: "follow", plural: true},
	&dashboardKeys.Unmonitor:         {name: "unmonitor", plural: true},
	&dashboardKeys.KillPane:          {name: "kill", plural: true},
}
