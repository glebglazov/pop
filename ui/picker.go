package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/junegunn/fzf/src/algo"
	"github.com/junegunn/fzf/src/util"
)

// Item represents a selectable item in the picker
type Item struct {
	Name    string // Display name
	Path    string // Full path (returned on selection)
	Context string // Additional context (e.g., branch name)
}

func (i Item) FilterValue() string {
	return i.Name
}

// Result holds the picker result
type Result struct {
	Selected *Item
	Action   Action
}

// Action represents what action the user wants to take
type Action int

const (
	ActionSelect Action = iota
	ActionCancel
	ActionDelete
	ActionForceDelete
	ActionNew
	ActionKillSession
	ActionReset
)

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

	showDelete      bool
	showNew         bool
	showContext     bool
	showKillSession bool
	showReset       bool
	cursorAtEnd     bool

	// Cursor memory: remembers selected item per filter query
	cursorMemory map[string]cursorState // filter query -> cursor state
	lastQuery    string                 // previous filter query (to detect changes)
}

// cursorState stores cursor position info for a filter query
type cursorState struct {
	path      string // selected item's path
	screenPos int    // cursor position relative to visible area (0 = top of visible)
}

// PickerOption configures the picker
type PickerOption func(*Picker)

// WithDelete enables delete keybindings
func WithDelete() PickerOption {
	return func(p *Picker) {
		p.showDelete = true
	}
}

// WithNew enables new item keybinding
func WithNew() PickerOption {
	return func(p *Picker) {
		p.showNew = true
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

// WithCursorAtEnd starts the cursor at the last item
func WithCursorAtEnd() PickerOption {
	return func(p *Picker) {
		p.cursorAtEnd = true
	}
}

// NewPicker creates a new picker with the given items
func NewPicker(items []Item, opts ...PickerOption) *Picker {
	ti := textinput.New()
	ti.Prompt = "> "
	ti.Cursor.SetMode(cursor.CursorStatic)
	ti.Cursor.Style = lipgloss.NewStyle().Background(lipgloss.Color("white")).Foreground(lipgloss.Color("black"))
	ti.Cursor.TextStyle = lipgloss.NewStyle().Background(lipgloss.Color("white")).Foreground(lipgloss.Color("black"))
	ti.Focus()

	p := &Picker{
		items:        items,
		filtered:     items,
		input:        ti,
		height:       10,
		cursorMemory: make(map[string]cursorState),
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

func (p *Picker) Init() tea.Cmd {
	if p.cursorAtEnd && len(p.filtered) > 0 {
		p.cursor = len(p.filtered) - 1
	}
	p.adjustScroll()
	return nil
}

func (p *Picker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
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

		case key.Matches(msg, keys.New):
			if p.showNew {
				p.result = Result{Action: ActionNew}
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

		case key.Matches(msg, keys.ClearInput):
			p.input.SetValue("")
			p.filter()
			return p, nil
		}

	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height - 4 // Reserve space for hints (1 line) + input box (3 lines)
		if p.height < 3 {
			p.height = 3
		}
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

	// Save current selection and screen position before changing filter
	if queryChanged && len(p.filtered) > 0 && p.cursor < len(p.filtered) {
		p.cursorMemory[p.lastQuery] = cursorState{
			path:      p.filtered[p.cursor].Path,
			screenPos: p.cursor - p.scroll,
		}
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
			chars := util.ToChars([]byte(item.Name))
			result, _ := algo.FuzzyMatchV2(false, true, true, &chars, pattern, false, slab)
			if result.Score > 0 {
				matches = append(matches, fzfMatch{item: item, score: result.Score})
			}
		}

		// Sort by score (ascending, so best match ends up at the bottom)
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
			p.cursor = p.findItemIndex(state.path)
			// Restore relative screen position
			p.scroll = p.cursor - state.screenPos
		} else {
			// First time seeing this query: cursor at best match (bottom)
			p.cursor = len(p.filtered) - 1
			p.scroll = 0 // will be adjusted below
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
	return -1
}

// buildHints returns the hints string based on enabled features
func (p *Picker) buildHints() string {
	var hints []string

	hints = append(hints, "↑/↓ navigate", "C-b/C-f page", "C-u clear", "Enter select", "Esc quit")

	if p.showKillSession {
		hints = append(hints, "C-k kill session")
	}
	if p.showReset {
		hints = append(hints, "C-r reset")
	}
	if p.showDelete {
		hints = append(hints, "⌫ delete")
	}
	if p.showNew {
		hints = append(hints, "C-n new")
	}

	return "  " + strings.Join(hints, " · ")
}

// adjustScroll ensures the cursor is visible by adjusting scroll offset only when necessary
func (p *Picker) adjustScroll() {
	visible := p.height
	if visible > len(p.filtered) {
		visible = len(p.filtered)
	}
	if visible == 0 {
		p.scroll = 0
		return
	}

	// If cursor is above visible area, scroll up
	if p.cursor < p.scroll {
		p.scroll = p.cursor
	}
	// If cursor is below visible area, scroll down
	if p.cursor >= p.scroll+visible {
		p.scroll = p.cursor - visible + 1
	}
	// Ensure scroll doesn't go negative or too far
	if p.scroll < 0 {
		p.scroll = 0
	}
	maxScroll := len(p.filtered) - visible
	if maxScroll < 0 {
		maxScroll = 0
	}
	if p.scroll > maxScroll {
		p.scroll = maxScroll
	}
}

func (p *Picker) View() string {
	var b strings.Builder

	// Styles
	selectedStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("237")).
		Foreground(lipgloss.Color("255"))
	pipeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("39")) // Blue

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

		if i == p.cursor {
			// Selected: blue pipe + highlighted background
			// Pad to full width for consistent highlight
			if p.width > 0 {
				padding := p.width - len([]rune(line)) - 2
				if padding > 0 {
					line += strings.Repeat(" ", padding)
				}
			}
			b.WriteString(pipeStyle.Render("▌ "))
			b.WriteString(selectedStyle.Render(line))
		} else {
			b.WriteString("  ")
			b.WriteString(line)
		}
		b.WriteString("\n")
	}

	// Input box
	boxWidth := p.width
	if boxWidth < 20 {
		boxWidth = 40
	}
	innerWidth := boxWidth - 2

	b.WriteString("┌")
	b.WriteString(strings.Repeat("─", innerWidth))
	b.WriteString("┐\n")

	inputView := p.input.View()
	padding := innerWidth - lipgloss.Width(inputView)
	if padding < 0 {
		padding = 0
	}
	b.WriteString("│")
	b.WriteString(inputView)
	b.WriteString(strings.Repeat(" ", padding))
	b.WriteString("│\n")

	b.WriteString("└")
	b.WriteString(strings.Repeat("─", innerWidth))
	b.WriteString("┘\n")

	// Hints line
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	hints := p.buildHints()
	b.WriteString(hintStyle.Render(hints))

	return b.String()
}

// Result returns the picker result after running
func (p *Picker) Result() Result {
	return p.result
}

// Run starts the picker and returns the result
func Run(items []Item, opts ...PickerOption) (Result, error) {
	p := NewPicker(items, opts...)
	program := tea.NewProgram(p, tea.WithAltScreen())
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
	New          key.Binding
	KillSession  key.Binding
	Reset        key.Binding
	ClearInput   key.Binding
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
		key.WithKeys("backspace", "delete"),
	),
	ForceDelete: key.NewBinding(
		key.WithKeys("ctrl+x"),
	),
	New: key.NewBinding(
		key.WithKeys("ctrl+n"),
	),
	KillSession: key.NewBinding(
		key.WithKeys("ctrl+k"),
	),
	Reset: key.NewBinding(
		key.WithKeys("ctrl+r"),
	),
	ClearInput: key.NewBinding(
		key.WithKeys("alt+backspace", "ctrl+u"),
	),
}

// Confirm shows a simple yes/no confirmation
func Confirm(prompt string) (bool, error) {
	fmt.Printf("%s [y/N]: ", prompt)
	var response string
	fmt.Scanln(&response)
	return strings.ToLower(response) == "y", nil
}
