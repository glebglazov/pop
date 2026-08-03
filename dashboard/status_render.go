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

// StatusTable is one table of the static status surface: the wiring list whose
// kinds compose its cells, and the containers to print. Kinds is the same list
// the matching dashboard page renders through, so the static table and the TUI
// print the same text (ADR-0173).
type StatusTable struct {
	Kinds []work.Kind
	Rows  []DashboardRow
}

// StatusTables are the two tables `pop work status` prints, in order.
type StatusTables struct {
	// TaskSets is page A's row set. Map rows are dropped from it on the way out
	// (see renderStatusTable): status reports what the daemon can advance, and a
	// Map never advances unattended, so this one row type breaks the otherwise
	// exact "status renders the dashboard's table" identity.
	TaskSets StatusTable
	// Routines is page B's row set, whole: every Routine there is, in the Routine
	// kind's own relevance order.
	Routines StatusTable
}

// RenderStatus prints the static status surface (ADR-0121): a one-line Summary
// headline, then the Work dashboard's task-set table (the same rows, columns,
// row filter, and sort — status and the dashboard key on one row builder and one
// comparator), then the Routines table, then a trailing drain.Scan errors section
// when there are scan errors. Every former per-bucket inventory section is
// retired; the STATUS column, live-drain indicator, and status suffixes now
// encode the picked-up / parked / awaiting / config-error state those sections
// carried.
//
// The Summary headline and drain.Scan errors are derived from the RunView (the
// existing aggregate) so the summary stays a scheduling roll-up; the table rows
// are the page snapshots the command builds. Output is plain text (no ANSI,
// non-interactive) so it stays greppable/pipeable and serves as the daemon's run
// baseline.
func RenderStatus(out io.Writer, snap drain.StatusSnapshot, tables StatusTables) {
	view := drain.BuildRunView(snap, time.Now())
	drain.RenderRunSummary(out, view)
	renderStatusTable(out, statusTaskSetsCaption, workPage(), tables.TaskSets, mapRow)
	renderStatusTable(out, statusRoutinesCaption, routinePage(), tables.Routines, nil)
	renderStatusScanErrors(out, view.ScanErrors)
}

// The two table captions. Two tables with two column sets under one headline
// need saying which is which; the trailing colon matches the scan-errors section.
const (
	statusTaskSetsCaption = "Task sets:"
	statusRoutinesCaption = "Routines:"
)

// renderStatusTable renders one page's rows as static plain text under caption:
// the page's own headers (asked of its primary kind) over its fully plain cells,
// with rows matching omit dropped. It reuses the page's column-width math
// (measured over the same plain cells it prints, so nothing is mismeasured), at
// natural widths with no terminal-fit shrink so nothing is truncated, and each
// line right-trimmed so an empty trailing cell leaves no dangling whitespace.
func renderStatusTable(out io.Writer, caption string, page dashboardPage, table StatusTable, omit func(DashboardRow) bool) {
	kinds := newWorkKinds(table.Kinds)
	rows := table.Rows
	if omit != nil {
		kept := make([]DashboardRow, 0, len(rows))
		for _, row := range rows {
			if !omit(row) {
				kept = append(kept, row)
			}
		}
		rows = kept
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, caption)
	if len(rows) == 0 {
		fmt.Fprintln(out, page.empty)
		return
	}
	headers := page.headers(kinds)
	widths := page.statusWidths(kinds, rows)
	fmt.Fprintln(out, strings.TrimRight(dashboardTableLine(headers, widths), " "))
	fmt.Fprintln(out, strings.TrimRight(dashboardTableSeparator(headers, widths), " "))
	for _, row := range rows {
		fmt.Fprintln(out, strings.TrimRight(dashboardTableLine(page.statusCells(kinds, row), widths), " "))
	}
}

// statusRowValues returns a Task-set row's fully plain column cells for the
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
