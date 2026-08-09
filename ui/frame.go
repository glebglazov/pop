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
	// Subheader is a standing one-liner under Header (e.g. the agent currently
	// in force). Unlike Status it persists across refreshes; unlike Footnote it
	// sits above the body so the choice is visible before the rows.
	Subheader string // "" = absent
	InputBox  string   // "" = absent; content when present (e.g. input.View() or " Help")
	Warnings  []string // reserved AND rendered; nil/empty = none
	Status    string   // "" = absent; transient action feedback, distinct from Warnings
	// Footnote is a dim standing one-liner about the environment the view runs
	// in, sitting between Status and Hints. Unlike Status it is derived from the
	// snapshot rather than from a keypress, so it persists across refreshes;
	// unlike Warnings it reports a condition the operator need not act on.
	Footnote string // "" = absent
	// Block is a multi-line region reserved and rendered between Footnote and
	// Hints. Unlike Warnings it is drawn verbatim — no amber prefix — so the
	// caller styles its own lines. Used by the Work dashboard's filter menu
	// (ADR-0197). nil/empty = absent.
	Block []string
	Hints string // "" = absent
}

// BodyHeight returns the body row budget for a terminal of height termH: termH
// minus every present region (1 for Notice, 1 for Header, 1 for Subheader, 3 for
// InputBox, len(Warnings) for warnings, 1 for Status, 1 for Footnote,
// len(Block) for Block, 1 for Hints), floored at >= 3.
func (f Frame) BodyHeight(termH int) int {
	h := termH
	if f.Notice != "" {
		h--
	}
	if f.Header != "" {
		h--
	}
	if f.Subheader != "" {
		h--
	}
	if f.InputBox != "" {
		h -= 3
	}
	h -= len(f.Warnings)
	if f.Status != "" {
		h--
	}
	if f.Footnote != "" {
		h--
	}
	h -= len(f.Block)
	if f.Hints != "" {
		h--
	}
	if h < 3 {
		h = 3
	}
	return h
}

// Render composes the frame's regions around body in the fixed order notice
// -> header -> subheader -> body -> input box -> warnings -> status -> footnote
// -> block -> hints, omitting absent ones. When TermH is known, a short body is
// padded to the full BodyHeight budget so trailing regions sit at the bottom of
// the screen.
func (f Frame) Render(body string) string {
	if f.TermH > 0 {
		body = f.padBody(body)
	}

	parts := make([]string, 0, 10)

	if f.Notice != "" {
		parts = append(parts, renderUpdateNotice(f.Width, f.Notice))
	}
	if f.Header != "" {
		parts = append(parts, headerStyle.Render(f.Header))
	}
	if f.Subheader != "" {
		parts = append(parts, hintStyle.Render(f.Subheader))
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

	if f.Footnote != "" {
		parts = append(parts, hintStyle.Render("  "+f.Footnote))
	}

	if len(f.Block) > 0 {
		parts = append(parts, strings.Join(f.Block, "\n"))
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
