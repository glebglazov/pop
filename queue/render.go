package queue

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/setkind"
	"github.com/glebglazov/pop/ui"
	"github.com/glebglazov/pop/work"
)

// The Work dashboard's data core is pure and carries no lipgloss (ADR-0143):
// styled cell wrappers, the lipgloss style maps, column-width/layout math, and
// table-line/header rendering stay queue-side as one render-shared layer. Both
// the static `pop queue status` render (status.go) and the TUI model
// (dashboard.go) key on this file so the boundary reads as designed rather
// than as leftovers.

// dashboardStatusCellStyled paints the STATUS cell the row's kind composed: it
// walks that one segment sequence and gives each token the style its tone asks
// for — the status label its semantic bucket colour, an attention badge its
// three-state colour (ADR-0156), every plain suffix nothing. Because both forms
// walk the same segments, the styled cell and the width-measured plain one
// (dashboardStatusCellText) differ only by ANSI, whatever kind wrote them.
func dashboardStatusCellStyled(kinds workKinds, row DashboardRow) string {
	segments := kinds.statusSegments(row)
	parts := make([]string, 0, len(segments))
	for _, seg := range segments {
		if seg.Text == "" {
			continue
		}
		parts = append(parts, dashboardStatusSegmentStyled(seg))
	}
	return strings.Join(parts, " · ")
}

// dashboardStatusSegmentStyled renders one segment. A tone names what the token
// means; this is the only place that decides what it looks like (ADR-0143).
func dashboardStatusSegmentStyled(seg work.StatusSegment) string {
	switch seg.Tone {
	case work.ToneLabel:
		if st, ok := dashboardStatusBucketStyle[seg.Text]; ok {
			return st.Render(seg.Text)
		}
	case work.ToneGood:
		return dashboardVerifiedAtAtHeadStyle.Render(seg.Text)
	case work.ToneWarn:
		return dashboardVerifiedAtDriftedStyle.Render(seg.Text)
	case work.ToneBad:
		return dashboardVerifiedAtUnverifiedStyle.Render(seg.Text)
	}
	return seg.Text
}

// detailStatusStyled paints the same segments for a detail header, where the
// status label sits inside brackets that already set it apart: the label is left
// unpainted (a bracketed label in its bucket colour reads as two markers for one
// fact) while every attention badge keeps its three-state colour (ADR-0156).
func detailStatusStyled(kinds workKinds, row DashboardRow) string {
	segments := kinds.statusSegments(row)
	parts := make([]string, 0, len(segments))
	for _, seg := range segments {
		if seg.Text == "" {
			continue
		}
		if seg.Tone == work.ToneLabel {
			parts = append(parts, seg.Text)
			continue
		}
		parts = append(parts, dashboardStatusSegmentStyled(seg))
	}
	return strings.Join(parts, " · ")
}

// dashboardManagedWtStyle colors the [managed wt] destination badge.
var dashboardManagedWtStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))

// dashboardVerifiedAtBadgeStyled renders the Verified-at SHA badge with the
// three-state colour rule (ADR-0156): green at HEAD, yellow when drifted, red
// unverified.
func dashboardVerifiedAtBadgeStyled(row DashboardRow) string {
	badge := tasks.DeriveVerifiedAtBadge(tasks.Row{
		Status:            row.RawStatus,
		VerifiedAtSHA:     row.VerifiedAtSHA,
		VerifiedAtDrifted: row.VerifiedAtDrifted,
	})
	text := tasks.VerifiedAtBadgeText(badge)
	if text == "" {
		return ""
	}
	switch badge.State {
	case tasks.VerifiedAtAtHead:
		return dashboardVerifiedAtAtHeadStyle.Render(text)
	case tasks.VerifiedAtDrifted:
		return dashboardVerifiedAtDriftedStyle.Render(text)
	case tasks.VerifiedAtUnverified:
		return dashboardVerifiedAtUnverifiedStyle.Render(text)
	default:
		return text
	}
}

// dashboardVerifiedAtAtHeadStyle colors a PASS-at-HEAD badge (same green as DONE).
var dashboardVerifiedAtAtHeadStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))

// dashboardVerifiedAtDriftedStyle colors a drifted PASS badge (same yellow as
// pop tasks status Details output).
var dashboardVerifiedAtDriftedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))

// dashboardVerifiedAtUnverifiedStyle colors the red unverified badge.
var dashboardVerifiedAtUnverifiedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))

// dashboardStatusBucketStyle maps a base status label to its semantic bucket
// color. Only the base label token is colored here; the verified@/auto-drain/
// orphaned suffixes keep their own styling, so this is applied to the label
// before suffixes are appended in dashboardStatusCellStyled. The map is keyed by
// the display label, so "IN PROGRESS" (the started-READY refinement) shares
// READY's blue bucket.
//
// Bucket rationale (from the grilling session):
//   - green  DONE — terminal success.
//   - blue   READY / IN PROGRESS — in-flight work, nothing wrong.
//   - yellow NEEDS-VERIFY / AWAITING-APPROVAL / BLOCKED — "needs-you": each
//     waits on a human decision. BLOCKED is a needs-you gate, not a failure,
//     so it sits with the amber attention bucket rather than red.
//   - red    FAILED / VERIFY-FAILED / MALFORMED / MISSING — the problem bucket;
//     MALFORMED (bad task file) and MISSING (no manifest) fold in here as
//     structural problems alongside outright failures.
//   - faint  DEFERRED — intentionally shelved, dimmed to recede.
//
// The mapping is trivially reversible, so no ADR backs it.
var dashboardStatusBucketStyle = map[string]lipgloss.Style{
	string(tasks.StatusDone):             lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
	string(tasks.StatusReady):            lipgloss.NewStyle().Foreground(lipgloss.Color("4")),
	"IN PROGRESS":                        lipgloss.NewStyle().Foreground(lipgloss.Color("4")),
	"WAYFINDING":                         lipgloss.NewStyle().Foreground(lipgloss.Color("6")),
	string(tasks.StatusNeedsVerify):      lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
	string(tasks.StatusAwaitingApproval): lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
	string(tasks.StatusBlocked):          lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
	string(tasks.StatusFailed):           lipgloss.NewStyle().Foreground(lipgloss.Color("1")),
	string(tasks.StatusVerifyFailed):     lipgloss.NewStyle().Foreground(lipgloss.Color("1")),
	string(tasks.StatusMalformed):        lipgloss.NewStyle().Foreground(lipgloss.Color("1")),
	string(tasks.StatusMissing):          lipgloss.NewStyle().Foreground(lipgloss.Color("1")),
	string(tasks.StatusDeferred):         lipgloss.NewStyle().Faint(true),
}

// renderDashboardDest applies destination-column styling to the plain label.
// kind and the plain destination labels key on work's definitions (ADR-0143)
// so the styled wrapper and the row-build fallbacks share one source.
func renderDashboardDest(kind work.DestKind, label string) string {
	switch kind {
	case work.DestManagedDirective:
		return dashboardManagedWtStyle.Render(work.DestLabelManagedWt)
	case work.DestNeedsBind:
		return ui.HintStyle.Render(work.DestLabelNeedsBind)
	case work.DestDoneManagedBound:
		return dashboardManagedWtStyle.Render("[managed wt " + label + "]")
	default:
		return label
	}
}

// dashboardColumns holds a table's precomputed natural and terminal-fit column
// widths, cached on the model so resize and row updates recompute cheaply. It
// carries the page whose columns it measures — a page's header labels are the
// floor every fit respects — and the wiring list those headers are asked of.
type dashboardColumns struct {
	page    dashboardPage
	kinds   workKinds
	natural []int
	widths  []int
	width   int
}

const (
	dashboardColProject = iota
	dashboardColSetID
	dashboardColStatus
	dashboardColWorktree
	dashboardColIndicator
)

const dashboardColSep = 2

// dashboardColShrinkOrder lists elastic columns in shrink priority: WORKTREE
// gives way first. The trailing activity cluster is fixed-width and absent here,
// so narrow-pane fitting never drops it (ADR-0158).
var dashboardColShrinkOrder = []int{
	dashboardColWorktree,
	dashboardColStatus,
	dashboardColSetID,
	dashboardColProject,
}

// dashboardTableHeaders is the Task-set columns as the static `pop queue status`
// table prints them, taken from the Task-set **Work kind** rather than restated
// here: the kind authors these cells, and a Map row on the same page fills them.
// The trailing column is the per-activity cluster: an empty header over the IVFS
// keys, so no label sits above the glyphs. The TUI asks the wired kind instead —
// see dashboardPage.headers.
func dashboardTableHeaders() []string {
	return setkind.TaskSetColumns()
}

func dashboardTableLineWidth(widths []int) int {
	if len(widths) == 0 {
		return 0
	}
	total := 0
	for _, w := range widths {
		total += w
	}
	return total + dashboardColSep*(len(widths)-1)
}

// dashboardListCellBudget is the visible width available to a List row's table
// cells after the List's 2-char cursor / pad prefix.
func dashboardListCellBudget(termWidth int) int {
	if termWidth > 2 {
		return termWidth - 2
	}
	return termWidth
}

// dashboardTableBodyBudget is the visible width available to a table line that
// carries a 2-char body indent ("  " prefix before the cells).
func dashboardTableBodyBudget(termWidth int) int {
	if termWidth > 2 {
		return termWidth - 2
	}
	return termWidth
}

func (c *dashboardColumns) syncNatural(kinds workKinds, rows []DashboardRow) {
	c.kinds = kinds
	c.natural = c.page.columnWidths(kinds, rows)
	c.refit()
}

func (c *dashboardColumns) refit() {
	c.widths = c.page.fitWidths(c.kinds, c.natural, dashboardListCellBudget(c.width))
}

// tableWidthsForRows is the fitted width set for a table rendered with a body
// indent rather than a List cursor column.
func (p dashboardPage) tableWidthsForRows(kinds workKinds, rows []DashboardRow, termWidth int) []int {
	return p.fitWidths(kinds, p.columnWidths(kinds, rows), dashboardTableBodyBudget(termWidth))
}

const (
	dashboardTwoLineWidthThreshold = 120
	dashboardTwoLineSetIDThreshold = 36
	// dashboardTwoLineHeightFloor is the pane-height floor below which the table
	// stays single-line regardless of width or set-id length (ADR-0107). In a
	// short tmux popup, visible-row density beats id completeness.
	dashboardTwoLineHeightFloor = 16
)

// dashboardTwoLineMode reports whether the Work dashboard should render each
// row on two lines. Two-line mode is height-gated (ADR-0107): it engages only
// when the pane is roomy (termHeight >= dashboardTwoLineHeightFloor). When
// roomy, it activates if the terminal is narrow (< 120 columns) or any visible
// Task set identifier is long (> 36 characters). Below the height floor every
// row stays single-line. When active, every row uses the same two-line shape
// (uniform height).
func dashboardTwoLineMode(rows []DashboardRow, termWidth, termHeight int) bool {
	if termHeight < dashboardTwoLineHeightFloor {
		return false
	}
	if termWidth < dashboardTwoLineWidthThreshold {
		return true
	}
	for _, row := range rows {
		if len(row.ID) > dashboardTwoLineSetIDThreshold {
			return true
		}
	}
	return false
}

// dashboardTwoLineHeaders returns the line-1 column headers for two-line mode:
// PROJECT, TASK SET, WORKTREE, and the trailing activity cluster (empty header).
// STATUS is rendered on line 2, indented to sit under the TASK SET column (see
// dashboardTwoLineStatusHeader).
func dashboardTwoLineHeaders() []string {
	return []string{"PROJECT", "TASK SET", "WORKTREE", ""}
}

// Line-1 column indices for two-line mode.
const (
	dashboardTwoLineColProject = iota
	dashboardTwoLineColSetID
	dashboardTwoLineColWorktree
	dashboardTwoLineColIndicator
)

// dashboardTwoLineStatusIndent is the leading padding for the line-2 STATUS cell
// so it aligns under the TASK SET column, past the PROJECT column and its
// separator.
func dashboardTwoLineStatusIndent(line1Widths []int) int {
	if len(line1Widths) <= dashboardTwoLineColProject {
		return 0
	}
	return line1Widths[dashboardTwoLineColProject] + dashboardColSep
}

// dashboardTwoLineStatusHeader renders the line-2 header: STATUS indented under
// the TASK SET column.
func dashboardTwoLineStatusHeader(line1Widths []int) string {
	return strings.Repeat(" ", dashboardTwoLineStatusIndent(line1Widths)) + "STATUS"
}

// dashboardTwoLineRowValuesLine1 returns the cell values for line 1 of a two-line
// row: PROJECT, TASK SET (the set id), WORKTREE, and the trailing activity
// cluster.
func dashboardTwoLineRowValuesLine1(row DashboardRow, live livePaneCache) []string {
	return []string{
		row.Project,
		row.ID,
		renderDashboardDest(row.DestKind, row.Worktree),
		dashboardActivityCluster(row, live, true),
	}
}

// dashboardTwoLineNaturalWidths returns the natural widths of the line-1 columns
// in two-line mode, floored at the header label width.
func dashboardTwoLineNaturalWidths(rows []DashboardRow) []int {
	headers := dashboardTwoLineHeaders()
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, v := range dashboardTwoLineRowValuesLine1(row, livePaneCache{}) {
			if n := lipgloss.Width(v); n > widths[i] {
				widths[i] = n
			}
		}
	}
	return widths
}

// dashboardTwoLineTableLineWidth returns the rendered width of a line-1 row or
// header given the two-line column widths.
func dashboardTwoLineTableLineWidth(widths []int) int {
	if len(widths) == 0 {
		return 0
	}
	total := 0
	for _, w := range widths {
		total += w
	}
	return total + dashboardColSep*(len(widths)-1)
}

// dashboardTwoLineColShrinkOrder lists elastic line-1 columns in shrink
// priority: WORKTREE gives way first, then PROJECT, so the TASK SET set id keeps
// as much width as possible and only truncates as a last resort. The trailing
// activity cluster is fixed-width and absent here, so it is never dropped.
var dashboardTwoLineColShrinkOrder = []int{
	dashboardTwoLineColWorktree,
	dashboardTwoLineColProject,
	dashboardTwoLineColSetID,
}

// dashboardTwoLineFitWidths shrinks the line-1 columns until the row fits budget.
func dashboardTwoLineFitWidths(natural []int, budget int) []int {
	if budget <= 0 || len(natural) == 0 {
		return append([]int(nil), natural...)
	}
	widths := append([]int(nil), natural...)
	headers := dashboardTwoLineHeaders()
	mins := make([]int, len(headers))
	for i, h := range headers {
		mins[i] = len(h)
	}
	for dashboardTwoLineTableLineWidth(widths) > budget {
		shrunk := false
		for _, col := range dashboardTwoLineColShrinkOrder {
			if widths[col] > mins[col] {
				widths[col]--
				shrunk = true
				break
			}
		}
		if !shrunk {
			break
		}
	}
	return widths
}

// dashboardTwoLineTableHeader renders the two-line mode line-1 header.
func dashboardTwoLineTableHeader(widths []int) string {
	return dashboardTableLine(dashboardTwoLineHeaders(), widths)
}

// dashboardTwoLineTableSeparator renders the two-line mode line-1 separator.
func dashboardTwoLineTableSeparator(widths []int) string {
	return dashboardTableSeparator(dashboardTwoLineHeaders(), widths)
}

// dashboardTwoLineRowLine1 renders the padded line-1 cells of a two-line row.
func dashboardTwoLineRowLine1(row DashboardRow, widths []int, live livePaneCache) string {
	return dashboardTableLine(dashboardTwoLineRowValuesLine1(row, live), widths)
}

// dashboardTwoLineRowLine2 renders line 2 of a two-line row: the STATUS value,
// indented to sit under the TASK SET column on line 1. The List (and the bespoke
// overlay path) supply the two-space gutter on top of this indent.
func dashboardTwoLineRowLine2(kinds workKinds, row DashboardRow, line1Widths []int) string {
	return strings.Repeat(" ", dashboardTwoLineStatusIndent(line1Widths)) + dashboardStatusCellStyled(kinds, row)
}

// dashboardTableChromeLines is the number of body lines above the List rows in
// single-line mode: the blank line under the summary header, the column header,
// and the separator.
const dashboardTableChromeLines = 3

// dashboardTwoLineChromeLines is the chrome height in two-line mode: the blank
// line, the line-1 (PROJECT/TASK SET/WORKTREE) header, the line-2 (STATUS)
// header, and the separator.
const dashboardTwoLineChromeLines = dashboardTableChromeLines + 1

// dashboardRowValues returns a row's rendered column cells, with the STATUS cell
// composed at render time from the row's live fields (styled for display).
func dashboardRowValues(kinds workKinds, row DashboardRow, live livePaneCache) []string {
	return []string{
		row.Project,
		row.ID,
		dashboardStatusCellStyled(kinds, row),
		renderDashboardDest(row.DestKind, row.Worktree),
		dashboardActivityCluster(row, live, true),
	}
}

// dashboardRowNaturalValues returns a row's column cells for width measurement.
// It matches dashboardRowValues but uses the plain, un-styled composed status so
// no ANSI ever reaches column-width math (ADR-0108).
func dashboardRowNaturalValues(kinds workKinds, row DashboardRow) []string {
	return []string{
		row.Project,
		row.ID,
		dashboardStatusCellText(kinds, row),
		renderDashboardDest(row.DestKind, row.Worktree),
		dashboardActivityCluster(row, livePaneCache{}, false),
	}
}

func dashboardTableLine(values []string, widths []int) string {
	parts := make([]string, len(values))
	for i, v := range values {
		width := 0
		if i < len(widths) {
			width = widths[i]
		}
		parts[i] = padDashboardCell(v, width)
	}
	return strings.Join(parts, "  ")
}

// dashboardTableSeparator rules each column that carries a header label. A column
// with no label — page A's trailing activity cluster (ADR-0158) — gets blanks
// instead: there is nothing to underline. Reading the rule off the headers is what
// lets a page whose last column *is* labelled (the Routine page's STATUS) keep its
// dashes without a second switch.
func dashboardTableSeparator(headers []string, widths []int) string {
	parts := make([]string, len(widths))
	for i, width := range widths {
		labelled := i < len(headers) && headers[i] != ""
		if !labelled {
			parts[i] = strings.Repeat(" ", width)
			continue
		}
		parts[i] = strings.Repeat("-", width)
	}
	return strings.Join(parts, "  ")
}

func padDashboardCell(s string, width int) string {
	if width <= 0 {
		return s
	}
	if lipgloss.Width(s) > width {
		s = ui.TruncateString(s, width)
	}
	if pad := width - lipgloss.Width(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}
