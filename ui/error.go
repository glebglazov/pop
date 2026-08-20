package ui

import (
	"fmt"
	"os"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/glebglazov/pop/internal/clipboard"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
)

// errorModel is the Bubbletea model for the dedicated error screen.
type errorModel struct {
	message string
	trace   string
	width   int
	height  int

	copied     bool   // true after a successful copy
	copyErrMsg string // non-empty if the last copy attempt failed

	// copyFunc performs the actual clipboard write. Injected so tests can
	// avoid touching the real tmux / /dev/tty. Defaults to CopyToClipboard.
	copyFunc func(string) error
}

var errorMessageStyle = lipgloss.NewStyle()

func errorTitleStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(colorAttention()).Bold(true)
}

func errorTraceStyle() lipgloss.Style { return lipgloss.NewStyle().Foreground(colorDim()) }

func errorCopiedStyle() lipgloss.Style { return lipgloss.NewStyle().Foreground(colorAccent()) }

func errorCopyFailedStyle() lipgloss.Style { return lipgloss.NewStyle().Foreground(colorWorking()) }

func (m *errorModel) Init() tea.Cmd {
	return nil
}

func (m *errorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, errorCopyKey):
			copy := m.copyFunc
			if copy == nil {
				copy = CopyToClipboard
			}
			if err := copy(m.clipboardPayload()); err != nil {
				m.copied = false
				m.copyErrMsg = err.Error()
			} else {
				m.copied = true
				m.copyErrMsg = ""
			}
			return m, nil
		default:
			// Any other key dismisses the error screen
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *errorModel) View() tea.View {
	var b strings.Builder

	title := errorTitleStyle().Render("  ✗ Error")
	b.WriteString(title)
	b.WriteString("\n\n")

	// Error message, indented
	for _, line := range strings.Split(m.message, "\n") {
		b.WriteString("  ")
		b.WriteString(errorMessageStyle.Render(line))
		b.WriteString("\n")
	}

	if m.trace != "" {
		b.WriteString("\n")
		b.WriteString(errorTitleStyle().Render("  Stack trace"))
		b.WriteString("\n\n")
		for _, line := range strings.Split(strings.TrimRight(m.trace, "\n"), "\n") {
			b.WriteString("  ")
			b.WriteString(errorTraceStyle().Render(line))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")

	// Status line: copied / copy failed
	switch {
	case m.copied:
		b.WriteString(errorCopiedStyle().Render("  ✓ Copied to clipboard"))
		b.WriteString("\n")
	case m.copyErrMsg != "":
		b.WriteString(errorCopyFailedStyle().Render("  ⚠ Copy failed: " + m.copyErrMsg))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(hintStyle().Render("  c copy · any other key dismiss"))

	v := tea.NewView(b.String())
	v.AltScreen = true
	v.KeyboardEnhancements = tea.KeyboardEnhancements{}
	return v
}

// clipboardPayload returns the full text to copy (error message plus stack trace, if any).
func (m *errorModel) clipboardPayload() string {
	if m.trace == "" {
		return m.message
	}
	return m.message + "\n\n" + m.trace
}

var errorCopyKey = key.NewBinding(key.WithKeys("c"))

// ShowError displays a dedicated error screen and blocks until the user dismisses it.
// If trace is non-empty, it is shown below the error message and included in the copy payload.
// This is safe to call after a Bubbletea program has already exited.
func ShowError(err error, trace string) {
	if err == nil {
		return
	}
	m := &errorModel{
		message: err.Error(),
		trace:   trace,
	}
	if _, runErr := RunProgram(m, nil, nil); runErr != nil {
		// Fall back to plain stderr if the TUI can't run (no tty, etc).
		fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
		if trace != "" {
			fmt.Fprintln(os.Stderr, trace)
		}
	}
}

// CopyToClipboard copies text to the system clipboard.
// Prefers `tmux load-buffer` when inside tmux, falls back to OSC 52 otherwise.
func CopyToClipboard(text string) error {
	return clipboard.Copy(text)
}

// CopyToClipboardWith is CopyToClipboard with an injectable tmux module
// handle, so tests can assert against the tmuxtest fake instead of a real
// tmux server.
func CopyToClipboardWith(mod tmuxmod.Tmux, text string) error {
	return clipboard.CopyWith(mod, text)
}
