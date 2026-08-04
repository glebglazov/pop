package ui

import (
	"sort"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/debug"
	"github.com/junegunn/fzf/src/algo"
	"github.com/junegunn/fzf/src/util"
)

// IconAttention is the icon used to mark items that have panes needing attention.
const IconAttention = "!"

// Item represents a selectable item in the picker
type Item struct {
	Name        string // Display name
	Path        string // Full path (returned on selection)
	Context     string // Additional context (e.g., branch name)
	Icon        string // Optional icon displayed to the left of name
	Marker      string // Optional leading marker, independent of Icon (e.g. Unbound managed worktree)
	SessionName string // Pre-computed tmux session name

	// Depth is how deep the row sits in a nested list: 0 for a top-level row, 1
	// for a child. Display only — the whole row shifts right, glyph columns
	// included, because indenting the name alone leaves the nesting nearly
	// invisible. Nothing about the row's identity changes with it.
	Depth int
	// Disclosure is the glyph trailing the name of a row that holds children —
	// the colourless "there is more here" signal. The caller supplies both forms
	// (collapsed and expanded); an empty string renders nothing.
	Disclosure string
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
	CursorIndex        int                       // cursor position at time of action
	UserDefinedCommand *UserDefinedCommandResult // set when Action == ActionUserDefinedCommand
}

// Action represents what action the user wants to take
type Action int

const (
	ActionConfirm Action = iota
	ActionCancel
	ActionDelete
	ActionForceDelete
	ActionKillSession
	ActionReset
	ActionOpenWindow
	ActionUserDefinedCommand
	ActionRefresh
	ActionYankPath
	ActionCreateWorktree
	ActionCreateManagedWorktree
	ActionSetPreferredWorkbench
)

// Picker is a fuzzy-searchable list picker
type Picker struct {
	items    []Item
	filtered []Item
	input    TextField
	list     *List[Item]
	cursor   int // synced from list; kept for test access
	scroll   int // synced from list; kept for test access
	height   int
	width    int
	result   Result

	showHelp           bool
	showDelete         bool
	showContext        bool
	showKillSession    bool
	showReset          bool
	showOpenWindow     bool
	showCreateWorktree bool
	showSetPreferred   bool
	cursorAtEnd        bool

	quickAccessModifier string
	quickAccess         *QuickAccess

	// Cursor memory: remembers selected item path per filter query
	cursorMemory map[string]string
	lastQuery    string

	tree *Tree

	customCommands   []UserDefinedKeyBinding
	iconLegend       []iconLegendEntry
	initialCursorIdx int
	warnings         []string
	updateNotice     string
	header           string
}

// iconLegendEntry maps an icon to its description in the help view
type iconLegendEntry struct {
	icon string
	desc string
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

// WithCreateWorktree enables the create-worktree keybindings: ctrl+a for an
// ordinary worktree, ctrl+t for a pop-managed worktree ahead of any Task set
// (ADR-0152)
func WithCreateWorktree() PickerOption {
	return func(p *Picker) {
		p.showCreateWorktree = true
	}
}

// WithSetPreferredWorkbench enables the set-preferred-workbench keybinding
// (ctrl+w). It is the feature flag gating the Workbench-preference picker
// surface (ADR-0078); both the project picker and the worktree dashboard opt in.
func WithSetPreferredWorkbench() PickerOption {
	return func(p *Picker) {
		p.showSetPreferred = true
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

// WithUpdateNotice sets the dimmed top-right Update notice text. Empty text
// shows nothing. The notice occupies a reserved top line so it never shifts
// the list, input box, or hints.
func WithUpdateNotice(text string) PickerOption {
	return func(p *Picker) {
		p.updateNotice = text
	}
}

// Tree wires nested-list gestures into the picker. The picker owns no tree of its
// own: it reads a row's place from Item.Depth — a row is expanded when the row
// below it sits deeper — and asks the caller for rows whenever the shape has to
// change. So the arrangement rules (what nests under what, what a query lists)
// stay with the caller that knows the domain, and the picker contributes only the
// keys.
type Tree struct {
	// Rows returns the rows to list for a query. An empty query gets the tree as
	// the caller currently has it expanded; a non-empty query gets whatever the
	// caller wants searched — for the project list, the flat universe, which is how
	// rows that nesting folds away stay reachable.
	Rows func(query string) []Item
	// SetExpanded records a row's new expansion state. The picker re-lists through
	// Rows immediately afterwards, so this only has to remember the bit.
	SetExpanded func(path string, expand bool)
}

// WithTree turns the left/right arrows into tree gestures while the query is
// empty, and hands re-listing to the caller. Without it the arrows are the query
// textfield's cursor keys, as they have always been.
func WithTree(t Tree) PickerOption {
	return func(p *Picker) {
		if t.Rows == nil || t.SetExpanded == nil {
			return
		}
		p.tree = &t
	}
}

// WithHeader sets a caption rendered above the list (e.g. "Pick a workbench").
// Empty text shows nothing. The header occupies a reserved top line so it never
// shifts the list, input box, or hints.
func WithHeader(text string) PickerOption {
	return func(p *Picker) {
		p.header = text
	}
}

// NewPicker creates a new picker with the given items
func NewPicker(items []Item, opts ...PickerOption) *Picker {
	p := &Picker{
		items:            items,
		filtered:         items,
		input:            NewTextField(),
		height:           10,
		cursorMemory:     make(map[string]string),
		initialCursorIdx: -1,
	}

	for _, opt := range opts {
		opt(p)
	}

	p.quickAccess = p.newQuickAccess()
	scrollMargin := 0
	if p.quickAccess.Enabled() {
		scrollMargin = 9
	}

	p.list = NewList(items, Opts[Item]{
		Key:          func(it Item) string { return it.Path },
		Wrap:         true,
		Anchor:       AnchorBottom,
		ScrollMargin: scrollMargin,
		QuickLabel:   p.quickAccess.LabelFunc(),
	})
	p.list.opts.Cell = p.pickerCell

	return p
}

func (p *Picker) newQuickAccess() *QuickAccess {
	modifier := p.quickAccessModifier
	if modifier == "" {
		modifier = "disabled"
	}
	return NewQuickAccess(modifier)
}

func (p *Picker) syncFromList() {
	p.cursor = p.list.Cursor()
	p.scroll = p.list.Scroll()
}

func (p *Picker) syncToList() {
	if p.cursor != p.list.Cursor() {
		p.list.SetCursor(p.cursor)
	}
}

func (p *Picker) selectedItem() (*Item, bool) {
	item, ok := p.list.Selected()
	if !ok {
		return nil, false
	}
	return &item, true
}

func (p *Picker) Init() tea.Cmd {
	if p.initialCursorIdx >= 0 && len(p.filtered) > 0 {
		p.list.SetCursor(p.initialCursorIdx)
	} else if p.cursorAtEnd && len(p.filtered) > 0 {
		p.list.SetCursor(len(p.filtered) - 1)
	}
	p.syncFromList()
	return nil
}

func (p *Picker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	p.syncToList()

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		// Help overlay: toggle, dismiss, or swallow keys while open.
		if ToggleHelp(&p.showHelp, msg) {
			return p, nil
		}

		switch {
		case key.Matches(msg, keys.Quit):
			p.result = Result{Action: ActionCancel}
			return p, tea.Quit

		case key.Matches(msg, keys.Enter):
			if item, ok := p.selectedItem(); ok {
				p.result = Result{
					Selected: item,
					Action:   ActionConfirm,
				}
			}
			return p, tea.Quit

		case key.Matches(msg, keys.Up):
			p.list.MoveUp()
			p.syncFromList()
			return p, nil

		case key.Matches(msg, keys.Down):
			p.list.MoveDown()
			p.syncFromList()
			return p, nil

		case key.Matches(msg, keys.HalfPageUp):
			p.list.HalfPageUp()
			p.syncFromList()
			return p, nil

		case key.Matches(msg, keys.HalfPageDown):
			p.list.HalfPageDown()
			p.syncFromList()
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
			if item, ok := p.selectedItem(); ok {
				p.result.Selected = item
			}
			return p, tea.Quit

		case key.Matches(msg, keys.Delete):
			if p.showDelete {
				if item, ok := p.selectedItem(); ok {
					p.result = Result{
						Selected: item,
						Action:   ActionDelete,
					}
					return p, tea.Quit
				}
			}

		case key.Matches(msg, keys.ForceDelete):
			if p.showDelete {
				if item, ok := p.selectedItem(); ok {
					p.result = Result{
						Selected: item,
						Action:   ActionForceDelete,
					}
					return p, tea.Quit
				}
			}

		case key.Matches(msg, keys.KillSession):
			if p.showKillSession {
				if item, ok := p.selectedItem(); ok {
					p.result = Result{
						Selected: item,
						Action:   ActionKillSession,
					}
					return p, tea.Quit
				}
			}

		case key.Matches(msg, keys.Reset):
			if p.showReset {
				if item, ok := p.selectedItem(); ok {
					p.result = Result{
						Selected: item,
						Action:   ActionReset,
					}
					return p, tea.Quit
				}
			}

		case key.Matches(msg, keys.OpenWindow):
			if p.showOpenWindow {
				if item, ok := p.selectedItem(); ok {
					p.result = Result{
						Selected: item,
						Action:   ActionOpenWindow,
					}
					return p, tea.Quit
				}
			}

		case key.Matches(msg, keys.CreateWorktree):
			if p.showCreateWorktree {
				p.result = Result{Action: ActionCreateWorktree}
				if item, ok := p.selectedItem(); ok {
					p.result.Selected = item
				}
				return p, tea.Quit
			}

		case key.Matches(msg, keys.CreateManagedWorktree):
			if p.showCreateWorktree {
				p.result = Result{Action: ActionCreateManagedWorktree}
				if item, ok := p.selectedItem(); ok {
					p.result.Selected = item
				}
				return p, tea.Quit
			}

		case key.Matches(msg, keys.SetPreferred):
			if p.showSetPreferred {
				if item, ok := p.selectedItem(); ok {
					p.result = Result{
						Selected: item,
						Action:   ActionSetPreferredWorkbench,
					}
					return p, tea.Quit
				}
			}

		case key.Matches(msg, keys.YankPath):
			if item, ok := p.selectedItem(); ok {
				p.result = Result{
					Selected: item,
					Action:   ActionYankPath,
				}
				return p, tea.Quit
			}

		// The tree gestures sit below the paging keys on purpose: ctrl+b and
		// ctrl+f are matched above and never reach here, so paging works the same
		// in both states.
		case p.treeActive() && key.Matches(msg, keys.TreeExpand):
			p.expandRow()
			return p, nil

		case p.treeActive() && key.Matches(msg, keys.TreeCollapse):
			p.collapseRow()
			return p, nil

		case key.Matches(msg, keys.ClearInput):
			p.input.SetValue("")
			p.filter()
			return p, nil

		case p.isQuickAccessKey(msg):
			n := p.quickAccessDigit(msg)
			targetIdx := p.list.Cursor() - n
			if targetIdx >= 0 && targetIdx < len(p.filtered) {
				p.result = Result{
					Selected: &p.filtered[targetIdx],
					Action:   ActionConfirm,
				}
				return p, tea.Quit
			}
			return p, nil

		}

	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = p.frameSpec().BodyHeight(msg.Height)
		p.list.Resize(p.height)
		p.syncFromList()
	}

	// Update text input
	p.input.Update(msg)

	// Filter items
	p.filter()

	return p, nil
}

// treeActive reports whether the arrows drive the tree rather than the query. A
// typed query flattens the list, and editing a query must never be hijacked by
// the tree — which is what makes the two behaviours coexist with no mode to
// learn.
func (p *Picker) treeActive() bool {
	return p.tree != nil && p.input.Value() == ""
}

// rowExpanded reads expansion off the rendered list itself: a row is open exactly
// when the row under it sits deeper. There is no second copy of the state to
// disagree with what is on screen.
func (p *Picker) rowExpanded(idx int) bool {
	if idx < 0 || idx+1 >= len(p.filtered) {
		return false
	}
	return p.filtered[idx+1].Depth > p.filtered[idx].Depth
}

// expandRow opens the row under the cursor, or walks into its first child when it
// is already open. Opening and descending are the same gesture, so a second press
// carries on inward instead of doing nothing.
func (p *Picker) expandRow() {
	idx := p.list.Cursor()
	if idx < 0 || idx >= len(p.filtered) {
		return
	}
	if p.rowExpanded(idx) {
		p.list.MoveDown()
		p.syncFromList()
		return
	}
	// Disclosure is the caller's own "this row holds children" mark; a row without
	// one has nothing to open.
	if p.filtered[idx].Disclosure == "" {
		return
	}
	path := p.filtered[idx].Path
	p.setExpanded(path, true, path)
	p.jumpToLastChild(path)
}

// jumpToLastChild lands the cursor on the bottom of the group just opened, which
// is what pulls the whole group into view: the list follows its cursor, so asking
// for the last child asks for every row above it too. The margin is suppressed
// for the jump — honouring it would settle the last child nine rows above the
// bottom line and scroll past rows the operator just asked to see. A group taller
// than the viewport pushes its parent off the top; `left` collapses and lands
// back on it, so the way back is one key.
func (p *Picker) jumpToLastChild(parentPath string) {
	idx := -1
	for i, it := range p.filtered {
		if it.Path == parentPath {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	last := idx
	for i := idx + 1; i < len(p.filtered) && p.filtered[i].Depth > p.filtered[idx].Depth; i++ {
		last = i
	}
	p.list.JumpTo(last)
	p.syncFromList()
}

// collapseRow closes the row under the cursor, or — from a child — closes the
// parent and lands the cursor on it, so the operator never watches the row under
// the cursor vanish out from under them.
func (p *Picker) collapseRow() {
	idx := p.list.Cursor()
	if idx < 0 || idx >= len(p.filtered) {
		return
	}
	row := p.filtered[idx]
	if p.rowExpanded(idx) {
		p.collapseAt(idx)
		return
	}
	if row.Depth == 0 {
		return
	}
	for i := idx - 1; i >= 0; i-- {
		if p.filtered[i].Depth < row.Depth {
			p.collapseAt(i)
			return
		}
	}
}

// collapseAt closes the group at parentIdx, reversing the expand literally: every
// row below the group keeps the screen line it was on, which puts the parent
// where its last visible child sat. The offset is read now rather than remembered
// from the expand, so moving around inside an open group cannot make the collapse
// jump somewhere stale. When the collapsed list no longer fills the viewport the
// clamp in SetScroll wins and the bottom anchor pads above — no row is invented
// to hold a line that no longer exists.
func (p *Picker) collapseAt(parentIdx int) {
	before := len(p.filtered)
	scroll := p.list.Scroll()
	path := p.filtered[parentIdx].Path
	p.setExpanded(path, false, path)
	p.list.SetScroll(scroll - (before - len(p.filtered)))
	p.syncFromList()
}

// setExpanded records the change, re-lists through the caller, and keeps the
// cursor on cursorPath — the row the gesture was about, which after a collapse is
// the parent rather than the child that is now hidden.
func (p *Picker) setExpanded(path string, expand bool, cursorPath string) {
	p.tree.SetExpanded(path, expand)
	// The query is empty whenever this runs (treeActive gates it), so the listed
	// rows are the tree itself with no filtering in between.
	p.items = p.tree.Rows("")
	p.filtered = p.items
	p.list.SetItems(p.filtered)
	if !p.list.SetCursorToKey(cursorPath) && len(p.filtered) > 0 {
		p.list.SetCursor(min(p.list.Cursor(), len(p.filtered)-1))
	}
	p.syncFromList()
}

// fzfMatch holds an item with its fuzzy match score
type fzfMatch struct {
	item  Item
	score int
}

func (p *Picker) filter() {
	query := p.input.Value()
	queryChanged := query != p.lastQuery

	// Save current selection before changing filter
	if queryChanged && len(p.filtered) > 0 && p.cursor < len(p.filtered) {
		path := p.filtered[p.cursor].Path
		p.cursorMemory[p.lastQuery] = path
		debug.Log("filter: query %q -> %q, saving cursor for %q: path=%q", p.lastQuery, query, p.lastQuery, path)
	}

	// A nested list searches a different row set than it browses: the first
	// keystroke of a query asks the caller for the flat universe, and clearing the
	// query asks for the tree back. Re-listing only on the transition keeps the
	// no-tree picker's filter path untouched.
	if p.tree != nil && queryChanged && (query == "") != (p.lastQuery == "") {
		p.items = p.tree.Rows(query)
	}

	// Build filtered list
	if query == "" {
		p.filtered = p.items
	} else {
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

	p.list.SetItems(p.filtered)

	if queryChanged {
		if path, ok := p.cursorMemory[query]; ok {
			debug.Log("filter: restoring cursor for %q: path=%q", query, path)
			if !p.list.SetCursorToKey(path) {
				p.list.SetCursor(len(p.filtered) - 1)
			}
		} else if len(p.filtered) > 0 {
			p.list.SetCursor(len(p.filtered) - 1)
			debug.Log("filter: first time query %q, cursor at bottom (%d), %d items", query, p.list.Cursor(), len(p.filtered))
		}
	}

	p.lastQuery = query
	p.syncFromList()
}

// buildHints returns the hints string based on enabled features
func (p *Picker) buildHints() string {
	return "  Enter open · Esc quit · C-h help"
}

// frameSpec builds the Frame describing the picker's screen chrome: the
// update notice, header, input box, warnings, and hints.
func (p *Picker) frameSpec() Frame {
	header := p.header
	if header != "" {
		header = "  " + header
	}
	return Frame{
		Width:    p.width,
		Notice:   p.updateNotice,
		Header:   header,
		InputBox: p.input.View(),
		Warnings: p.warnings,
		Hints:    p.buildHints(),
	}
}

// formatKeyHint converts a key binding to a display-friendly hint format
func formatKeyHint(b key.Binding) string {
	keys := b.Keys()
	if len(keys) == 0 {
		return ""
	}
	k := keys[0]
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

func pickerKeyPress(msg tea.KeyPressMsg) KeyPress {
	return KeyPress{
		Code: msg.Code,
		Alt:  msg.Mod.Contains(tea.ModAlt),
		Ctrl: msg.Mod.Contains(tea.ModCtrl),
	}
}

func (p *Picker) isQuickAccessKey(msg tea.KeyPressMsg) bool {
	return p.quickAccess.Digit(pickerKeyPress(msg)) >= 1
}

func (p *Picker) quickAccessDigit(msg tea.KeyPressMsg) int {
	return p.quickAccess.Digit(pickerKeyPress(msg))
}

func (p *Picker) pickerHasIcons() bool {
	for j := range p.items {
		if p.items[j].Icon != "" {
			return true
		}
	}
	return false
}

func (p *Picker) pickerHasMarkers() bool {
	for j := range p.items {
		if p.items[j].Marker != "" {
			return true
		}
	}
	return false
}

func (p *Picker) pickerMaxContextLen() int {
	if !p.showContext {
		return 0
	}
	maxContextLen := 0
	for _, item := range p.filtered {
		if len(item.Context) > maxContextLen {
			maxContextLen = len(item.Context)
		}
	}
	return maxContextLen
}

func (p *Picker) pickerCell(item Item, _ RowState) string {
	maxContextLen := p.pickerMaxContextLen()
	hasIcons := p.pickerHasIcons()
	hasMarkers := p.pickerHasMarkers()

	name := item.Name
	if item.Disclosure != "" {
		name += " " + item.Disclosure
	}

	var line string
	if p.showContext && item.Context != "" {
		contextPadding := maxContextLen - len(item.Context)
		line = " [" + item.Context + "]" + strings.Repeat(" ", contextPadding) + " " + name
	} else {
		line = " " + name
	}

	if hasIcons {
		if item.Icon != "" {
			line = " " + item.Icon + line
		} else {
			line = "  " + line
		}
	}

	// Marker is a separate column from Icon: session/attention state and
	// managed-binding state are independent facts that can both apply to the
	// same row, so one must not overwrite the other.
	if hasMarkers {
		if item.Marker != "" {
			line = " " + item.Marker + line
		} else {
			line = "  " + line
		}
	}

	// The indent goes outside every column, so a child row's glyph moves with its
	// name. A depth-0 row is byte-identical to a list that never nests.
	return strings.Repeat("  ", item.Depth) + line
}

func (p *Picker) View() tea.View {
	var content string
	if p.showHelp {
		content = p.viewHelp()
	} else {
		content = p.viewProject()
	}
	v := tea.NewView(content)
	v.AltScreen = true
	v.KeyboardEnhancements = tea.KeyboardEnhancements{}
	return v
}

func (p *Picker) helpEntries() []HelpEntry {
	entries := []HelpEntry{
		{"↑/↓ C-p/C-n", "Navigate"},
		{"C-b/C-f", "Page up / down"},
		{"C-u", "Clear filter"},
	}
	if p.tree != nil {
		entries = append(entries, HelpEntry{"→/←", "Expand / collapse (empty filter)"})
	}
	entries = append(entries,
		HelpEntry{"Enter", "Select"},
		HelpEntry{"Esc", "Quit"},
	)

	if p.showKillSession && !p.isKeyOverridden("ctrl+k") {
		entries = append(entries, HelpEntry{"C-k", "Kill tmux session"})
	}
	if p.showReset && !p.isKeyOverridden("ctrl+r") {
		entries = append(entries, HelpEntry{"C-r", "Reset history"})
	}
	if p.showOpenWindow && !p.isKeyOverridden("ctrl+o") {
		entries = append(entries, HelpEntry{"C-o", "Open in window"})
	}
	if p.showCreateWorktree && !p.isKeyOverridden("ctrl+a") {
		entries = append(entries, HelpEntry{"C-a", "Create worktree"})
	}
	if p.showCreateWorktree && !p.isKeyOverridden("ctrl+t") {
		entries = append(entries, HelpEntry{"C-t", "Create managed worktree"})
	}
	if p.showSetPreferred && !p.isKeyOverridden("ctrl+w") {
		entries = append(entries, HelpEntry{"C-w", "Set preferred workbench"})
	}
	if p.showDelete && !p.isKeyOverridden("ctrl+d") {
		entries = append(entries, HelpEntry{"C-d", "Delete"})
	}
	if !p.isKeyOverridden("ctrl+y") {
		entries = append(entries, HelpEntry{"C-y", "Yank path to pane"})
	}
	if p.showDelete && !p.isKeyOverridden("ctrl+x") {
		entries = append(entries, HelpEntry{"C-x", "Force delete"})
	}
	switch p.quickAccessModifier {
	case "alt":
		entries = append(entries, HelpEntry{"A-1..9", "Quick select"})
	case "ctrl":
		entries = append(entries, HelpEntry{"C-1..9", "Quick select"})
	}

	for _, cc := range p.customCommands {
		entries = append(entries, HelpEntry{formatKeyHint(cc.Binding), cc.Label})
	}

	iconsSeen := make(map[string]bool)
	for _, item := range p.items {
		if item.Icon != "" {
			iconsSeen[item.Icon] = true
		}
		if item.Marker != "" {
			iconsSeen[item.Marker] = true
		}
	}
	if len(iconsSeen) > 0 {
		entries = append(entries, HelpEntry{"", ""})
		for _, legend := range p.iconLegend {
			if iconsSeen[legend.icon] {
				entries = append(entries, HelpEntry{legend.icon, legend.desc})
			}
		}
	}

	return entries
}

func (p *Picker) viewHelp() string {
	return RenderHelpOverlay("Help", p.helpEntries(), p.width, p.height)
}

func (p *Picker) viewProject() string {
	return p.frameSpec().Render(strings.Join(p.list.VisibleRows(), "\n"))
}

// Result returns the picker result after running
func (p *Picker) Result() Result {
	p.result.CursorIndex = p.list.Cursor()
	return p.result
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
	Up                    key.Binding
	Down                  key.Binding
	HalfPageUp            key.Binding
	HalfPageDown          key.Binding
	Enter                 key.Binding
	Quit                  key.Binding
	Delete                key.Binding
	ForceDelete           key.Binding
	KillSession           key.Binding
	Reset                 key.Binding
	OpenWindow            key.Binding
	ClearInput            key.Binding
	YankPath              key.Binding
	CreateWorktree        key.Binding
	CreateManagedWorktree key.Binding
	SetPreferred          key.Binding
	TreeExpand            key.Binding
	TreeCollapse          key.Binding
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
	YankPath: key.NewBinding(
		key.WithKeys("ctrl+y"),
	),
	CreateWorktree: key.NewBinding(
		key.WithKeys("ctrl+a"),
	),
	CreateManagedWorktree: key.NewBinding(
		key.WithKeys("ctrl+t"),
	),
	SetPreferred: key.NewBinding(
		key.WithKeys("ctrl+w"),
	),
	// Bare arrows only: ctrl+f and ctrl+b stay paging, and the emacs cursor keys
	// the textfield owns keep their meaning when a query is typed.
	TreeExpand: key.NewBinding(
		key.WithKeys("right"),
	),
	TreeCollapse: key.NewBinding(
		key.WithKeys("left"),
	),
}
