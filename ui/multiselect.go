package ui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// MultiSelectItem is one row in a MultiSelect list.
type MultiSelectItem struct {
	Label      string // display text for the row
	Locked     bool   // row cannot be toggled; shown with LockedMark instead of a checkbox
	LockedMark string // status glyph rendered in place of a checkbox for locked rows (e.g. "✓")
	Checked    bool   // initial checked state; ignored when Locked
}

// MultiSelectResult is the outcome of a MultiSelect run.
type MultiSelectResult struct {
	// Confirmed is true when the user pressed Enter; false means cancelled
	// (Esc / Ctrl-C), in which case Checked is empty.
	Confirmed bool
	// Checked holds the indices of checked, unlocked rows in list order.
	Checked []int
}

// msRow pairs a MultiSelectItem with its original index for checkbox state.
type msRow struct {
	item  MultiSelectItem
	index int
}

// MultiSelect is a checkbox list: rows can be toggled on and off, with some
// rows locked (a status indicator, not a removable selection). Unlike the
// fuzzy Picker it returns many selections at once and has no filter input.
type MultiSelect struct {
	title   string
	items   []MultiSelectItem
	checked []bool
	list    *List[msRow]
	cursor  int
	width   int
	height  int
	result  MultiSelectResult

	showHelp bool
}

var multiSelectToggle = key.NewBinding(key.WithKeys("space"))

func multiSelectCell(m *MultiSelect) func(msRow, RowState) string {
	return func(row msRow, _ RowState) string {
		it := row.item
		i := row.index

		var box string
		if it.Locked {
			mark := it.LockedMark
			if mark == "" {
				mark = " "
			}
			box = dimStyle.Render(mark)
		} else if m.checked[i] {
			box = "[x]"
		} else {
			box = "[ ]"
		}

		line := box + " " + it.Label
		if it.Locked {
			line = box + " " + dimStyle.Render(it.Label)
		}
		return line
	}
}

// NewMultiSelect builds a multi-select over the given rows. The cursor starts
// on the first unlocked row (or the first row if all are locked), and each
// unlocked row's initial checkbox state comes from its Checked field.
func NewMultiSelect(title string, items []MultiSelectItem) *MultiSelect {
	checked := make([]bool, len(items))
	rows := make([]msRow, len(items))
	cursor := 0
	cursorSet := false
	for i, it := range items {
		rows[i] = msRow{item: it, index: i}
		if !it.Locked {
			checked[i] = it.Checked
			if !cursorSet {
				cursor = i
				cursorSet = true
			}
		}
	}

	m := &MultiSelect{
		title:   title,
		items:   items,
		checked: checked,
		list: NewList(rows, Opts[msRow]{
			Key:          func(r msRow) string { return r.item.Label },
			Cell:         nil, // set below after m exists
			Wrap:         true,
			Anchor:       AnchorTop,
			ScrollMargin: 0,
		}),
		cursor: cursor,
	}
	m.list.opts.Cell = multiSelectCell(m)
	m.list.SetCursor(cursor)
	return m
}

func (m *MultiSelect) Init() tea.Cmd {
	return nil
}

func (m *MultiSelect) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.list.Resize(m.frameSpec().BodyHeight(msg.Height))

	case tea.KeyPressMsg:
		// Help overlay: toggle, dismiss, or swallow keys while open.
		if ToggleHelp(&m.showHelp, msg) {
			return m, nil
		}

		switch {
		case key.Matches(msg, keys.Quit):
			m.result = MultiSelectResult{Confirmed: false}
			return m, tea.Quit

		case key.Matches(msg, keys.Enter):
			m.result = MultiSelectResult{Confirmed: true, Checked: m.checkedIndices()}
			return m, tea.Quit

		case key.Matches(msg, multiSelectToggle):
			if m.cursor >= 0 && m.cursor < len(m.items) && !m.items[m.cursor].Locked {
				m.checked[m.cursor] = !m.checked[m.cursor]
			}
			return m, nil

		case key.Matches(msg, keys.Up):
			m.list.MoveUp()
			m.cursor = m.list.Cursor()
			return m, nil

		case key.Matches(msg, keys.Down):
			m.list.MoveDown()
			m.cursor = m.list.Cursor()
			return m, nil
		}
	}
	return m, nil
}

func (m *MultiSelect) helpEntries() []HelpEntry {
	return []HelpEntry{
		{"Space", "Toggle selection"},
		{"Enter", "Confirm selections"},
		{"↑/↓", "Navigate"},
		{"Esc", "Cancel"},
	}
}

func (m *MultiSelect) viewHelp() string {
	height := m.height
	if height <= 0 {
		height = 10
	}
	return RenderHelpOverlay("Help · Select", m.helpEntries(), m.width, height)
}

// frameSpec builds the Frame describing MultiSelect's screen chrome: a
// header (the title) and a static hint line, with no notice, input box, or
// warnings.
func (m *MultiSelect) frameSpec() Frame {
	return Frame{
		Width:  m.width,
		Header: m.title,
		Hints:  "  Space toggle · Enter confirm · Esc cancel · C-h help",
	}
}

// checkedIndices returns the indices of checked, unlocked rows in list order.
func (m *MultiSelect) checkedIndices() []int {
	var out []int
	for i := range m.items {
		if m.checked[i] && !m.items[i].Locked {
			out = append(out, i)
		}
	}
	return out
}

func (m *MultiSelect) View() tea.View {
	var content string
	if m.showHelp {
		content = m.viewHelp()
	} else {
		content = m.view()
	}
	v := tea.NewView(content)
	v.AltScreen = true
	v.KeyboardEnhancements = tea.KeyboardEnhancements{}
	return v
}

func (m *MultiSelect) view() string {
	return m.frameSpec().Render(strings.Join(m.list.VisibleRows(), "\n"))
}

// Result returns the outcome after the program exits.
func (m *MultiSelect) Result() MultiSelectResult {
	return m.result
}

// RunMultiSelect runs an interactive multi-select over items and returns the
// result. Cancelling (Esc/Ctrl-C) yields a non-confirmed, empty result.
func RunMultiSelect(title string, items []MultiSelectItem) (MultiSelectResult, error) {
	m := NewMultiSelect(title, items)
	program := tea.NewProgram(m)
	out, err := program.Run()
	if err != nil {
		return MultiSelectResult{Confirmed: false}, err
	}
	return out.(*MultiSelect).Result(), nil
}
