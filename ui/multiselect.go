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

// MultiSelect is a checkbox list: rows can be toggled on and off, with some
// rows locked (a status indicator, not a removable selection). Unlike the
// fuzzy Picker it returns many selections at once and has no filter input.
type MultiSelect struct {
	title   string
	items   []MultiSelectItem
	checked []bool
	cursor  int
	scroll  int
	height  int
	width   int
	result  MultiSelectResult
}

var multiSelectToggle = key.NewBinding(key.WithKeys("space"))

// NewMultiSelect builds a multi-select over the given rows. The cursor starts
// on the first unlocked row (or the first row if all are locked), and each
// unlocked row's initial checkbox state comes from its Checked field.
func NewMultiSelect(title string, items []MultiSelectItem) *MultiSelect {
	checked := make([]bool, len(items))
	cursor := 0
	cursorSet := false
	for i, it := range items {
		if !it.Locked {
			checked[i] = it.Checked
			if !cursorSet {
				cursor = i
				cursorSet = true
			}
		}
	}
	return &MultiSelect{
		title:   title,
		items:   items,
		checked: checked,
		cursor:  cursor,
		height:  10,
	}
}

func (m *MultiSelect) Init() tea.Cmd {
	m.adjustScroll()
	return nil
}

func (m *MultiSelect) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
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
			if len(m.items) > 0 {
				if m.cursor > 0 {
					m.cursor--
				} else {
					m.cursor = len(m.items) - 1
				}
				m.adjustScroll()
			}
			return m, nil

		case key.Matches(msg, keys.Down):
			if len(m.items) > 0 {
				if m.cursor < len(m.items)-1 {
					m.cursor++
				} else {
					m.cursor = 0
				}
				m.adjustScroll()
			}
			return m, nil
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height - 3 // reserve title (1) + blank (1) + hints (1)
		if m.height < 3 {
			m.height = 3
		}
		m.adjustScroll()
	}
	return m, nil
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

func (m *MultiSelect) adjustScroll() {
	m.scroll = adjustScroll(m.cursor, m.scroll, m.height, len(m.items), 0)
}

func (m *MultiSelect) View() tea.View {
	v := tea.NewView(m.view())
	v.AltScreen = true
	v.KeyboardEnhancements = tea.KeyboardEnhancements{}
	return v
}

func (m *MultiSelect) view() string {
	var b strings.Builder

	b.WriteString(headerStyle.Render(m.title))
	b.WriteString("\n\n")

	visible := m.height
	if visible > len(m.items) {
		visible = len(m.items)
	}
	for i := m.scroll; i < m.scroll+visible && i < len(m.items); i++ {
		it := m.items[i]

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

		if i == m.cursor {
			b.WriteString(indicatorStyle.Render("█") + " " + line)
		} else {
			b.WriteString("  " + line)
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(hintStyle.Render("  Space toggle · Enter confirm · Esc cancel"))

	return b.String()
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
