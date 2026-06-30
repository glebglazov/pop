package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Frame is the shared screen-chrome owner for budgeted TUI list views. It
// pairs with List: List owns the body (rows, cursor, anchor); Frame owns
// everything around it and the body-height budget. A single declaration of
// which regions are present drives both BodyHeight and Render, so the
// reserved-line count can never drift from what's actually drawn.
type Frame struct {
	Width    int
	Notice   string   // "" = absent (rendered via renderUpdateNotice)
	Header   string   // "" = absent
	InputBox string   // "" = absent; content when present (e.g. input.View() or " Help")
	Warnings []string // reserved AND rendered; nil/empty = none
	Hints    string   // "" = absent
}

// BodyHeight returns the body row budget for a terminal of height termH: termH
// minus every present region (1 for Notice, 1 for Header, 3 for InputBox,
// len(Warnings) for warnings, 1 for Hints), floored at >= 3.
func (f Frame) BodyHeight(termH int) int {
	h := termH
	if f.Notice != "" {
		h--
	}
	if f.Header != "" {
		h--
	}
	if f.InputBox != "" {
		h -= 3
	}
	h -= len(f.Warnings)
	if f.Hints != "" {
		h--
	}
	if h < 3 {
		h = 3
	}
	return h
}

// Render composes the frame's regions around body in the fixed order notice
// -> header -> body -> input box -> warnings -> hints, omitting absent ones.
func (f Frame) Render(body string) string {
	parts := make([]string, 0, 6)

	if f.Notice != "" {
		parts = append(parts, renderUpdateNotice(f.Width, f.Notice))
	}
	if f.Header != "" {
		parts = append(parts, headerStyle.Render(f.Header))
	}

	parts = append(parts, body)

	if f.InputBox != "" {
		var ib strings.Builder
		writeInputBox(&ib, f.Width, f.InputBox)
		parts = append(parts, strings.TrimSuffix(ib.String(), "\n"))
	}

	if len(f.Warnings) > 0 {
		warnStyle := lipgloss.NewStyle().Foreground(colorWorking)
		lines := make([]string, len(f.Warnings))
		for i, w := range f.Warnings {
			lines[i] = warnStyle.Render("  ⚠ " + w)
		}
		parts = append(parts, strings.Join(lines, "\n"))
	}

	if f.Hints != "" {
		parts = append(parts, hintStyle.Render(f.Hints))
	}

	return strings.Join(parts, "\n")
}
