package dashboard

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/glebglazov/pop/tasks/drain"
	"github.com/glebglazov/pop/work"
)

// RenderStatus prints the static Queue status surface (ADR-0121): a one-line
// Summary headline, then the Work dashboard's task-set table (the same rows,
// columns, row filter, and sort — status and the dashboard key on one row
// builder and one comparator), then a trailing drain.Scan errors section when there
// are scan errors. Every former per-bucket inventory section is retired; the
// STATUS column, live-drain indicator, and status suffixes now encode the
// picked-up / parked / awaiting / config-error state those sections carried.
//
// The Summary headline and drain.Scan errors are derived from the RunView (the
// existing aggregate) so the summary stays a scheduling roll-up; the table rows
// are the dashboard rows the command builds via queue.BuildDashboard. Output is
// plain text (no ANSI, non-interactive) so it stays greppable/pipeable and
// serves as the Queue run baseline.
// kinds is the same wiring list the dashboard renders through: the STATUS cell
// is composed by the kind that owns each row, so the static table and the TUI
// print the same text (ADR-0173).
func RenderStatus(out io.Writer, kinds []work.Kind, snap drain.StatusSnapshot, rows []DashboardRow) {
	view := drain.BuildRunView(snap, time.Now())
	drain.RenderRunSummary(out, view)
	renderStatusTable(out, newWorkKinds(kinds), rows)
	renderStatusScanErrors(out, view.ScanErrors)
}

// renderStatusTable renders the dashboard's task-set table as static plain
// text: the PROJECT / TASK SET / STATUS / WORKTREE columns plus the trailing
// live-drain indicator. It reuses the dashboard's headers and column-width math
// (dashboardColumnWidths measures with lipgloss.Width, which strips ANSI, so the
// widths match the dashboard's) but renders fully plain cells via
// statusRowValues so no styling leaks into the pipeable surface. Widths are the
// natural widths (no terminal-fit shrink) so nothing is truncated, and each line
// is right-trimmed so the empty trailing indicator leaves no dangling
// whitespace.
func renderStatusTable(out io.Writer, kinds workKinds, rows []DashboardRow) {
	fmt.Fprintln(out)
	if len(rows) == 0 {
		fmt.Fprintln(out, "No queue-actionable task sets.")
		return
	}
	headers := dashboardTableHeaders()
	widths := workPage().columnWidths(kinds, rows)
	fmt.Fprintln(out, strings.TrimRight(dashboardTableLine(headers, widths), " "))
	fmt.Fprintln(out, strings.TrimRight(dashboardTableSeparator(headers, widths), " "))
	for _, row := range rows {
		fmt.Fprintln(out, strings.TrimRight(dashboardTableLine(statusRowValues(kinds, row), widths), " "))
	}
}

// statusRowValues returns a dashboard row's fully plain column cells for the
// static status table. Unlike dashboardRowValues / dashboardRowNaturalValues —
// which style the WORKTREE destination badge for the TUI — every cell here is
// plain text: the composed STATUS cell (already un-styled) and the plain
// destination label keep the status surface ANSI-free and greppable.
func statusRowValues(kinds workKinds, row DashboardRow) []string {
	return []string{
		row.Project,
		row.ID,
		dashboardStatusCellText(kinds, row),
		work.WorktreeLabel(row.DestKind, row.Worktree),
		dashboardActivityCluster(row, livePaneCache{}, false),
	}
}

// renderStatusScanErrors prints the trailing drain.Scan errors section, only when
// there are scan errors, projects sorted for stable output.
func renderStatusScanErrors(out io.Writer, scanErrors map[string]string) {
	if len(scanErrors) == 0 {
		return
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Scan errors:")
	projects := make([]string, 0, len(scanErrors))
	for project := range scanErrors {
		projects = append(projects, project)
	}
	sort.Strings(projects)
	for _, project := range projects {
		fmt.Fprintf(out, "  %s: %s\n", project, scanErrors[project])
	}
}
