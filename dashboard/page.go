package dashboard

import (
	"charm.land/lipgloss/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/routine"
	"github.com/glebglazov/pop/tasks/drain"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// One dashboard, two pages. The `v` toggle that used to swap between two TUIs now
// switches between two pages of this one: page A holds the registered kinds (Task
// sets and Maps) under the Task-set columns, page B holds Routines under the
// Routine columns. `pop routine dashboard` is a thin entry onto page B.
//
// The split is a paged one rather than a kind-filter predicate over a single flat
// list because the two column sets share no cell: one flat list would have to
// render both under one header, and the toggle the operator already knows says
// "show me the other thing" rather than "narrow this".
//
// Everything page-shaped is a field of dashboardPage — which kinds the page
// lists, which of them heads it, how a container projects onto cells, the words
// its chrome uses — so a page for a future kind is an entry here instead of
// custom dashboard code.

// Page names one page of the Work dashboard.
type Page int

const (
	// PageWork lists Task sets and Maps.
	PageWork Page = iota
	// PageRoutines lists Routines.
	PageRoutines
)

// dashboardPage is one page's whole configuration.
type dashboardPage struct {
	id Page
	// title is the header prefix and the noun the help overlay titles carry.
	title string
	// primary is the kind whose Columns() this page's header row reads. A
	// non-primary kind on the page fills those same columns, which is exactly what
	// a Map row already does to the Task-set ones. The header belongs to the kind
	// that authors the cells beneath it (ADR-0173), so a page declares which kind
	// heads it and nothing more.
	primary work.KindID
	// kinds resolves the page's wiring list, once per build: the poll rebuilds
	// through it, so a page never renders another page's containers and each page's
	// Kind.Summary counts only its own.
	kinds func(*drain.Deps, *config.Config) []work.Kind
	// styledCells and plainCells project one container onto this page's column
	// cells — styled for display, plain for the column-width math, in the order the
	// primary kind's Columns() names them. Cells are container fields, so the
	// projection is a pure function of the row; it lives page-side because painting
	// them is the render layer's business (ADR-0143).
	styledCells func(workKinds, DashboardRow, livePaneCache) []string
	plainCells  func(workKinds, DashboardRow) []string
	// statusCells is the same projection for the static `pop work status` table,
	// where no cell may carry ANSI at all — the TUI's plain cells still style the
	// WORKTREE badge, because the TUI paints them.
	statusCells func(workKinds, DashboardRow) []string
	// shrinkOrder lists the elastic columns in the order they give way when the
	// table overruns the terminal budget.
	shrinkOrder []int
	// twoLineCapable marks a page that may fold a row onto two lines in a narrow
	// pane (ADR-0107). Only the Task-set columns have ever done so.
	twoLineCapable bool
	// rowFilters marks a page with a Work view preset list behind `f`
	// (ADR-0197). Presets are Task-set / Map vocabulary on shared deps, so a page
	// they mean nothing to must not offer the key — it would quietly change the
	// other page's view.
	rowFilters bool
	// toggleWord is what the footer hint and the help overlay say `v` switches to.
	toggleWord string
	// empty is the body text for a page with no rows at all.
	empty string
	// searchNoun is what this page calls its rows in the zero-match empty state —
	// the one screen where the search has hidden everything, so the term and the
	// way out both have to be written on it.
	searchNoun string
}

// pageSpec returns one page's configuration. It is the whole of the page table:
// two entries, each naming its primary kind.
func pageSpec(id Page) dashboardPage {
	if id == PageRoutines {
		return routinePage()
	}
	return workPage()
}

// other is the page `v` switches to. With two pages the toggle is total: every
// page has exactly one other, from either side.
func (p dashboardPage) other() Page {
	if p.id == PageRoutines {
		return PageWork
	}
	return PageRoutines
}

// workPage is page A: the registered kinds under the Task-set columns, with the
// activity cluster trailing them and two-line folding available in a narrow pane.
func workPage() dashboardPage {
	return dashboardPage{
		id:      PageWork,
		title:   "Work",
		primary: ref.KindTaskSet,
		kinds: func(d *drain.Deps, cfg *config.Config) []work.Kind {
			return d.WorkKinds(cfg)
		},
		styledCells:    dashboardRowValues,
		plainCells:     dashboardRowNaturalValues,
		statusCells:    statusRowValues,
		shrinkOrder:    dashboardColShrinkOrder,
		twoLineCapable: true,
		rowFilters:     true,
		toggleWord:     "routines",
		empty:          "No work-actionable task sets.",
		searchNoun:     "task sets",
	}
}

// routinePage is page B: every Routine there is, ordered by the Routine kind's
// own comparator — relevance tier, then id — with no filtering of any sort. A
// global list is what makes the "M here" tally in its summary meaningful, and it
// is why the outside-a-project special case the Routine TUI carried is gone.
func routinePage() dashboardPage {
	return dashboardPage{
		id:      PageRoutines,
		title:   "Routines",
		primary: ref.KindRoutine,
		kinds: func(d *drain.Deps, cfg *config.Config) []work.Kind {
			return d.RoutinePageKinds(cfg)
		},
		styledCells:   routineRowValues,
		plainCells:    routineRowNaturalValues,
		statusCells:   routineRowNaturalValues,
		shrinkOrder:   routineColShrinkOrder,
		toggleWord:    "work",
		empty:      routine.EmptyListHint,
		searchNoun: "routines",
	}
}

// Routine column indices, in the order the Routine kind's Columns() names them.
const (
	routineColID = iota
	routineColDirectory
	routineColSchedule
	routineColLastRun
	routineColStatus
)

// routineColShrinkOrder lists the Routine page's elastic columns in shrink
// priority: the bound directory (the longest cell, and the one a reader can infer
// from the id) gives way first, the status label last.
var routineColShrinkOrder = []int{
	routineColDirectory,
	routineColSchedule,
	routineColLastRun,
	routineColID,
	routineColStatus,
}

// routineRowValues returns a Routine row's rendered column cells.
func routineRowValues(kinds workKinds, row DashboardRow, _ livePaneCache) []string {
	return []string{
		routineIDCellStyled(row),
		row.RoutineDirectory,
		row.RoutineSchedule,
		row.RoutineLastRun,
		dashboardStatusCellStyled(kinds, row),
	}
}

// routineRowNaturalValues returns the same cells for width measurement: no ANSI
// ever reaches column math (ADR-0108), and the badge is measured because it is
// rendered.
func routineRowNaturalValues(kinds workKinds, row DashboardRow) []string {
	return []string{
		routineIDCellPlain(row),
		row.RoutineDirectory,
		row.RoutineSchedule,
		row.RoutineLastRun,
		dashboardStatusCellText(kinds, row),
	}
}

// dashboardBadgeStyle colours a container's marker badge — today a Project
// routine's ◆ (ADR-0138), which sets it apart from the authored Routines it sits
// among without being a kind of its own.
var dashboardBadgeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))

// routineIDCellPlain is the id cell with its badge, unstyled.
func routineIDCellPlain(row DashboardRow) string {
	if row.Badge == "" {
		return row.ID
	}
	return row.Badge + " " + row.ID
}

// routineIDCellStyled is the rendered id cell: a badged row carries the badge
// colour across badge and id, an unbadged one is plain.
func routineIDCellStyled(row DashboardRow) string {
	if row.Badge == "" {
		return row.ID
	}
	return dashboardBadgeStyle.Render(row.Badge + " " + row.ID)
}

// headers is this page's column header row, asked of its primary kind. A page
// whose primary kind is not wired shows no header rather than a guess — the same
// rule kindFor follows for cells — and the widths below still size to the cells.
func (p dashboardPage) headers(kinds workKinds) []string {
	return kinds.columns(p.primary)
}

// columnWidths precomputes each column's natural width over the full row set,
// floored at the header label width.
func (p dashboardPage) columnWidths(kinds workKinds, rows []DashboardRow) []int {
	return p.widthsOver(p.plainCells, kinds, rows)
}

// statusWidths is the same measurement over the cells the static status table
// prints, so that surface is never sized by a projection it does not render.
func (p dashboardPage) statusWidths(kinds workKinds, rows []DashboardRow) []int {
	return p.widthsOver(p.statusCells, kinds, rows)
}

func (p dashboardPage) widthsOver(cells func(workKinds, DashboardRow) []string, kinds workKinds, rows []DashboardRow) []int {
	widths := headerWidths(p.headers(kinds))
	for _, row := range rows {
		for i, v := range cells(kinds, row) {
			widths = growWidths(widths, i+1)
			if n := lipgloss.Width(v); n > widths[i] {
				widths[i] = n
			}
		}
	}
	return widths
}

// fitWidths shrinks this page's elastic columns until the table fits budget,
// never below a column's header label. When the budget is still exceeded, cells
// truncate at render time via padDashboardCell.
func (p dashboardPage) fitWidths(kinds workKinds, natural []int, budget int) []int {
	if budget <= 0 || len(natural) == 0 {
		return append([]int(nil), natural...)
	}
	widths := append([]int(nil), natural...)
	mins := growWidths(headerWidths(p.headers(kinds)), len(widths))
	for dashboardTableLineWidth(widths) > budget {
		shrunk := false
		for _, col := range p.shrinkOrder {
			if col < len(widths) && widths[col] > mins[col] {
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

// twoLine reports whether this page folds each row onto two lines at the current
// pane size. A page whose columns were never designed for it never folds.
func (p dashboardPage) twoLine(rows []DashboardRow, termWidth, termHeight int) bool {
	return p.twoLineCapable && dashboardTwoLineMode(rows, termWidth, termHeight)
}

// headerWidths seeds the column widths with each header label's own width.
func headerWidths(headers []string) []int {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	return widths
}

// growWidths extends widths to at least n columns. A kind may author more cells
// than it declares headers for (a fake kind in a test declares none), and a
// missing header must cost a column its rendering, not a panic.
func growWidths(widths []int, n int) []int {
	for len(widths) < n {
		widths = append(widths, 0)
	}
	return widths
}
