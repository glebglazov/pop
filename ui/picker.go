package ui

import (
	"sort"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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
	SessionName string // Pre-computed tmux session name
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
)

// Picker is a fuzzy-searchable list picker
type Picker struct {
	items    []Item
	filtered []Item
	input    textinput.Model
	list     *List[Item]
	cursor   int // synced from list; kept for test access
	scroll   int // synced from list; kept for test access
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

	quickAccessModifier string
	quickAccess         *QuickAccess

	// Cursor memory: remembers selected item path per filter query
	cursorMemory map[string]string
	lastQuery    string

	customCommands []UserDefinedKeyBinding
	iconLegend     []iconLegendEntry
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
	ti := newTextInput()

	p := &Picker{
		items:            items,
		filtered:         items,
		input:            ti,
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

		case key.Matches(msg, keys.YankPath):
			if item, ok := p.selectedItem(); ok {
				p.result = Result{
					Selected: item,
					Action:   ActionYankPath,
				}
				return p, tea.Quit
			}

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
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)

	// Filter items
	p.filter()

	return p, cmd
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
	return "  Enter open · Esc quit · F1 help"
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

	var line string
	if p.showContext && item.Context != "" {
		contextPadding := maxContextLen - len(item.Context)
		line = " [" + item.Context + "]" + strings.Repeat(" ", contextPadding) + " " + item.Name
	} else {
		line = " " + item.Name
	}

	if hasIcons {
		if item.Icon != "" {
			line = " " + item.Icon + line
		} else {
			line = "  " + line
		}
	}

	return line
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
	entries = append(entries, helpEntry{"C-y", "Yank path to pane"})
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

	iconsSeen := make(map[string]bool)
	for _, item := range p.items {
		if item.Icon != "" {
			iconsSeen[item.Icon] = true
		}
	}
	if len(iconsSeen) > 0 {
		entries = append(entries, helpEntry{"", ""})
		for _, legend := range p.iconLegend {
			if iconsSeen[legend.icon] {
				entries = append(entries, helpEntry{legend.icon, legend.desc})
			}
		}
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

	emptyLines := p.height - len(helpLines)
	for i := 0; i < emptyLines; i++ {
		b.WriteString("\n")
	}

	for _, line := range helpLines {
		b.WriteString(line)
		b.WriteString("\n")
	}

	writeInputBox(&b, p.width, " Help")
	b.WriteString(hintStyle.Render("  Esc back"))

	return b.String()
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
	Up                key.Binding
	Down              key.Binding
	HalfPageUp        key.Binding
	HalfPageDown      key.Binding
	Enter             key.Binding
	Quit              key.Binding
	Delete            key.Binding
	ForceDelete       key.Binding
	KillSession       key.Binding
	Reset             key.Binding
	OpenWindow        key.Binding
	ClearInput        key.Binding
	Help              key.Binding
	YankPath          key.Binding
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
	YankPath: key.NewBinding(
		key.WithKeys("ctrl+y"),
	),
}
