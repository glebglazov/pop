package ui

import (
	"unicode"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// promptGlyph is the house prompt glyph for single-line text entry (ADR-0081).
const promptGlyph = "❯ "

// TextField is pop's hand-rolled house single-line text entry (ADR-0081): a
// Model-shaped embeddable widget (not a standalone Program) holding a rune
// buffer, a block cursor, and the house prompt glyph "❯ ". It owns an
// emacs-style editing keymap (arrows, ctrl+b/ctrl+f, home/end incl.
// ctrl+a/ctrl+e, backspace, ctrl+u clear, and word-level editing: ctrl+w,
// alt+b/alt+f, alt+d, ctrl+k) as a default. A host may reserve only keys that
// produce no text — arrows, tab, pgup/pgdn, ctrl+n/ctrl+p, ctrl+c, and its own
// commit and cancel keys — and must forward every printable key here (ADR-0213);
// no host may keep a letter or digit for itself. It emits no
// tea.Cmd. It is the single house config point for text entry, retiring the old
// bubbles/v2/textinput wrapper.
type TextField struct {
	value   []rune // current edit buffer
	cursor  int    // insertion index into value, 0..len(value)
	focused bool
}

// NewTextField returns a focused, empty text field with the house prompt glyph.
func NewTextField() TextField {
	return TextField{focused: true}
}

// Value returns the current buffer contents.
func (m TextField) Value() string {
	return string(m.value)
}

// SetValue replaces the buffer, clamping the cursor into the new bounds.
func (m *TextField) SetValue(s string) {
	m.value = []rune(s)
	if m.cursor > len(m.value) {
		m.cursor = len(m.value)
	}
}

// Cursor returns the current insertion index (rune offset).
func (m TextField) Cursor() int {
	return m.cursor
}

// SetCursor moves the insertion point, clamping into buffer bounds.
func (m *TextField) SetCursor(pos int) {
	if pos < 0 {
		pos = 0
	}
	if pos > len(m.value) {
		pos = len(m.value)
	}
	m.cursor = pos
}

// Focus marks the field focused (its block cursor renders when focused).
func (m *TextField) Focus() {
	m.focused = true
}

// Update applies a single message to the buffer. Only key presses mutate state;
// any other message is ignored. It emits no tea.Cmd, so callers need not thread
// one back through their own Update.
func (m *TextField) Update(msg tea.Msg) {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return
	}

	switch {
	case key.Matches(keyMsg, textFieldKeys.Backspace):
		if m.cursor > 0 {
			m.value = append(m.value[:m.cursor-1], m.value[m.cursor:]...)
			m.cursor--
		}
		return

	case key.Matches(keyMsg, textFieldKeys.Left):
		if m.cursor > 0 {
			m.cursor--
		}
		return

	case key.Matches(keyMsg, textFieldKeys.Right):
		if m.cursor < len(m.value) {
			m.cursor++
		}
		return

	case key.Matches(keyMsg, textFieldKeys.Home):
		m.cursor = 0
		return

	case key.Matches(keyMsg, textFieldKeys.End):
		m.cursor = len(m.value)
		return

	case key.Matches(keyMsg, textFieldKeys.Clear):
		m.value = nil
		m.cursor = 0
		return

	case key.Matches(keyMsg, textFieldKeys.DeleteWordBack):
		start := prevWordStart(m.value, m.cursor)
		m.value = append(m.value[:start], m.value[m.cursor:]...)
		m.cursor = start
		return

	case key.Matches(keyMsg, textFieldKeys.WordLeft):
		m.cursor = prevWordStart(m.value, m.cursor)
		return

	case key.Matches(keyMsg, textFieldKeys.WordRight):
		m.cursor = nextWordEnd(m.value, m.cursor)
		return

	case key.Matches(keyMsg, textFieldKeys.DeleteWordForward):
		end := nextWordEnd(m.value, m.cursor)
		m.value = append(m.value[:m.cursor], m.value[end:]...)
		return

	case key.Matches(keyMsg, textFieldKeys.KillLine):
		m.value = m.value[:m.cursor]
		return
	}

	// Printable input: Text is non-empty only for keys that produce characters,
	// so this naturally ignores unhandled control keys.
	if keyMsg.Text != "" {
		runes := []rune(keyMsg.Text)
		m.value = append(m.value[:m.cursor], append(runes, m.value[m.cursor:]...)...)
		m.cursor += len(runes)
	}
}

// View renders the prompt glyph followed by the buffer. When focused, a
// reverse-video block cursor marks the insertion point.
func (m TextField) View() string {
	buffer := string(m.value)
	if m.focused {
		buffer = renderInputWithCursor(m.value, m.cursor)
	}
	return indicatorStyle().Render(promptGlyph) + buffer
}

// renderInputWithCursor draws the buffer with a reverse-video block over the
// rune at the cursor (or a trailing block when the cursor sits past the end).
func renderInputWithCursor(value []rune, cursor int) string {
	cursorStyle := dimStyle().Reverse(true)
	if cursor >= len(value) {
		return string(value) + cursorStyle.Render(" ")
	}
	before := string(value[:cursor])
	under := cursorStyle.Render(string(value[cursor]))
	after := string(value[cursor+1:])
	return before + under + after
}

// prevWordStart reports the index of the start of the word behind cursor: the
// spaces immediately behind the cursor are skipped first, then the run of
// non-space runes before them. At index 0 it returns 0, so the word motions
// stop at the buffer edge instead of wrapping.
func prevWordStart(value []rune, cursor int) int {
	i := cursor
	for i > 0 && unicode.IsSpace(value[i-1]) {
		i--
	}
	for i > 0 && !unicode.IsSpace(value[i-1]) {
		i--
	}
	return i
}

// nextWordEnd reports the index just past the word ahead of cursor, skipping
// any spaces under the cursor first. At the end of the buffer it returns the
// buffer length.
func nextWordEnd(value []rune, cursor int) int {
	i := cursor
	for i < len(value) && unicode.IsSpace(value[i]) {
		i++
	}
	for i < len(value) && !unicode.IsSpace(value[i]) {
		i++
	}
	return i
}

// textFieldKeyMap is the default emacs-style editing keymap owned by TextField.
type textFieldKeyMap struct {
	Backspace         key.Binding
	Left              key.Binding
	Right             key.Binding
	Home              key.Binding
	End               key.Binding
	Clear             key.Binding
	DeleteWordBack    key.Binding
	WordLeft          key.Binding
	WordRight         key.Binding
	DeleteWordForward key.Binding
	KillLine          key.Binding
}

var textFieldKeys = textFieldKeyMap{
	Backspace:         key.NewBinding(key.WithKeys("backspace")),
	Left:              key.NewBinding(key.WithKeys("left", "ctrl+b")),
	Right:             key.NewBinding(key.WithKeys("right", "ctrl+f")),
	Home:              key.NewBinding(key.WithKeys("home", "ctrl+a")),
	End:               key.NewBinding(key.WithKeys("end", "ctrl+e")),
	Clear:             key.NewBinding(key.WithKeys("ctrl+u", "alt+backspace")),
	DeleteWordBack:    key.NewBinding(key.WithKeys("ctrl+w")),
	WordLeft:          key.NewBinding(key.WithKeys("alt+b")),
	WordRight:         key.NewBinding(key.WithKeys("alt+f")),
	DeleteWordForward: key.NewBinding(key.WithKeys("alt+d")),
	KillLine:          key.NewBinding(key.WithKeys("ctrl+k")),
}
