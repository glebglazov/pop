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
	TermH    int      // terminal height; 0 = unknown, disables bottom-anchor padding
	Notice   string   // "" = absent (rendered via renderUpdateNotice)
	Header   string   // "" = absent
	InputBox string   // "" = absent; content when present (e.g. input.View() or " Help")
	Warnings []string // reserved AND rendered; nil/empty = none
	Status   string   // "" = absent; transient action feedback, distinct from Warnings
	Hints    string   // "" = absent
}

// BodyHeight returns the body row budget for a terminal of height termH: termH
// minus every present region (1 for Notice, 1 for Header, 3 for InputBox,
// len(Warnings) for warnings, 1 for Status, 1 for Hints), floored at >= 3.
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
	if f.Status != "" {
		h--
	}
	if f.Hints != "" {
		h--
	}
	if h < 3 {
		h = 3
	}
	return h
}

// Render composes the frame's regions around body in the fixed order notice
// -> header -> body -> input box -> warnings -> status -> hints, omitting
// absent ones. When TermH is known, a short body is padded to the full
// BodyHeight budget so trailing regions sit at the bottom of the screen.
func (f Frame) Render(body string) string {
	if f.TermH > 0 {
		body = f.padBody(body)
	}

	parts := make([]string, 0, 7)

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

	if f.Status != "" {
		statusStyle := lipgloss.NewStyle().Foreground(colorAccent)
		parts = append(parts, statusStyle.Render("  "+f.Status))
	}

	if f.Hints != "" {
		parts = append(parts, hintStyle.Render(f.Hints))
	}

	return strings.Join(parts, "\n")
}

// padBody appends blank lines so body occupies the full BodyHeight budget,
// pushing trailing regions to the bottom of the screen. A body that already
// fills or overfills the budget is returned unchanged (byte-identical).
func (f Frame) padBody(body string) string {
	budget := f.BodyHeight(f.TermH)
	lines := strings.Count(body, "\n") + 1
	if lines >= budget {
		return body
	}
	return body + strings.Repeat("\n", budget-lines)
}
