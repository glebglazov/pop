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
	Width  int
	TermH  int    // terminal height; 0 = unknown, disables bottom-anchor padding
	Notice string // "" = absent (rendered via renderUpdateNotice)
	Header string // "" = absent
	// Subheader is a standing one-liner under Header (e.g. the agent currently
	// in force). Unlike a Flash it persists across refreshes; unlike Footnote it
	// sits above the body so the choice is visible before the rows.
	Subheader string   // "" = absent
	InputBox  string   // "" = absent; content when present (e.g. input.View() or " Help")
	Warnings  []string // reserved AND rendered; nil/empty = none
	// Footnote is a dim standing one-liner about the environment the view runs
	// in, sitting between the warnings and the bottom line. Unlike a Flash it is
	// derived from the snapshot rather than from a keypress, so it persists
	// across refreshes; unlike Warnings it reports a condition the operator need
	// not act on.
	Footnote string // "" = absent
	// Block is a multi-line region reserved and rendered between Footnote and
	// Hints. Unlike Warnings it is drawn verbatim — no amber prefix — so the
	// caller styles its own lines. Used by the Work dashboard's filter menu
	// (ADR-0197). nil/empty = absent. When the pane cannot hold the full
	// block, fittedBlock clips it with an explicit indicator so Render never
	// exceeds TermH.
	Block []string
	// Flash and Hints share the bottom line. A live Flash takes it for three
	// seconds and the hints stay hidden until it expires (ADR-0204) — one line
	// either way, so a message costs no layout shift.
	Flash Flash
	Hints string // "" = absent
	// Mode is the word a true mode holds at the left of the bottom line, e.g.
	// SelectionMode. It sits ahead of the flash as well as the hints, because a
	// mode outlives any one message and a verb's refusal is exactly the moment
	// the human most needs to see which mode they are in (ADR-0215 decision 4).
	Mode string // "" = no mode
}

// modeWordStyle paints a mode word in the house accent, the same weight as a
// header: the bottom line's other tenants are dim or accent-plain, so the mode
// reads as chrome that is on rather than as a message.
var modeWordStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)

// bottomLine is the rendered bottom line: the live flash when there is one,
// otherwise the hints, and "" when the view has neither. One accessor so the
// budget and the paint cannot disagree about whether that line is present.
func (f Frame) bottomLine() string {
	rest := ""
	if line := f.Flash.Line(); line != "" {
		rest = line
	} else if f.Hints != "" {
		rest = hintStyle.Render(f.Hints)
	}
	return WithModeWord(f.Mode, rest)
}

// WithModeWord puts mode at the left of a bottom line already built, so a view
// that composes its own footer instead of going through Frame still shows which
// mode it is in. The word carries the table's own two-column row indent on the
// left, and the same again on the right, so it sits evenly between the margin and
// whatever follows it — a mode word butted straight against the hints reads as
// one run-on string. A rest that already indents itself (a flash always does) is
// left as it is, so the gap is two columns either way.
func WithModeWord(mode, rest string) string {
	if mode == "" {
		return rest
	}
	word := modeWordStyle.Render("  " + mode)
	if rest == "" || strings.HasPrefix(StripANSI(rest), " ") {
		return word + rest
	}
	return word + "  " + rest
}

// frameClipLine is the last Block row when the pane cannot hold the full list.
// Same wording as GateMenu's pane clamp so a clipped frame reads as house UI.
const frameClipLine = "  … clipped to fit the pane"

// fixedChromeLines returns the line count of every present region except Block
// and the body. Shared by BodyHeight and fittedBlock so budget and paint agree.
func (f Frame) fixedChromeLines() int {
	n := 0
	if f.Notice != "" {
		n++
	}
	if f.Header != "" {
		n++
	}
	if f.Subheader != "" {
		n++
	}
	if f.InputBox != "" {
		n += 3
	}
	n += len(f.Warnings)
	if f.Footnote != "" {
		n++
	}
	if f.bottomLine() != "" {
		n++
	}
	return n
}

// fittedBlock returns the Block lines that fit in termH alongside fixed chrome
// and a body floor of 3 (or whatever remains on a shorter pane). Block is the
// region that grows with the caller's content, so it yields when space runs
// out — clipped with frameClipLine rather than silently truncated (ADR-0197).
// termH <= 0 means height is unknown: the full Block is returned unchanged.
func (f Frame) fittedBlock(termH int) []string {
	if len(f.Block) == 0 {
		return nil
	}
	if termH <= 0 {
		return f.Block
	}
	remaining := termH - f.fixedChromeLines()
	if remaining <= 0 {
		return nil
	}
	minBody := 3
	if remaining < minBody {
		// Pane cannot hold the body floor; keep the body, drop the block.
		return nil
	}
	budget := remaining - minBody
	if budget <= 0 {
		return nil
	}
	if len(f.Block) <= budget {
		return f.Block
	}
	indicator := hintStyle.Render(frameClipLine)
	if budget == 1 {
		return []string{indicator}
	}
	out := make([]string, budget)
	copy(out, f.Block[:budget-1])
	out[budget-1] = indicator
	return out
}

// BodyHeight returns the body row budget for a terminal of height termH: termH
// minus every present region (1 for Notice, 1 for Header, 1 for Subheader, 3 for
// InputBox, len(Warnings) for warnings, 1 for Footnote, len(fitted Block) for
// Block, 1 for the flash-or-hints bottom line). When the pane is tall enough the
// body is floored at 3; on a shorter pane the floor yields so chrome (and a
// clipped Block) still fit — BodyHeight and Render stay in agreement.
func (f Frame) BodyHeight(termH int) int {
	blockLines := len(f.Block)
	if termH > 0 {
		blockLines = len(f.fittedBlock(termH))
	}
	h := termH - f.fixedChromeLines() - blockLines
	if h < 3 {
		// Floor only when the pane can actually hold chrome + body floor.
		// Otherwise the floor would force Render past TermH (ADR-0197).
		if termH <= 0 || f.fixedChromeLines()+blockLines+3 <= termH {
			h = 3
		} else if h < 0 {
			h = 0
		}
	}
	return h
}

// Render composes the frame's regions around body in the fixed order notice
// -> header -> subheader -> body -> input box -> warnings -> footnote -> block
// -> flash-or-hints, omitting absent ones. When TermH is known, a short body is
// padded to the full BodyHeight budget so trailing regions sit at the bottom of
// the screen, a long Block is clipped to the same budget, and the result never
// exceeds TermH lines.
func (f Frame) Render(body string) string {
	block := f.Block
	includeBody := true
	if f.TermH > 0 {
		body = f.padBody(body)
		block = f.fittedBlock(f.TermH)
		if f.BodyHeight(f.TermH) == 0 {
			includeBody = false
		}
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

	if includeBody {
		parts = append(parts, body)
	}

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

	if f.Footnote != "" {
		parts = append(parts, hintStyle.Render("  "+f.Footnote))
	}

	if len(block) > 0 {
		parts = append(parts, strings.Join(block, "\n"))
	}

	if bottom := f.bottomLine(); bottom != "" {
		parts = append(parts, bottom)
	}

	out := strings.Join(parts, "\n")
	if f.TermH > 0 {
		out = clampFrameToTermH(out, f.TermH, f.bottomLine() != "")
	}
	return out
}

// clampFrameToTermH is the last-resort guarantee that a Frame never exceeds the
// pane. fittedBlock should already have absorbed surplus via Block; this catches
// overfull bodies or other multi-line regions. When the bottom line (flash or
// hints) is present it stays on the last row — the cut lands above it with an
// explicit clip marker.
func clampFrameToTermH(content string, termH int, hasBottomLine bool) string {
	if termH <= 0 {
		return content
	}
	lines := strings.Split(content, "\n")
	if len(lines) <= termH {
		return content
	}
	indicator := hintStyle.Render(frameClipLine)
	if hasBottomLine {
		bottom := lines[len(lines)-1]
		if termH == 1 {
			return bottom
		}
		kept := append([]string{}, lines[:termH-2]...)
		kept = append(kept, indicator, bottom)
		return strings.Join(kept, "\n")
	}
	if termH == 1 {
		return indicator
	}
	kept := append([]string{}, lines[:termH-1]...)
	kept = append(kept, indicator)
	return strings.Join(kept, "\n")
}

// padBody appends blank lines so body occupies the full BodyHeight budget,
// pushing trailing regions to the bottom of the screen. A body that already
// fills the budget is returned unchanged (byte-identical). A body that
// overfills is truncated to the budget so Render cannot exceed TermH.
func (f Frame) padBody(body string) string {
	budget := f.BodyHeight(f.TermH)
	if budget <= 0 {
		return ""
	}
	lines := strings.Split(body, "\n")
	if len(lines) > budget {
		return strings.Join(lines[:budget], "\n")
	}
	if len(lines) >= budget {
		return body
	}
	return body + strings.Repeat("\n", budget-len(lines))
}
