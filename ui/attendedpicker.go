package ui

import (
	"fmt"
	"io"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/internal/tty"
)

// AttendedAgentEntry is one selectable attended entry: the command a pick
// returns, and the shared one-line render its host built for it.
type AttendedAgentEntry struct {
	// Cmd is the entry's complete command — what a pick hands back, and all a
	// host needs to launch or hold the choice.
	Cmd string
	// Label is the entry as the Attended entry render spells it.
	Label string
}

// AttendedAgentPicker is the list a human picks the attended agent from
// (ADR-0264). It is the surface behind `tab` on a gate menu's assist row: one
// level, one list, and a choice handed straight back.
//
// It holds no config and reaches no writer. The gate keeps a pick for its
// attended session without that behavior entering this component.
// It writes nothing to stdout on any path (ADR-0202 decision 11).
type AttendedAgentPicker struct {
	entries  []AttendedAgentEntry
	cursor   int
	width    int
	height   int
	showHelp bool

	choice *AttendedAgentEntry
	done   bool
}

// NewAttendedAgentPicker builds a picker over entries, highlighting the head —
// the entry in force, which is the one a human who opened the list by accident
// wants to leave with.
func NewAttendedAgentPicker(entries []AttendedAgentEntry) *AttendedAgentPicker {
	return &AttendedAgentPicker{entries: entries}
}

// Choice returns the picked entry, or nil when the picker exited unchanged.
func (p *AttendedAgentPicker) Choice() *AttendedAgentEntry { return p.choice }

// Done reports whether the picker has closed (picked or left unchanged).
func (p *AttendedAgentPicker) Done() bool { return p.done }

// Init implements tea.Model.
func (p *AttendedAgentPicker) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (p *AttendedAgentPicker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height
		return p, nil
	case tea.KeyPressMsg:
		if ToggleHelp(&p.showHelp, msg) {
			return p, nil
		}
		return p, p.handleKey(msg)
	}
	return p, nil
}

func (p *AttendedAgentPicker) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(msg, attendedPickerKeys.Up):
		p.moveCursor(-1)
		return nil
	case key.Matches(msg, attendedPickerKeys.Down):
		p.moveCursor(1)
		return nil
	case key.Matches(msg, attendedPickerKeys.Submit):
		return p.pick(p.cursor)
	case key.Matches(msg, attendedPickerKeys.Cancel):
		return p.exitUnchanged()
	}
	s := strings.TrimSpace(msg.String())
	if len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
		return p.pick(int(s[0]-'0') - 1)
	}
	return nil
}

func (p *AttendedAgentPicker) moveCursor(delta int) {
	n := len(p.entries)
	if n == 0 {
		return
	}
	p.cursor = (p.cursor + delta + n) % n
}

func (p *AttendedAgentPicker) pick(idx int) tea.Cmd {
	if idx < 0 || idx >= len(p.entries) {
		return nil
	}
	entry := p.entries[idx]
	p.choice = &entry
	p.cursor = idx
	p.done = true
	return tea.Quit
}

func (p *AttendedAgentPicker) exitUnchanged() tea.Cmd {
	p.choice = nil
	p.done = true
	return tea.Quit
}

// View implements tea.Model. AltScreen stays false for the same reason the gate
// menu's does: the drain log the gate was opened over stays readable above it.
func (p *AttendedAgentPicker) View() tea.View {
	content := p.viewHelp()
	if !p.showHelp {
		content = clampToPane(p.ViewContent(), p.height)
	}
	v := tea.NewView(content)
	v.AltScreen = false
	v.KeyboardEnhancements = tea.KeyboardEnhancements{}
	return v
}

func (p *AttendedAgentPicker) viewHelp() string {
	height := p.height
	if height <= 0 {
		height = 12
	}
	return RenderHelpOverlay("Help · Attended agent", p.helpEntries(), p.width, height)
}

func (p *AttendedAgentPicker) helpEntries() []HelpEntry {
	return []HelpEntry{
		{"1-9", "Pick that entry"},
		{"Enter", "Pick the highlighted entry"},
		{"↑/↓ j/k", "Move highlight"},
		{"Esc", "Leave the agent unchanged"},
		{"C-h", "Toggle this help"},
	}
}

// ViewContent renders the list. The interactive frame and any golden test share
// this one render.
func (p *AttendedAgentPicker) ViewContent() string {
	var b strings.Builder
	b.WriteString(headerStyle().Render("Attended agent"))
	b.WriteString("\n\n")
	if len(p.entries) == 0 {
		b.WriteString(hintStyle().Render("  (no usable entries)"))
		b.WriteString("\n")
	}
	for i, entry := range p.entries {
		prefix := "  "
		digit := " "
		if i < 9 {
			digit = fmt.Sprintf("%d", i+1)
		}
		line := fmt.Sprintf("%s. %s", digit, entry.Label)
		if i == p.cursor {
			prefix = IndicatorStyle().Render("▸ ")
			line = selectedGateItemStyle().Render(line)
		}
		b.WriteString(prefix)
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(hintStyle().Render("  1-9 pick · enter pick · ↑/↓ move · esc leave unchanged"))
	b.WriteString("\n")
	return b.String()
}

// RunAttendedAgentPicker runs the list as an inline (no altscreen) program over
// in/out and returns the picked entry, or nil when the human left it unchanged.
// Without a terminal there is nothing to pick with, so it returns nil having
// rendered nothing: a headless gate offers no chooser at all (ADR-0264
// decision 7), and printing the list where stdout may be a data channel is the
// one thing this component must never do.
func RunAttendedAgentPicker(entries []AttendedAgentEntry, in io.Reader, out io.Writer, warn func(string, ...any)) (*AttendedAgentEntry, error) {
	fd, ok := tty.TerminalFd(in)
	if !ok {
		return nil, nil
	}
	claimTerminal(fd, warn)
	final, err := RunProgram(NewAttendedAgentPicker(entries), in, out, tea.WithoutSignalHandler())
	if err != nil {
		return nil, err
	}
	fp, ok := final.(*AttendedAgentPicker)
	if !ok || fp == nil {
		return nil, fmt.Errorf("attended agent picker: unexpected model type %T", final)
	}
	return fp.Choice(), nil
}

var attendedPickerKeys = struct {
	Up     key.Binding
	Down   key.Binding
	Submit key.Binding
	Cancel key.Binding
}{
	Up:     key.NewBinding(key.WithKeys("up", "k", "ctrl+p")),
	Down:   key.NewBinding(key.WithKeys("down", "j", "ctrl+n")),
	Submit: key.NewBinding(key.WithKeys("enter")),
	Cancel: key.NewBinding(key.WithKeys("esc", "ctrl+c")),
}
