package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/glebglazov/pop/debug"
	"github.com/junegunn/fzf/src/algo"
	"github.com/junegunn/fzf/src/util"
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

// IconAttention is the icon used to mark items that have panes needing attention.
// Used by the picker to gate the right-arrow attention sub-view.
const IconAttention = "!"

// Item represents a selectable item in the picker
type Item struct {
	Name    string // Display name
	Path    string // Full path (returned on selection)
	Context string // Additional context (e.g., branch name)
	Icon    string // Optional icon displayed to the left of name
}

func (i Item) FilterValue() string {
	return i.Name
}

// UserDefinedCommandResult holds info about a custom command to execute
type UserDefinedCommandResult struct {
	Command string
	Exit    bool
}

// Result holds the picker result
type Result struct {
	Selected           *Item
	Action             Action
	CursorIndex        int                      // cursor position at time of action
	UserDefinedCommand *UserDefinedCommandResult // set when Action == ActionUserDefinedCommand
	AttentionFollowing bool                      // whether following mode was active
}

// Action represents what action the user wants to take
type Action int

const (
	ActionSelect Action = iota
	ActionCancel
	ActionDelete
	ActionForceDelete
	ActionKillSession
	ActionReset
	ActionOpenWindow
	ActionUserDefinedCommand
	ActionSwitchToPane
	ActionRefresh
)

// AttentionStatus indicates why a pane appears in the attention view
type AttentionStatus int

const (
	AttentionNeedsAttention AttentionStatus = iota
	AttentionWorking
	AttentionIdle
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

// Picker is a fuzzy-searchable list picker
type Picker struct {
	items    []Item
	filtered []Item
	input    textinput.Model
	cursor   int
	scroll   int // scroll offset (index of first visible item)
	height   int
	width    int
	result   Result

	showHelp        bool
	showDelete      bool
	showContext     bool
	showKillSession bool
	showReset       bool
	showOpenWindow  bool
	cursorAtEnd     bool

	// Quick access: modifier+digit to select items above cursor
	quickAccessModifier string // "alt" (default), "ctrl", or "disabled"

	// Cursor memory: remembers selected item per filter query
	cursorMemory map[string]cursorState // filter query -> cursor state
	lastQuery    string                 // previous filter query (to detect changes)

	// Custom commands
	customCommands []UserDefinedKeyBinding

	// Icon legend entries for help view
	iconLegend []iconLegendEntry

	// Initial cursor index override (-1 = not set)
	initialCursorIdx int

	// Warnings to display in the picker
	warnings []string

	// Attention sub-view
	attentionMode      bool
	attentionPanes     []AttentionPane
	attentionAllPanes  []AttentionPane // full list (source of truth for dashboard)
	attentionFollowing bool            // true = showing followed-only view
	attentionCursor  int
	attentionScroll  int
	attentionPreview   string
	attentionTitle     string
	attentionEmptyNote string
	attentionDirty     bool
	previewFunc        func(paneID string) string
	reloadFunc         func() []AttentionPane
	markReadFunc       func(paneID string)
	markAttentionFunc  func(paneID string)
	toggleFollowFunc   func(paneID string)
	unmonitorFunc      func(paneID string)
	setNoteFunc        func(paneID, note string)
	spinnerFrame       int // current spinner animation frame

	// Note editing state
	editingNote bool
	noteInput   textinput.Model
}

// iconLegendEntry maps an icon to its description in the help view
type iconLegendEntry struct {
	icon string
	desc string
}

// cursorState stores cursor position info for a filter query
type cursorState struct {
	path      string // selected item's path
	screenPos int    // cursor position relative to visible area (0 = top of visible)
}

// UserDefinedKeyBinding holds a custom key binding and its associated command
type UserDefinedKeyBinding struct {
	Binding key.Binding
	Command string
	Label   string
	Exit    bool
}

// UserDefinedCommand defines a custom command to add to the picker
type UserDefinedCommand struct {
	Key     string
	Label   string
	Command string
	Exit    bool
}

// PickerOption configures the picker
type PickerOption func(*Picker)

// WithDelete enables delete keybindings
func WithDelete() PickerOption {
	return func(p *Picker) {
		p.showDelete = true
	}
}

// WithContext enables displaying item context (e.g., branch names)
func WithContext() PickerOption {
	return func(p *Picker) {
		p.showContext = true
	}
}

// WithKillSession enables kill session keybinding (ctrl+k)
func WithKillSession() PickerOption {
	return func(p *Picker) {
		p.showKillSession = true
	}
}

// WithReset enables reset (remove from history) keybinding (ctrl+r)
func WithReset() PickerOption {
	return func(p *Picker) {
		p.showReset = true
	}
}

// WithOpenWindow enables open-in-tmux-window keybinding (ctrl+o)
func WithOpenWindow() PickerOption {
	return func(p *Picker) {
		p.showOpenWindow = true
	}
}

// WithCursorAtEnd starts the cursor at the last item
func WithCursorAtEnd() PickerOption {
	return func(p *Picker) {
		p.cursorAtEnd = true
	}
}

// WithQuickAccess enables quick access shortcuts with the given modifier
func WithQuickAccess(modifier string) PickerOption {
	return func(p *Picker) {
		if modifier == "" {
			modifier = "alt"
		}
		p.quickAccessModifier = modifier
	}
}

// WithIconLegend adds icon descriptions to the help view.
// Only icons that appear in the current item list are shown.
func WithIconLegend(entries ...IconLegend) PickerOption {
	return func(p *Picker) {
		for _, e := range entries {
			p.iconLegend = append(p.iconLegend, iconLegendEntry{icon: e.Icon, desc: e.Desc})
		}
	}
}

// IconLegend describes what an icon means in the help view
type IconLegend struct {
	Icon string
	Desc string
}

// WithInitialCursorIndex sets the initial cursor position by index.
// Takes priority over WithCursorAtEnd. Index is clamped to bounds.
func WithInitialCursorIndex(idx int) PickerOption {
	return func(p *Picker) {
		p.initialCursorIdx = idx
	}
}

// WithUserDefinedCommands adds custom key bindings and commands to the picker
func WithUserDefinedCommands(commands []UserDefinedCommand) PickerOption {
	return func(p *Picker) {
		for _, cmd := range commands {
			binding := key.NewBinding(key.WithKeys(cmd.Key))
			p.customCommands = append(p.customCommands, UserDefinedKeyBinding{
				Binding: binding,
				Command: cmd.Command,
				Label:   cmd.Label,
				Exit:    cmd.Exit,
			})
		}
	}
}

// WithWarnings adds warning messages to display in the picker
func WithWarnings(warnings []string) PickerOption {
	return func(p *Picker) {
		p.warnings = warnings
	}
}

// AttentionCallbacks holds callback functions for the attention sub-view.
type AttentionCallbacks struct {
	Preview       func(paneID string) string // returns pane content for preview
	MarkRead      func(paneID string)        // marks a pane as read
	MarkAttention func(paneID string)        // marks a pane as needs-attention
	ToggleFollow  func(paneID string)        // toggles following flag
	Unmonitor     func(paneID string)        // removes a pane from monitor state
	SetNote       func(paneID, note string)  // sets note on a pane
}

// WithAttentionPanes enables the attention sub-view with the given panes and callbacks.
func WithAttentionPanes(panes []AttentionPane, cb AttentionCallbacks) PickerOption {
	return func(p *Picker) {
		p.attentionAllPanes = panes
		p.attentionPanes = make([]AttentionPane, len(panes))
		copy(p.attentionPanes, panes)
		p.previewFunc = cb.Preview
		p.markReadFunc = cb.MarkRead
		p.markAttentionFunc = cb.MarkAttention
		p.toggleFollowFunc = cb.ToggleFollow
		p.unmonitorFunc = cb.Unmonitor
		p.setNoteFunc = cb.SetNote
	}
}

// WithAttentionEmptyNote sets a note line shown below the "No panes need attention" message.
func WithAttentionEmptyNote(note string) PickerOption {
	return func(p *Picker) {
		p.attentionEmptyNote = note
	}
}

// WithAttentionReload sets a function that reloads attention panes when "r" is pressed in the empty state.
func WithAttentionReload(fn func() []AttentionPane) PickerOption {
	return func(p *Picker) {
		p.reloadFunc = fn
	}
}

// WithAttentionFollowing sets the initial following mode for the attention view.
func WithAttentionFollowing(following bool) PickerOption {
	return func(p *Picker) {
		p.attentionFollowing = following
	}
}

// NewPicker creates a new picker with the given items
func NewPicker(items []Item, opts ...PickerOption) *Picker {
	ti := newTextInput()

	p := &Picker{
		items:            items,
		filtered:         items,
		input:            ti,
		height:           10,
		cursorMemory:     make(map[string]cursorState),
		initialCursorIdx: -1,
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

func (p *Picker) hasWorkingPanes() bool {
	for _, pane := range p.attentionPanes {
		if pane.Status == AttentionWorking {
			return true
		}
	}
	return false
}

func (p *Picker) Init() tea.Cmd {
	if p.initialCursorIdx >= 0 && len(p.filtered) > 0 {
		p.cursor = p.initialCursorIdx
		if p.cursor >= len(p.filtered) {
			p.cursor = len(p.filtered) - 1
		}
	} else if p.cursorAtEnd && len(p.filtered) > 0 {
		p.cursor = len(p.filtered) - 1
	}
	p.adjustScroll()
	var cmds []tea.Cmd
	if p.hasWorkingPanes() {
		cmds = append(cmds, spinnerTick())
	}
	if p.reloadFunc != nil {
		cmds = append(cmds, reloadTick())
	}
	return tea.Batch(cmds...)
}

func (p *Picker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	case spinnerTickMsg:
		p.spinnerFrame = (p.spinnerFrame + 1) % len(spinnerFrames)
		if p.hasWorkingPanes() {
			return p, spinnerTick()
		}
		return p, nil
	case reloadTickMsg:
		if p.reloadFunc != nil {
			hadWorking := p.hasWorkingPanes()
			p.reloadAttentionPanes()
			cmds := []tea.Cmd{reloadTick()}
			// Start spinner if reload introduced working panes
			if !hadWorking && p.hasWorkingPanes() {
				cmds = append(cmds, spinnerTick())
			}
			return p, tea.Batch(cmds...)
		}
		return p, nil
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		// Help overlay: esc dismisses, all other keys are swallowed
		if p.showHelp {
			if key.Matches(msg, keys.Quit) {
				p.showHelp = false
			}
			return p, nil
		}

		// Toggle help overlay
		if key.Matches(msg, keys.Help) {
			p.showHelp = true
			return p, nil
		}

		// Attention sub-view: handle keys when in attention mode
		if p.attentionMode {
			return p.updateAttention(msg)
		}

		switch {
		case key.Matches(msg, keys.Quit):
			p.result = Result{Action: ActionCancel}
			return p, tea.Quit

		case key.Matches(msg, keys.Enter):
			if len(p.filtered) > 0 {
				p.result = Result{
					Selected: &p.filtered[p.cursor],
					Action:   ActionSelect,
				}
			}
			return p, tea.Quit

		case key.Matches(msg, keys.Up):
			if len(p.filtered) > 0 {
				if p.cursor > 0 {
					p.cursor--
				} else {
					p.cursor = len(p.filtered) - 1 // wrap to bottom
				}
				p.adjustScroll()
			}
			return p, nil

		case key.Matches(msg, keys.Down):
			if len(p.filtered) > 0 {
				if p.cursor < len(p.filtered)-1 {
					p.cursor++
				} else {
					p.cursor = 0 // wrap to top
				}
				p.adjustScroll()
			}
			return p, nil

		case key.Matches(msg, keys.HalfPageUp):
			if len(p.filtered) > 0 {
				page := p.height
				if page < 1 {
					page = 1
				}
				p.cursor -= page
				if p.cursor < 0 {
					p.cursor = 0
				}
				p.adjustScroll()
			}
			return p, nil

		case key.Matches(msg, keys.HalfPageDown):
			if len(p.filtered) > 0 {
				page := p.height
				if page < 1 {
					page = 1
				}
				p.cursor += page
				if p.cursor >= len(p.filtered) {
					p.cursor = len(p.filtered) - 1
				}
				p.adjustScroll()
			}
			return p, nil

		case p.matchUserDefinedCommand(msg) != nil:
			cc := p.matchUserDefinedCommand(msg)
			p.result = Result{
				Action: ActionUserDefinedCommand,
				UserDefinedCommand: &UserDefinedCommandResult{
					Command: cc.Command,
					Exit:    cc.Exit,
				},
			}
			if len(p.filtered) > 0 {
				p.result.Selected = &p.filtered[p.cursor]
			}
			return p, tea.Quit

		case key.Matches(msg, keys.Delete):
			if p.showDelete && len(p.filtered) > 0 {
				p.result = Result{
					Selected: &p.filtered[p.cursor],
					Action:   ActionDelete,
				}
				return p, tea.Quit
			}

		case key.Matches(msg, keys.ForceDelete):
			if p.showDelete && len(p.filtered) > 0 {
				p.result = Result{
					Selected: &p.filtered[p.cursor],
					Action:   ActionForceDelete,
				}
				return p, tea.Quit
			}

		case key.Matches(msg, keys.KillSession):
			if p.showKillSession && len(p.filtered) > 0 {
				p.result = Result{
					Selected: &p.filtered[p.cursor],
					Action:   ActionKillSession,
				}
				return p, tea.Quit
			}

		case key.Matches(msg, keys.Reset):
			if p.showReset && len(p.filtered) > 0 {
				p.result = Result{
					Selected: &p.filtered[p.cursor],
					Action:   ActionReset,
				}
				return p, tea.Quit
			}

		case key.Matches(msg, keys.OpenWindow):
			if p.showOpenWindow && len(p.filtered) > 0 {
				p.result = Result{
					Selected: &p.filtered[p.cursor],
					Action:   ActionOpenWindow,
				}
				return p, tea.Quit
			}

		case key.Matches(msg, keys.ClearInput):
			p.input.SetValue("")
			p.filter()
			return p, nil

		case p.isQuickAccessKey(msg):
			n := p.quickAccessDigit(msg)
			targetIdx := p.cursor - n
			if targetIdx >= 0 && targetIdx < len(p.filtered) {
				p.result = Result{
					Selected: &p.filtered[targetIdx],
					Action:   ActionSelect,
				}
				return p, tea.Quit
			}
			return p, nil

		case key.Matches(msg, keys.Attention):
			if len(p.attentionPanes) > 0 && len(p.filtered) > 0 {
				// Only enter attention mode if the selected item has the attention icon
				if p.filtered[p.cursor].Icon == IconAttention {
					p.attentionMode = true
					p.attentionCursor = 0
					p.attentionScroll = 0
					p.fetchAttentionPreview()
					return p, nil
				}
			}
		}

	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height - 4 // Reserve space for hints (1 line) + input box (3 lines)
		if p.height < 3 {
			p.height = 3
		}
		if p.attentionMode {
			p.adjustAttentionScroll()
		}
	}

	// Update text input
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)

	// Filter items
	p.filter()

	return p, cmd
}

// updateAttention handles key events when in attention sub-view mode
func (p *Picker) updateAttention(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Note editing mode: capture all keys
	if p.editingNote {
		switch {
		case key.Matches(msg, keys.Quit): // Esc/Ctrl+C
			p.editingNote = false
			return p, nil
		case key.Matches(msg, keys.Enter):
			note := strings.TrimSpace(p.noteInput.Value())
			pane := &p.attentionPanes[p.attentionCursor]
			// Auto-follow on save if not already
			if !pane.Following && p.toggleFollowFunc != nil {
				p.toggleFollowFunc(pane.PaneID)
				pane.Following = true
				for i := range p.attentionAllPanes {
					if p.attentionAllPanes[i].PaneID == pane.PaneID {
						p.attentionAllPanes[i].Following = true
						break
					}
				}
			}
			p.setNoteFunc(pane.PaneID, note)
			pane.Note = note
			for i := range p.attentionAllPanes {
				if p.attentionAllPanes[i].PaneID == pane.PaneID {
					p.attentionAllPanes[i].Note = note
					break
				}
			}
			p.attentionDirty = true
			p.editingNote = false
			return p, nil
		default:
			var cmd tea.Cmd
			p.noteInput, cmd = p.noteInput.Update(msg)
			return p, cmd
		}
	}

	switch {
	case key.Matches(msg, keys.Back):
		if len(p.items) == 0 {
			p.result = Result{Action: ActionCancel}
			return p, tea.Quit
		}
		if p.attentionDirty {
			p.result = Result{Action: ActionRefresh}
			return p, tea.Quit
		}
		p.attentionMode = false
		return p, nil

	case key.Matches(msg, keys.Quit):
		if msg.Code == 0x1b && len(p.items) > 0 { // esc — go back to normal view
			if p.attentionDirty {
				p.result = Result{Action: ActionRefresh}
				return p, tea.Quit
			}
			p.attentionMode = false
			return p, nil
		}
		// esc in standalone mode or ctrl+c — quit
		p.result = Result{Action: ActionCancel}
		return p, tea.Quit

	case key.Matches(msg, keys.Enter):
		if len(p.attentionPanes) == 0 {
			p.result = Result{Action: ActionCancel}
			return p, tea.Quit
		}
		pane := p.attentionPanes[p.attentionCursor]
		p.result = Result{
			Selected: &Item{Name: pane.Name, Path: pane.PaneID, Context: pane.Session},
			Action:   ActionSwitchToPane,
		}
		return p, tea.Quit

	case key.Matches(msg, keys.AttentionUp):
		if len(p.attentionPanes) > 0 {
			if p.attentionCursor > 0 {
				p.attentionCursor--
			} else {
				p.attentionCursor = len(p.attentionPanes) - 1
			}
			p.adjustAttentionScroll()
			p.fetchAttentionPreview()
		}
		return p, nil

	case key.Matches(msg, keys.AttentionDown):
		if len(p.attentionPanes) > 0 {
			if p.attentionCursor < len(p.attentionPanes)-1 {
				p.attentionCursor++
			} else {
				p.attentionCursor = 0
			}
			p.adjustAttentionScroll()
			p.fetchAttentionPreview()
		}
		return p, nil

	case key.Matches(msg, keys.Reload):
		if len(p.attentionPanes) == 0 && p.reloadFunc != nil {
			p.attentionAllPanes = p.reloadFunc()
			p.rebuildAttentionView()
			p.attentionCursor = 0
			p.attentionScroll = 0
			p.fetchAttentionPreview()
		}
		return p, nil

	case key.Matches(msg, keys.Reset):
		if len(p.attentionPanes) > 0 && p.markReadFunc != nil {
			pane := &p.attentionPanes[p.attentionCursor]
			p.markReadFunc(pane.PaneID)
			pane.Status = AttentionIdle
			p.updateAllPanesStatus(pane.PaneID, AttentionIdle)
			p.sortAttentionPanes()
			if p.attentionCursor >= len(p.attentionPanes) {
				p.attentionCursor = len(p.attentionPanes) - 1
			}
			p.attentionDirty = true
			p.adjustAttentionScroll()
			p.fetchAttentionPreview()
		}
		return p, nil

	case key.Matches(msg, keys.MarkAttention):
		if len(p.attentionPanes) > 0 && p.markAttentionFunc != nil {
			pane := &p.attentionPanes[p.attentionCursor]
			p.markAttentionFunc(pane.PaneID)
			pane.Status = AttentionNeedsAttention
			p.updateAllPanesStatus(pane.PaneID, AttentionNeedsAttention)
			p.sortAttentionPanes()
			if p.attentionCursor >= len(p.attentionPanes) {
				p.attentionCursor = len(p.attentionPanes) - 1
			}
			p.attentionDirty = true
			p.adjustAttentionScroll()
			p.fetchAttentionPreview()
		}
		return p, nil

	case key.Matches(msg, keys.FollowPane):
		if len(p.attentionPanes) > 0 && p.toggleFollowFunc != nil {
			pane := &p.attentionPanes[p.attentionCursor]
			p.toggleFollowFunc(pane.PaneID)
			pane.Following = !pane.Following
			// Clear note when unfollowing
			if !pane.Following && pane.Note != "" && p.setNoteFunc != nil {
				p.setNoteFunc(pane.PaneID, "")
				pane.Note = ""
			}
			// Update source-of-truth list
			for i := range p.attentionAllPanes {
				if p.attentionAllPanes[i].PaneID == pane.PaneID {
					p.attentionAllPanes[i].Following = pane.Following
					p.attentionAllPanes[i].Note = pane.Note
					break
				}
			}
			p.attentionDirty = true
			// If in following view and we just unfollowed, rebuild to remove it
			if p.attentionFollowing && !pane.Following {
				p.rebuildAttentionView()
			}
		}
		return p, nil

	case key.Matches(msg, keys.ToggleFollowView):
		p.attentionFollowing = !p.attentionFollowing
		p.rebuildAttentionView()
		if p.hasWorkingPanes() {
			return p, spinnerTick()
		}
		return p, nil

	case key.Matches(msg, keys.EditNote):
		if len(p.attentionPanes) > 0 && p.setNoteFunc != nil {
			pane := p.attentionPanes[p.attentionCursor]
			p.editingNote = true
			p.noteInput = textinput.New()
			p.noteInput.Prompt = "note: "
			p.noteInput.SetValue(pane.Note)
			p.noteInput.Focus()
		}
		return p, nil

	case key.Matches(msg, keys.ForceDelete):
		if len(p.attentionPanes) > 0 && p.unmonitorFunc != nil {
			pane := p.attentionPanes[p.attentionCursor]
			p.unmonitorFunc(pane.PaneID)
			p.attentionDirty = true
			// Remove from source-of-truth list
			for i := range p.attentionAllPanes {
				if p.attentionAllPanes[i].PaneID == pane.PaneID {
					p.attentionAllPanes = append(p.attentionAllPanes[:i], p.attentionAllPanes[i+1:]...)
					break
				}
			}
			p.attentionPanes = append(p.attentionPanes[:p.attentionCursor], p.attentionPanes[p.attentionCursor+1:]...)
			if len(p.attentionPanes) == 0 {
				if len(p.items) == 0 {
					p.result = Result{Action: ActionCancel}
					return p, tea.Quit
				}
				p.result = Result{Action: ActionRefresh}
				return p, tea.Quit
			}
			if p.attentionCursor >= len(p.attentionPanes) {
				p.attentionCursor = 0
			}
			p.adjustAttentionScroll()
			p.fetchAttentionPreview()
		}
		return p, nil
	}
	return p, nil
}

// reloadAttentionPanes refreshes the attention pane list from the reload function,
// preserving the cursor on the same pane when possible.
func (p *Picker) reloadAttentionPanes() {
	if p.reloadFunc == nil {
		return
	}
	// Remember current selection
	var selectedPaneID string
	if p.attentionCursor < len(p.attentionPanes) {
		selectedPaneID = p.attentionPanes[p.attentionCursor].PaneID
	}

	p.attentionAllPanes = p.reloadFunc()
	p.rebuildAttentionView()

	// Try to restore cursor to the same pane
	restored := false
	if selectedPaneID != "" {
		for i, pane := range p.attentionPanes {
			if pane.PaneID == selectedPaneID {
				p.attentionCursor = i
				restored = true
				break
			}
		}
	}
	if !restored {
		if len(p.attentionPanes) > 0 {
			p.attentionCursor = len(p.attentionPanes) - 1
		} else {
			p.attentionCursor = 0
		}
	}
	p.adjustAttentionScroll()
	p.fetchAttentionPreview()
}

// sortAttentionPanes performs a stable sort of attention panes by status group:
// idle (top) → working (middle) → needs_attention (bottom, closest to cursor).
func (p *Picker) sortAttentionPanes() {
	sort.SliceStable(p.attentionAllPanes, func(i, j int) bool {
		return attentionStatusOrder(p.attentionAllPanes[i].Status) < attentionStatusOrder(p.attentionAllPanes[j].Status)
	})
	sort.SliceStable(p.attentionPanes, func(i, j int) bool {
		return attentionStatusOrder(p.attentionPanes[i].Status) < attentionStatusOrder(p.attentionPanes[j].Status)
	})
}

func attentionStatusOrder(s AttentionStatus) int {
	switch s {
	case AttentionIdle:
		return 0
	case AttentionWorking:
		return 1
	case AttentionNeedsAttention:
		return 2
	default:
		return 0
	}
}

// updateAllPanesStatus syncs a status change to the attentionAllPanes source-of-truth list.
func (p *Picker) updateAllPanesStatus(paneID string, status AttentionStatus) {
	for i := range p.attentionAllPanes {
		if p.attentionAllPanes[i].PaneID == paneID {
			p.attentionAllPanes[i].Status = status
			break
		}
	}
}

// rebuildAttentionView filters attentionAllPanes into attentionPanes
// based on the current view mode, clamping cursor to bounds.
func (p *Picker) rebuildAttentionView() {
	// Remember currently selected pane to restore cursor after rebuild
	var selectedPaneID string
	if p.attentionCursor >= 0 && p.attentionCursor < len(p.attentionPanes) {
		selectedPaneID = p.attentionPanes[p.attentionCursor].PaneID
	}

	if p.attentionFollowing {
		filtered := make([]AttentionPane, 0)
		for _, pane := range p.attentionAllPanes {
			if pane.Following {
				filtered = append(filtered, pane)
			}
		}
		p.attentionPanes = filtered
	} else {
		p.attentionPanes = make([]AttentionPane, len(p.attentionAllPanes))
		copy(p.attentionPanes, p.attentionAllPanes)
	}

	// Try to restore cursor to the same pane
	restored := false
	if selectedPaneID != "" {
		for i, pane := range p.attentionPanes {
			if pane.PaneID == selectedPaneID {
				p.attentionCursor = i
				restored = true
				break
			}
		}
	}
	if !restored {
		if p.attentionCursor >= len(p.attentionPanes) {
			p.attentionCursor = len(p.attentionPanes) - 1
		}
		if p.attentionCursor < 0 {
			p.attentionCursor = 0
		}
	}
	p.adjustAttentionScroll()
	p.fetchAttentionPreview()
}

// fetchAttentionPreview calls the preview function for the currently selected attention pane
func (p *Picker) fetchAttentionPreview() {
	if p.previewFunc == nil || len(p.attentionPanes) == 0 {
		p.attentionPreview = ""
		return
	}
	p.attentionPreview = p.previewFunc(p.attentionPanes[p.attentionCursor].PaneID)
}

// adjustAttentionScroll ensures the attention cursor is visible
func (p *Picker) adjustAttentionScroll() {
	listHeight := p.height + 2 // match viewAttention's visible row count
	if listHeight <= 0 {
		listHeight = 1
	}
	// Don't scroll past what's needed to fill the viewport
	maxScroll := len(p.attentionPanes) - listHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	if p.attentionScroll > maxScroll {
		p.attentionScroll = maxScroll
	}
	// Ensure cursor is visible
	if p.attentionCursor < p.attentionScroll {
		p.attentionScroll = p.attentionCursor
	}
	if p.attentionCursor >= p.attentionScroll+listHeight {
		p.attentionScroll = p.attentionCursor - listHeight + 1
	}
}

// fzfMatch holds an item with its fuzzy match score
type fzfMatch struct {
	item  Item
	score int
}

func (p *Picker) filter() {
	query := p.input.Value()
	queryChanged := query != p.lastQuery

	// Save current selection and screen position before changing filter
	if queryChanged && len(p.filtered) > 0 && p.cursor < len(p.filtered) {
		state := cursorState{
			path:      p.filtered[p.cursor].Path,
			screenPos: p.cursor - p.scroll,
		}
		p.cursorMemory[p.lastQuery] = state
		debug.Log("filter: query %q -> %q, saving cursor for %q: path=%q screenPos=%d", p.lastQuery, query, p.lastQuery, state.path, state.screenPos)
	}

	// Build filtered list
	if query == "" {
		p.filtered = p.items
	} else {
		// Use fzf's algorithm for fuzzy matching
		pattern := []rune(strings.ToLower(query))
		slab := util.MakeSlab(100*1024, 2048)

		var matches []fzfMatch
		for _, item := range p.items {
			chars := util.ToChars([]byte(strings.ToLower(item.Name)))
			result, _ := algo.FuzzyMatchV2(false, true, true, &chars, pattern, false, slab)
			if result.Score > 0 {
				matches = append(matches, fzfMatch{item: item, score: result.Score})
			}
		}

		sort.Slice(matches, func(i, j int) bool {
			return matches[i].score < matches[j].score
		})

		p.filtered = make([]Item, len(matches))
		for i, m := range matches {
			p.filtered[i] = m.item
		}
	}

	// Position cursor and scroll
	if queryChanged {
		if state, ok := p.cursorMemory[query]; ok {
			// Restore cursor to remembered item for this query
			idx := p.findItemIndex(state.path)
			debug.Log("filter: restoring cursor for %q: path=%q idx=%d screenPos=%d", query, state.path, idx, state.screenPos)
			p.cursor = idx
			// Restore relative screen position
			p.scroll = p.cursor - state.screenPos
		} else {
			// First time seeing this query: cursor at best match (bottom)
			p.cursor = len(p.filtered) - 1
			p.scroll = 0 // will be adjusted below
			debug.Log("filter: first time query %q, cursor at bottom (%d), %d items", query, p.cursor, len(p.filtered))
		}
	}

	p.lastQuery = query

	// Ensure cursor is in bounds
	if p.cursor >= len(p.filtered) {
		p.cursor = len(p.filtered) - 1
	}
	if p.cursor < 0 {
		p.cursor = 0
	}

	p.adjustScroll()
}

// findItemIndex returns the index of the item with the given path, or -1 if not found
func (p *Picker) findItemIndex(path string) int {
	for i, item := range p.filtered {
		if item.Path == path {
			return i
		}
	}
	debug.Log("findItemIndex: path %q not found in %d filtered items", path, len(p.filtered))
	return -1
}

// buildHints returns the hints string based on enabled features
func (p *Picker) buildHints() string {
	hints := "  Enter select · Esc quit · F1 help"
	if len(p.attentionPanes) > 0 {
		hints += " · → attention"
	}
	return hints
}

// formatKeyHint converts a key binding to a display-friendly hint format
func formatKeyHint(b key.Binding) string {
	keys := b.Keys()
	if len(keys) == 0 {
		return ""
	}
	k := keys[0]
	// Convert common key formats to hint format
	k = strings.ReplaceAll(k, "ctrl+", "C-")
	k = strings.ReplaceAll(k, "ctrl-", "C-")
	k = strings.ReplaceAll(k, "alt+", "A-")
	k = strings.ReplaceAll(k, "alt-", "A-")
	return k
}

// matchUserDefinedCommand returns the first user-defined command binding that
// matches the given key message, or nil if none match.
func (p *Picker) matchUserDefinedCommand(msg tea.KeyPressMsg) *UserDefinedKeyBinding {
	for i := range p.customCommands {
		if key.Matches(msg, p.customCommands[i].Binding) {
			return &p.customCommands[i]
		}
	}
	return nil
}

// isKeyOverridden returns true if any user-defined command uses one of the given keys.
func (p *Picker) isKeyOverridden(builtinKeys ...string) bool {
	for _, cc := range p.customCommands {
		for _, ck := range cc.Binding.Keys() {
			for _, bk := range builtinKeys {
				if ck == bk {
					return true
				}
			}
		}
	}
	return false
}

// isQuickAccessKey returns true if the key message is a quick access trigger
func (p *Picker) isQuickAccessKey(msg tea.KeyPressMsg) bool {
	return p.quickAccessDigit(msg) >= 1
}

// quickAccessDigit extracts the digit (1-9) from a quick access key message.
// Returns 0 if the key is not a valid quick access trigger.
func (p *Picker) quickAccessDigit(msg tea.KeyPressMsg) int {
	if msg.Code < '1' || msg.Code > '9' {
		return 0
	}
	digit := int(msg.Code - '0')
	switch p.quickAccessModifier {
	case "alt":
		if msg.Mod.Contains(tea.ModAlt) {
			return digit
		}
	case "ctrl":
		if msg.Mod.Contains(tea.ModCtrl) {
			return digit
		}
	}
	return 0
}

// quickAccessLabel returns the display label for a quick access number (e.g., "^1", "⌥2")
func (p *Picker) quickAccessLabel(n int) string {
	switch p.quickAccessModifier {
	case "ctrl":
		return fmt.Sprintf("^%d ", n)
	case "alt":
		return fmt.Sprintf("⌥%d ", n)
	default:
		return "   "
	}
}

// quickAccessPadding returns the blank padding matching quickAccessLabel width
func (p *Picker) quickAccessPadding() string {
	if p.quickAccessEnabled() {
		return "   "
	}
	return "  "
}

// quickAccessEnabled returns true if quick access is active (not disabled or empty)
func (p *Picker) quickAccessEnabled() bool {
	return p.quickAccessModifier != "" && p.quickAccessModifier != "disabled"
}

func (p *Picker) adjustScroll() {
	margin := 0
	if p.quickAccessEnabled() {
		margin = 9
	}
	p.scroll = adjustScroll(p.cursor, p.scroll, p.height, len(p.filtered), margin)
}

func (p *Picker) View() tea.View {
	var content string
	if p.showHelp {
		content = p.viewHelp()
	} else if p.attentionMode {
		content = p.viewAttention()
	} else {
		content = p.viewNormal()
	}
	v := tea.NewView(content)
	v.AltScreen = true
	v.KeyboardEnhancements = tea.KeyboardEnhancements{}
	return v
}

func (p *Picker) viewHelp() string {
	var b strings.Builder

	type helpEntry struct {
		key  string
		desc string
	}

	entries := []helpEntry{
		{"↑/↓ C-p/C-n", "Navigate"},
		{"C-b/C-f", "Page up / down"},
		{"C-u", "Clear filter"},
		{"Enter", "Select"},
		{"Esc", "Quit"},
	}

	if p.showKillSession && !p.isKeyOverridden("ctrl+k") {
		entries = append(entries, helpEntry{"C-k", "Kill tmux session"})
	}
	if p.showReset && !p.isKeyOverridden("ctrl+r") {
		entries = append(entries, helpEntry{"C-r", "Reset history"})
	}
	if p.showOpenWindow && !p.isKeyOverridden("ctrl+o") {
		entries = append(entries, helpEntry{"C-o", "Open in window"})
	}
	if p.showDelete && !p.isKeyOverridden("ctrl+d") {
		entries = append(entries, helpEntry{"C-d", "Delete"})
	}
	if p.showDelete && !p.isKeyOverridden("ctrl+x") {
		entries = append(entries, helpEntry{"C-x", "Force delete"})
	}
	switch p.quickAccessModifier {
	case "alt":
		entries = append(entries, helpEntry{"A-1..9", "Quick select"})
	case "ctrl":
		entries = append(entries, helpEntry{"C-1..9", "Quick select"})
	}

	for _, cc := range p.customCommands {
		entries = append(entries, helpEntry{formatKeyHint(cc.Binding), cc.Label})
	}

	// Icon legend: show entries for icons present in the item list
	iconsSeen := make(map[string]bool)
	for _, item := range p.items {
		if item.Icon != "" {
			iconsSeen[item.Icon] = true
		}
	}
	if len(iconsSeen) > 0 {
		entries = append(entries, helpEntry{"", ""}) // blank separator
		for _, legend := range p.iconLegend {
			if iconsSeen[legend.icon] {
				entries = append(entries, helpEntry{legend.icon, legend.desc})
			}
		}
	}

	// Find max key display width for alignment
	maxKeyWidth := 0
	for _, e := range entries {
		if w := lipgloss.Width(e.key); w > maxKeyWidth {
			maxKeyWidth = w
		}
	}

	// Build help lines
	var helpLines []string
	for _, e := range entries {
		padding := maxKeyWidth - lipgloss.Width(e.key)
		helpLines = append(helpLines, "  "+e.key+strings.Repeat(" ", padding)+"   "+e.desc)
	}

	// Push content to bottom
	emptyLines := p.height - len(helpLines)
	for i := 0; i < emptyLines; i++ {
		b.WriteString("\n")
	}

	for _, line := range helpLines {
		b.WriteString(line)
		b.WriteString("\n")
	}

	writeInputBox(&b, p.width, " Help")

	// Hints line
	b.WriteString(hintStyle.Render("  Esc back"))

	return b.String()
}

func (p *Picker) viewAttention() string {
	var b strings.Builder

	sepStyle := lipgloss.NewStyle().
		Foreground(colorSeparator)

	// Layout: left panel (pane list) + separator + right panel (preview)
	leftWidth := p.width * 3 / 10
	if leftWidth < 15 {
		leftWidth = 15
	}
	rightWidth := p.width - leftWidth - 1 // 1 for separator
	if rightWidth < 10 {
		rightWidth = 10
	}

	// Reserve 1 line for hints + 1 line for header
	listHeight := p.height + 2 // viewNormal reserves 4 lines; we need 1 for hints + 1 for header

	// Empty panes: show title + dismissable message
	if len(p.attentionPanes) == 0 {
		msgStyle := lipgloss.NewStyle().Foreground(colorDim)
		var eb strings.Builder
		headerText := p.attentionTitle
		if p.attentionFollowing {
			headerText += " · following"
		} else {
			headerText += " · normal"
		}
		eb.WriteString(headerStyle.Render(" " + headerText))
		eb.WriteString("\n")
		for i := 0; i < p.height-1; i++ {
			eb.WriteString("\n")
		}
		if p.attentionFollowing {
			eb.WriteString(msgStyle.Render("  No followed panes"))
		} else {
			eb.WriteString(msgStyle.Render("  No active panes"))
		}
		if p.attentionEmptyNote != "" {
			eb.WriteString("\n")
			eb.WriteString(hintStyle.Render("  " + p.attentionEmptyNote))
		}
		eb.WriteString("\n")
		hint := "  F toggle view · Enter or Esc to dismiss"
		if p.reloadFunc != nil {
			hint += " · r to reload"
		}
		eb.WriteString(hintStyle.Render(hint))
		return eb.String()
	}

	// Header in left panel
	headerText := p.attentionPanes[p.attentionCursor].Session
	if p.attentionTitle != "" {
		headerText = p.attentionTitle
	}
	if p.attentionFollowing {
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
	pane := p.attentionPanes[p.attentionCursor]
	paneName := pane.Name
	pinSuffix := ""
	pinVisualWidth := 0
	if pane.Following {
		pinSuffix = " 📌"
		pinVisualWidth = 3 // space(1) + pin emoji(2 cells)
	}
	// Truncate name to fit, leaving room for pin
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
	previewLines := strings.Split(p.attentionPreview, "\n")
	// Trim trailing empty lines
	for len(previewLines) > 0 && strings.TrimSpace(previewLines[len(previewLines)-1]) == "" {
		previewLines = previewLines[:len(previewLines)-1]
	}

	// Visible attention panes
	start := p.attentionScroll
	if start > len(p.attentionPanes) {
		start = len(p.attentionPanes)
	}
	visible := listHeight
	if visible > len(p.attentionPanes)-start {
		visible = len(p.attentionPanes) - start
	}

	// For preview: show last lines that fit (push to bottom like main view)
	previewStart := 0
	if len(previewLines) > listHeight {
		previewStart = len(previewLines) - listHeight
	}

	// Empty lines to push list content to bottom
	emptyLines := listHeight - visible
	for i := 0; i < emptyLines; i++ {
		// Empty left panel + separator + preview line
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

	// Status icon styles
	attentionIconStyle := lipgloss.NewStyle().Foreground(colorAttention)
	workingIconStyle := lipgloss.NewStyle().Foreground(colorWorking)
	idleIconStyle := lipgloss.NewStyle().Foreground(colorIdle)

	// Render list rows alongside preview
	for i := 0; i < visible; i++ {
		listIdx := start + i
		previewIdx := previewStart + emptyLines + i

		// Left panel: pane list item with status icon
		var left string
		pane := p.attentionPanes[listIdx]

		// Status icon: 2 visual chars (icon + space), or 3 with pin (icon + pin + space)
		var icon string
		switch pane.Status {
		case AttentionWorking:
			icon = workingIconStyle.Render(spinnerFrames[p.spinnerFrame])
		case AttentionNeedsAttention:
			icon = attentionIconStyle.Render("●")
		case AttentionIdle:
			icon = idleIconStyle.Render("●")
		}
		iconWidth := 2 // icon + trailing space
		if pane.Following {
			icon += "📌"
			iconWidth = 4 // icon + pin(2) + trailing space
		}
		icon += " "

		// Account for icon width + prefix (1 for cursor pipe or 2 for spaces)
		nameWidth := leftWidth - iconWidth - 3 // 1 pipe + 1 space + icon + name (selected)
		name := truncateString(pane.Name, nameWidth)

		if listIdx == p.attentionCursor {
			padding := leftWidth - len([]rune(name)) - iconWidth - 3
			if padding < 0 {
				padding = 0
			}
			left = pipeStyle.Render("▌") + selectedStyle.Render(" "+icon+name+strings.Repeat(" ", padding))
		} else {
			padding := leftWidth - len([]rune(name)) - iconWidth - 2 // 2 spaces + icon
			if padding < 0 {
				padding = 0
			}
			left = "  " + icon + name + strings.Repeat(" ", padding)
		}

		// Right panel: preview content
		rightContent := ""
		if previewIdx < len(previewLines) {
			rightContent = truncateString(previewLines[previewIdx], rightWidth)
		}

		b.WriteString("\x1b[0m") // reset before left panel
		b.WriteString(left)
		b.WriteString(sepStyle.Render("│"))
		b.WriteString(rightContent)
		b.WriteString("\x1b[0m\n")
	}

	// Hints or note input
	if p.editingNote {
		b.WriteString("  " + p.noteInput.View())
	} else {
		var hints string
		if len(p.items) > 0 {
			hints = "  f view · ← back · Enter switch · C-a attention · C-f follow · C-r read · C-x unmonitor · Esc cancel"
		} else {
			hints = "  f view · Enter switch · C-a attention · C-f follow · C-r read · C-x unmonitor · Esc quit"
		}
		b.WriteString(hintStyle.Render(hints))
	}

	return b.String()
}

func truncateString(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	visibleWidth := 0
	inEscape := false
	lastSafe := 0
	for i, r := range s {
		if inEscape {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEscape = false
			}
			continue
		}
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if visibleWidth >= maxWidth {
			return s[:lastSafe]
		}
		visibleWidth++
		lastSafe = i + len(string(r))
	}
	return s
}

func (p *Picker) viewNormal() string {
	var b strings.Builder

	// Items
	visible := p.height
	if visible > len(p.filtered) {
		visible = len(p.filtered)
	}

	// Use stored scroll offset
	start := p.scroll

	// Add empty lines to push content to bottom
	emptyLines := p.height - visible
	for i := 0; i < emptyLines; i++ {
		b.WriteString("\n")
	}

	// Calculate max context length for alignment (only if showing context)
	maxContextLen := 0
	if p.showContext {
		for i := start; i < start+visible && i < len(p.filtered); i++ {
			if len(p.filtered[i].Context) > maxContextLen {
				maxContextLen = len(p.filtered[i].Context)
			}
		}
	}

	// Check if any item has an icon (not just visible ones, to keep stable layout)
	hasIcons := false
	for j := range p.items {
		if p.items[j].Icon != "" {
			hasIcons = true
			break
		}
	}

	for i := start; i < start+visible && i < len(p.filtered); i++ {
		item := p.filtered[i]

		// Build display line with optional context
		var line string
		if p.showContext && item.Context != "" {
			contextPadding := maxContextLen - len(item.Context)
			line = " [" + item.Context + "]" + strings.Repeat(" ", contextPadding) + " " + item.Name
		} else {
			line = " " + item.Name
		}

		// Prepend icon column when any item has an icon
		if hasIcons {
			if item.Icon != "" {
				line = " " + item.Icon + line
			} else {
				line = "  " + line
			}
		}

		prefixWidth := len(p.quickAccessPadding())
		distFromCursor := p.cursor - i
		hasNumber := p.quickAccessEnabled() && distFromCursor >= 1 && distFromCursor <= 9

		if i == p.cursor {
			// Selected: blue pipe + highlighted background
			// Pad to full width for consistent highlight
			if p.width > 0 {
				padding := p.width - len([]rune(line)) - prefixWidth
				if padding > 0 {
					line += strings.Repeat(" ", padding)
				}
			}
			b.WriteString(pipeStyle.Render("▌"))
			if hasNumber {
				b.WriteString(dimStyle.Render(fmt.Sprintf("%d ", distFromCursor)))
			} else {
				b.WriteString(strings.Repeat(" ", prefixWidth-1))
			}
			b.WriteString(selectedStyle.Render(line))
		} else {
			if hasNumber {
				b.WriteString(dimStyle.Render(p.quickAccessLabel(distFromCursor)))
			} else {
				b.WriteString(p.quickAccessPadding())
			}
			b.WriteString(line)
		}
		b.WriteString("\n")
	}

	writeInputBox(&b, p.width, p.input.View())

	// Warnings
	if len(p.warnings) > 0 {
		warnStyle := lipgloss.NewStyle().Foreground(colorWorking)
		for _, w := range p.warnings {
			b.WriteString(warnStyle.Render("  ⚠ " + w))
			b.WriteString("\n")
		}
	}

	// Hints line
	hints := p.buildHints()
	b.WriteString(hintStyle.Render(hints))

	return b.String()
}

// Result returns the picker result after running
func (p *Picker) Result() Result {
	p.result.CursorIndex = p.cursor
	p.result.AttentionFollowing = p.attentionFollowing
	return p.result
}

// RunAttention starts the picker directly in the attention sub-view.
// Returns the selected pane (ActionSwitchToPane) or cancel.
func RunAttention(title string, panes []AttentionPane, cb AttentionCallbacks, reloadFn func() []AttentionPane, opts ...PickerOption) (Result, error) {
	p := NewPicker(nil, append([]PickerOption{WithAttentionPanes(panes, cb), WithAttentionReload(reloadFn)}, opts...)...)
	p.attentionMode = true
	p.attentionTitle = title
	if p.attentionFollowing {
		p.rebuildAttentionView()
	}
	if len(p.attentionPanes) > 0 {
		p.attentionCursor = len(p.attentionPanes) - 1
	}
	p.adjustAttentionScroll()
	p.fetchAttentionPreview()
	program := tea.NewProgram(p)
	m, err := program.Run()
	if err != nil {
		return Result{Action: ActionCancel}, err
	}
	return m.(*Picker).Result(), nil
}

// Run starts the picker and returns the result
func Run(items []Item, opts ...PickerOption) (Result, error) {
	p := NewPicker(items, opts...)
	program := tea.NewProgram(p)
	m, err := program.Run()
	if err != nil {
		return Result{Action: ActionCancel}, err
	}
	return m.(*Picker).Result(), nil
}

// Key bindings
type keyMap struct {
	Up           key.Binding
	Down         key.Binding
	HalfPageUp   key.Binding
	HalfPageDown key.Binding
	Enter        key.Binding
	Quit         key.Binding
	Delete       key.Binding
	ForceDelete  key.Binding
	KillSession  key.Binding
	Reset        key.Binding
	OpenWindow   key.Binding
	ClearInput   key.Binding
	Help         key.Binding
	Attention        key.Binding
	MarkAttention    key.Binding
	FollowPane       key.Binding
	ToggleFollowView key.Binding
	Back             key.Binding
	Reload         key.Binding
	AttentionUp    key.Binding
	AttentionDown  key.Binding
	EditNote       key.Binding
}

var keys = keyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "ctrl+p"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "ctrl+n"),
	),
	HalfPageUp: key.NewBinding(
		key.WithKeys("ctrl+b"),
	),
	HalfPageDown: key.NewBinding(
		key.WithKeys("ctrl+f"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
	),
	Quit: key.NewBinding(
		key.WithKeys("esc", "ctrl+c"),
	),
	Delete: key.NewBinding(
		key.WithKeys("ctrl+d"),
	),
	ForceDelete: key.NewBinding(
		key.WithKeys("ctrl+x"),
	),
	KillSession: key.NewBinding(
		key.WithKeys("ctrl+k"),
	),
	Reset: key.NewBinding(
		key.WithKeys("ctrl+r"),
	),
	OpenWindow: key.NewBinding(
		key.WithKeys("ctrl+o"),
	),
	ClearInput: key.NewBinding(
		key.WithKeys("alt+backspace", "ctrl+u"),
	),
	Help: key.NewBinding(
		key.WithKeys("f1"),
	),
	Attention: key.NewBinding(
		key.WithKeys("right"),
	),
	MarkAttention: key.NewBinding(
		key.WithKeys("ctrl+a"),
	),
	FollowPane: key.NewBinding(
		key.WithKeys("ctrl+f"),
	),
	ToggleFollowView: key.NewBinding(
		key.WithKeys("f"),
	),
	Back: key.NewBinding(
		key.WithKeys("left"),
	),
	Reload: key.NewBinding(
		key.WithKeys("r"),
	),
	AttentionUp: key.NewBinding(
		key.WithKeys("up", "ctrl+p", "k"),
	),
	AttentionDown: key.NewBinding(
		key.WithKeys("down", "ctrl+n", "j"),
	),
	EditNote: key.NewBinding(
		key.WithKeys("N"),
	),
}

