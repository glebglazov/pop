package ui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// HelpKeys is the shared help overlay toggle binding.
var HelpKeys = key.NewBinding(
	key.WithKeys("ctrl+h"),
)

// helpCloseKeys is the shared dismiss binding for help overlays.
var helpCloseKeys = key.NewBinding(
	key.WithKeys("esc"),
)

// HelpEntry is a single row in the help overlay.
type HelpEntry struct {
	Key  string
	Desc string
}

// ToggleHelp updates showHelp in response to the shared help/dismiss keys.
// It returns true when the key was consumed (help was toggled, dismissed, or
// swallowed while the overlay is open).
func ToggleHelp(showHelp *bool, msg tea.KeyPressMsg) bool {
	if *showHelp {
		if key.Matches(msg, HelpKeys) || key.Matches(msg, helpCloseKeys) {
			*showHelp = false
		}
		return true
	}
	if key.Matches(msg, HelpKeys) {
		*showHelp = true
		return true
	}
	return false
}

// RenderHelpOverlay renders a help overlay with aligned key/description
// columns, a bottom input-box chrome showing title, and the standard footer
// hint "C-h toggle · Esc close".
func RenderHelpOverlay(title string, entries []HelpEntry, width, height int) string {
	var b strings.Builder

	maxKeyWidth := 0
	for _, e := range entries {
		if w := lipgloss.Width(e.Key); w > maxKeyWidth {
			maxKeyWidth = w
		}
	}

	var lines []string
	for _, e := range entries {
		padding := maxKeyWidth - lipgloss.Width(e.Key)
		lines = append(lines, "  "+e.Key+strings.Repeat(" ", padding)+"   "+e.Desc)
	}

	emptyLines := height - len(lines)
	if emptyLines < 0 {
		emptyLines = 0
	}
	for i := 0; i < emptyLines; i++ {
		b.WriteString("\n")
	}
	for _, line := range lines {
		b.WriteString(line)
		b.WriteString("\n")
	}

	writeInputBox(&b, width, " "+title)
	b.WriteString(hintStyle.Render("  C-h toggle · Esc close"))

	return b.String()
}
