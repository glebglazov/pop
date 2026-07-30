package queue

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/ui"
	"github.com/glebglazov/pop/work"
)

// The Work dashboard's data core is pure and carries no lipgloss (ADR-0143):
// styled cell wrappers, the lipgloss style maps, column-width/layout math, and
// table-line/header rendering stay queue-side as one render-shared layer. Both
// the static `pop queue status` render (status.go) and the TUI model
// (dashboard.go) key on this file so the boundary reads as designed rather
// than as leftovers.

// dashboardStatusCellStyled is the plain composed STATUS cell (work.StatusCell)
// with per-token styling for the TUI: the base label carries its semantic
// bucket colour and the immunized "verified @ <sha>" token renders yellow,
// while the auto-drain, orphaned, parked, and config-error suffixes stay
// plain. It layers styling over work's unstyled composition (ADR-0143) so
// work.StatusCell — the width-measured form — stays ANSI-free, and it
// reproduces work.StatusCell's token order so the two forms differ only by
// ANSI. Map rows colour the WAYFINDING label and keep the tally plain
// (ADR-0130).
func dashboardStatusCellStyled(row DashboardRow) string {
	if row.IsMap {
		label := "WAYFINDING"
		if st, ok := dashboardStatusBucketStyle[label]; ok {
			label = st.Render(label)
		}
		return fmt.Sprintf("%s · %d open / %d frontier", label, row.MapOpen, row.MapFrontier)
	}
	label := work.StatusLabel(row)
	if st, ok := dashboardStatusBucketStyle[label]; ok {
		label = st.Render(label)
	}
	if badgeText := dashboardVerifiedAtBadgeStyled(row); badgeText != "" {
		label += " · " + badgeText
	}
	if work.AutoDrainWaiting(row) {
		label += " · auto-drain"
	}
	if row.Orphaned {
		label += " · orphaned"
	}
	// Parked and config-error ride the STATUS cell (ADR-0111) as uncoloured plain
	// text, trailing the auto-drain/orphaned suffixes in the same fixed order
	// work.StatusCell uses.
	if row.Parked {
		label += " · parked"
	}
	if row.ConfigError != "" {
		label += " · config error: " + row.ConfigError
	}
	return label
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
// widths, cached on the model so resize and row updates recompute cheaply.
type dashboardColumns struct {
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

// dashboardTableHeaders is the fixed column header row. The trailing column is
// the per-activity cluster: an empty header over the IVFS keys, so no label sits
// above the glyphs.
func dashboardTableHeaders() []string {
	return []string{"PROJECT", "TASK SET", "STATUS", "WORKTREE", ""}
}

// dashboardColumnWidths precomputes each column's natural width over the full row
// set, floored at the header label width.
func dashboardColumnWidths(rows []DashboardRow) []int {
	headers := dashboardTableHeaders()
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, v := range dashboardRowNaturalValues(row) {
			if n := lipgloss.Width(v); n > widths[i] {
				widths[i] = n
			}
		}
	}
	return widths
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

// dashboardFitColumnWidths shrinks elastic columns until the table fits budget.
// When budget is still exceeded after shrinking, cells are truncated at render
// time via padDashboardCell.
func dashboardFitColumnWidths(natural []int, budget int) []int {
	if budget <= 0 || len(natural) == 0 {
		return append([]int(nil), natural...)
	}
	widths := append([]int(nil), natural...)
	headers := dashboardTableHeaders()
	mins := make([]int, len(headers))
	for i, h := range headers {
		mins[i] = len(h)
	}
	for dashboardTableLineWidth(widths) > budget {
		shrunk := false
		for _, col := range dashboardColShrinkOrder {
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

func (c *dashboardColumns) syncNatural(rows []DashboardRow) {
	c.natural = dashboardColumnWidths(rows)
	c.refit()
}

func (c *dashboardColumns) refit() {
	c.widths = dashboardFitColumnWidths(c.natural, dashboardListCellBudget(c.width))
}

func dashboardTableWidthsForRows(rows []DashboardRow, termWidth int) []int {
	return dashboardFitColumnWidths(dashboardColumnWidths(rows), dashboardTableBodyBudget(termWidth))
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
		if len(row.SetID) > dashboardTwoLineSetIDThreshold {
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
		row.SetID,
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
		for i, v := range dashboardTwoLineRowValuesLine1(row, nil) {
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
	return dashboardTableSeparator(widths)
}

// dashboardTwoLineRowLine1 renders the padded line-1 cells of a two-line row.
func dashboardTwoLineRowLine1(row DashboardRow, widths []int, live livePaneCache) string {
	return dashboardTableLine(dashboardTwoLineRowValuesLine1(row, live), widths)
}

// dashboardTwoLineRowLine2 renders line 2 of a two-line row: the STATUS value,
// indented to sit under the TASK SET column on line 1. The List (and the bespoke
// overlay path) supply the two-space gutter on top of this indent.
func dashboardTwoLineRowLine2(row DashboardRow, line1Widths []int) string {
	return strings.Repeat(" ", dashboardTwoLineStatusIndent(line1Widths)) + dashboardStatusCellStyled(row)
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
func dashboardRowValues(row DashboardRow, live livePaneCache) []string {
	return []string{
		row.Project,
		row.SetID,
		dashboardStatusCellStyled(row),
		renderDashboardDest(row.DestKind, row.Worktree),
		dashboardActivityCluster(row, live, true),
	}
}

// dashboardRowNaturalValues returns a row's column cells for width measurement.
// It matches dashboardRowValues but uses the plain, un-styled composed status so
// no ANSI ever reaches column-width math (ADR-0108).
func dashboardRowNaturalValues(row DashboardRow) []string {
	return []string{
		row.Project,
		row.SetID,
		work.StatusCell(row),
		renderDashboardDest(row.DestKind, row.Worktree),
		dashboardActivityCluster(row, nil, false),
	}
}

func dashboardTableLine(values []string, widths []int) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = padDashboardCell(v, widths[i])
	}
	return strings.Join(parts, "  ")
}

func dashboardTableSeparator(widths []int) string {
	parts := make([]string, len(widths))
	for i, width := range widths {
		// The trailing activity cluster has no header label, so its rule is blank
		// (spaces) rather than dashes — nothing to underline (ADR-0158).
		if i == len(widths)-1 {
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
