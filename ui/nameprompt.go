package ui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// namePromptModel is a hand-rolled single-line text input on raw bubbletea.
// pop imports zero charmbracelet/bubbles input components (ADR-0076); this
// mirrors the picker's house style rather than pulling in bubbles/textinput.
//
// It backs the worktree-name step (slice 02): pre-filled with the branch-derived
// default, the human accepts it, edits it, or cancels with Esc. Submitting an
// empty value falls back to the default.
type namePromptModel struct {
	header       string
	defaultValue string

	value  []rune // current edit buffer
	cursor int     // insertion index into value, 0..len(value)

	// submitted is true after Enter; cancelled is true after Esc/ctrl+c.
	submitted bool
	cancelled bool
}

func newNamePrompt(header, defaultValue string) *namePromptModel {
	v := []rune(defaultValue)
	return &namePromptModel{
		header:       header,
		defaultValue: defaultValue,
		value:        v,
		cursor:       len(v),
	}
}

// result returns the chosen name. An empty buffer falls back to the default.
func (m *namePromptModel) result() string {
	if len(m.value) == 0 {
		return m.defaultValue
	}
	return string(m.value)
}

func (m *namePromptModel) Init() tea.Cmd { return nil }

func (m *namePromptModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}

	switch {
	case key.Matches(keyMsg, namePromptKeys.Cancel):
		m.cancelled = true
		return m, tea.Quit

	case key.Matches(keyMsg, namePromptKeys.Submit):
		m.submitted = true
		return m, tea.Quit

	case key.Matches(keyMsg, namePromptKeys.Backspace):
		if m.cursor > 0 {
			m.value = append(m.value[:m.cursor-1], m.value[m.cursor:]...)
			m.cursor--
		}
		return m, nil

	case key.Matches(keyMsg, namePromptKeys.Left):
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil

	case key.Matches(keyMsg, namePromptKeys.Right):
		if m.cursor < len(m.value) {
			m.cursor++
		}
		return m, nil

	case key.Matches(keyMsg, namePromptKeys.Home):
		m.cursor = 0
		return m, nil

	case key.Matches(keyMsg, namePromptKeys.End):
		m.cursor = len(m.value)
		return m, nil

	case key.Matches(keyMsg, namePromptKeys.Clear):
		m.value = nil
		m.cursor = 0
		return m, nil
	}

	// Printable input: Text is non-empty only for keys that produce characters,
	// so this naturally ignores unhandled control keys.
	if keyMsg.Text != "" {
		runes := []rune(keyMsg.Text)
		m.value = append(m.value[:m.cursor], append(runes, m.value[m.cursor:]...)...)
		m.cursor += len(runes)
	}
	return m, nil
}

func (m *namePromptModel) View() tea.View {
	var b strings.Builder

	b.WriteString(headerStyle.Render("  " + m.header))
	b.WriteString("\n\n")

	// Render the edit buffer with a block cursor at the insertion point.
	b.WriteString("  ")
	b.WriteString(indicatorStyle.Render("❯ "))
	b.WriteString(renderInputWithCursor(m.value, m.cursor))
	b.WriteString("\n\n")

	b.WriteString(hintStyle.Render("  enter confirm · esc cancel"))

	v := tea.NewView(b.String())
	v.AltScreen = true
	v.KeyboardEnhancements = tea.KeyboardEnhancements{}
	return v
}

// renderInputWithCursor draws the buffer with a reverse-video block over the
// rune at the cursor (or a trailing block when the cursor sits past the end).
func renderInputWithCursor(value []rune, cursor int) string {
	cursorStyle := dimStyle.Reverse(true)
	if cursor >= len(value) {
		return string(value) + cursorStyle.Render(" ")
	}
	before := string(value[:cursor])
	under := cursorStyle.Render(string(value[cursor]))
	after := string(value[cursor+1:])
	return before + under + after
}

type namePromptKeyMap struct {
	Submit    key.Binding
	Cancel    key.Binding
	Backspace key.Binding
	Left      key.Binding
	Right     key.Binding
	Home      key.Binding
	End       key.Binding
	Clear     key.Binding
}

var namePromptKeys = namePromptKeyMap{
	Submit:    key.NewBinding(key.WithKeys("enter")),
	Cancel:    key.NewBinding(key.WithKeys("esc", "ctrl+c")),
	Backspace: key.NewBinding(key.WithKeys("backspace")),
	Left:      key.NewBinding(key.WithKeys("left", "ctrl+b")),
	Right:     key.NewBinding(key.WithKeys("right", "ctrl+f")),
	Home:      key.NewBinding(key.WithKeys("home", "ctrl+a")),
	End:       key.NewBinding(key.WithKeys("end", "ctrl+e")),
	Clear:     key.NewBinding(key.WithKeys("ctrl+u", "alt+backspace")),
}

// PromptName shows a single-line editable prompt pre-filled with defaultValue.
// It returns the chosen name and confirmed=true on Enter (an empty buffer falls
// back to defaultValue), or confirmed=false when the human cancels with Esc.
func PromptName(header, defaultValue string) (name string, confirmed bool, err error) {
	m := newNamePrompt(header, defaultValue)
	final, err := tea.NewProgram(m).Run()
	if err != nil {
		return "", false, err
	}
	fm := final.(*namePromptModel)
	if fm.cancelled || !fm.submitted {
		return "", false, nil
	}
	return fm.result(), true, nil
}
