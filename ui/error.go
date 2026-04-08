package ui

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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
	// avoid touching the real tmux / /dev/tty. Defaults to copyToClipboard.
	copyFunc func(string) error
}

var (
	errorTitleStyle = lipgloss.NewStyle().
			Foreground(colorAttention).
			Bold(true)

	errorMessageStyle = lipgloss.NewStyle().
				Foreground(colorSelectedFg)

	errorTraceStyle = lipgloss.NewStyle().
			Foreground(colorDim)

	errorCopiedStyle = lipgloss.NewStyle().
				Foreground(colorAccent)

	errorCopyFailedStyle = lipgloss.NewStyle().
				Foreground(colorWorking)
)

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
				copy = copyToClipboard
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

	title := errorTitleStyle.Render("  ✗ Error")
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
		b.WriteString(errorTitleStyle.Render("  Stack trace"))
		b.WriteString("\n\n")
		for _, line := range strings.Split(strings.TrimRight(m.trace, "\n"), "\n") {
			b.WriteString("  ")
			b.WriteString(errorTraceStyle.Render(line))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")

	// Status line: copied / copy failed
	switch {
	case m.copied:
		b.WriteString(errorCopiedStyle.Render("  ✓ Copied to clipboard"))
		b.WriteString("\n")
	case m.copyErrMsg != "":
		b.WriteString(errorCopyFailedStyle.Render("  ⚠ Copy failed: " + m.copyErrMsg))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(hintStyle.Render("  c copy · any other key dismiss"))

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
	program := tea.NewProgram(m)
	if _, runErr := program.Run(); runErr != nil {
		// Fall back to plain stderr if the TUI can't run (no tty, etc).
		fmt.Fprintf(os.Stderr, "error: %s\n", err.Error())
		if trace != "" {
			fmt.Fprintln(os.Stderr, trace)
		}
	}
}

// copyToClipboard copies text to the system clipboard.
// Prefers `tmux load-buffer` when inside tmux, falls back to OSC 52 otherwise.
func copyToClipboard(text string) error {
	if os.Getenv("TMUX") != "" {
		cmd := exec.Command("tmux", "load-buffer", "-w", "-")
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err == nil {
			return nil
		}
		// Fall through to OSC 52 if tmux load-buffer failed.
	}
	return osc52Copy(text)
}

// osc52Copy writes an OSC 52 escape sequence to /dev/tty, which most modern
// terminal emulators honor to update the system clipboard.
func osc52Copy(text string) error {
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	seq := "\x1b]52;c;" + encoded + "\x07"

	// Write to /dev/tty so the sequence reaches the terminal even if stderr/stdout
	// have been redirected.
	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		// Fall back to stderr as a last resort.
		if _, werr := os.Stderr.WriteString(seq); werr != nil {
			return werr
		}
		return nil
	}
	defer tty.Close()
	if _, err := tty.WriteString(seq); err != nil {
		return err
	}
	return nil
}
