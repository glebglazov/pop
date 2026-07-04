package ui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// namePromptModel is the domain wrapper around the house TextField (ADR-0081):
// it owns the base hint, the empty→default fallback, and the PromptName Program,
// delegating its buffer and editing keymap to the shared field.
//
// It backs the worktree-name step (ADR-0076): the human types the new branch
// name into an empty field, or cancels with Esc. The prompt hints the base ref
// being forked from as `(base: <ref>)`; submitting an empty value falls back to
// the branch-derived default.
type namePromptModel struct {
	header       string
	defaultValue string
	base         string

	field TextField // shared house buffer + block cursor + editing keymap

	// submitted is true after Enter; cancelled is true after Esc/ctrl+c.
	submitted bool
	cancelled bool

	showHelp bool
	width    int
	height   int
}

func newNamePrompt(header, defaultValue, base string) *namePromptModel {
	return &namePromptModel{
		header:       header,
		defaultValue: defaultValue,
		base:         base,
		field:        NewTextField(),
	}
}

// result returns the chosen name. An empty buffer falls back to the default.
func (m *namePromptModel) result() string {
	if m.field.Value() == "" {
		return m.defaultValue
	}
	return m.field.Value()
}

func (m *namePromptModel) Init() tea.Cmd { return nil }

func (m *namePromptModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyPressMsg:
		// Help overlay: toggle, dismiss, or swallow keys while open.
		if ToggleHelp(&m.showHelp, msg) {
			return m, nil
		}

		// Intercept the domain's reserved keys first; the field handles the rest.
		switch {
		case key.Matches(msg, namePromptKeys.Cancel):
			m.cancelled = true
			return m, tea.Quit

		case key.Matches(msg, namePromptKeys.Submit):
			m.submitted = true
			return m, tea.Quit
		}

		m.field.Update(msg)
		return m, nil
	}

	return m, nil
}

func (m *namePromptModel) helpEntries() []HelpEntry {
	return []HelpEntry{
		{"Enter", "Confirm and submit"},
		{"Esc", "Cancel"},
		{"←/→ C-b/C-f", "Move cursor"},
		{"Backspace", "Delete character before cursor"},
		{"C-a", "Go to start of line"},
		{"C-e", "Go to end of line"},
		{"C-u", "Clear all text"},
	}
}

func (m *namePromptModel) viewHelp() string {
	height := m.height
	if height <= 0 {
		height = 10
	}
	return RenderHelpOverlay("Help · Name", m.helpEntries(), m.width, height)
}

func (m *namePromptModel) viewNormal() tea.View {
	var b strings.Builder

	b.WriteString(headerStyle.Render("  " + m.header))
	if m.base != "" {
		b.WriteString(hintStyle.Render("  (base: " + m.base + ")"))
	}
	b.WriteString("\n\n")

	// Render the edit buffer (prompt glyph + block cursor) via the shared field.
	b.WriteString("  ")
	b.WriteString(m.field.View())
	b.WriteString("\n\n")

	b.WriteString(hintStyle.Render("  enter confirm · esc cancel · C-h help"))

	v := tea.NewView(b.String())
	v.AltScreen = true
	v.KeyboardEnhancements = tea.KeyboardEnhancements{}
	return v
}

func (m *namePromptModel) View() tea.View {
	if m.showHelp {
		return tea.NewView(m.viewHelp())
	}
	return m.viewNormal()
}

type namePromptKeyMap struct {
	Submit key.Binding
	Cancel key.Binding
}

var namePromptKeys = namePromptKeyMap{
	Submit: key.NewBinding(key.WithKeys("enter")),
	Cancel: key.NewBinding(key.WithKeys("esc", "ctrl+c")),
}

// PromptName shows a single-line editable prompt with an empty field, hinting
// the base ref as `(base: <base>)`. It returns the chosen name and
// confirmed=true on Enter (an empty buffer falls back to defaultValue), or
// confirmed=false when the human cancels with Esc.
func PromptName(header, defaultValue, base string) (name string, confirmed bool, err error) {
	m := newNamePrompt(header, defaultValue, base)
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
