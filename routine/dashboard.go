package routine

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/ui"
)

const dashboardPollInterval = 2 * time.Second

// DashboardRow is one read-only Routine dashboard table row.
type DashboardRow struct {
	ID        string
	Directory string
	Schedule  string
	LastRun   string
	Status    string
	Paused    bool
	// LastReportPath is the absolute path of the routine's most recent run
	// report. Empty when the routine has never fired or its latest run has
	// no report (e.g. skipped).
	LastReportPath string
}

// DashboardSnapshot is the data model for `pop routine dashboard`.
type DashboardSnapshot struct {
	Rows []DashboardRow
	// Warnings names routines whose manifest could not be loaded; healthy
	// rows still render.
	Warnings []string
}

// BuildDashboard derives Routine dashboard rows from on-disk routine artifacts
// and the execution-state store.
func BuildDashboard(d *Deps) (DashboardSnapshot, error) {
	return BuildDashboardWith(d)
}

// BuildDashboardWith is the injectable BuildDashboard entry point.
func BuildDashboardWith(d *Deps) (DashboardSnapshot, error) {
	if d == nil {
		d = DefaultDeps()
	}
	if _, err := ReconcileRunsWith(d); err != nil {
		return DashboardSnapshot{}, err
	}
	routines, warnings, err := ListRoutines(d)
	if err != nil {
		return DashboardSnapshot{}, err
	}
	s, ok, err := openExecutionStoreIfExists(d)
	if err != nil {
		return DashboardSnapshot{}, err
	}
	if ok {
		defer func() { _ = s.Close() }()
	}

	rows := make([]DashboardRow, 0, len(routines))
	for _, r := range routines {
		row := DashboardRow{
			ID:        r.ID,
			Directory: r.Manifest.BoundDirectory,
			Schedule:  r.Manifest.Schedule,
			Paused:    r.Manifest.Paused,
		}
		if s != nil {
			last, err := LastFireTime(s, r.ID)
			if err != nil {
				return DashboardSnapshot{}, err
			}
			row.LastRun = formatLastRun(last)
			row.Status, row.LastReportPath = dashboardStatusFor(d, s, r)
		} else {
			row.LastRun = "never"
			row.Status = dashboardIdleStatus(r.Manifest, "")
		}
		rows = append(rows, row)
	}
	snap := DashboardSnapshot{Rows: rows}
	for _, w := range warnings {
		snap.Warnings = append(snap.Warnings, fmt.Sprintf("routine %s: %v", w.ID, w.Err))
	}
	return snap, nil
}

func formatLastRun(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return t.UTC().Format("2006-01-02 15:04")
}

// dashboardStatusFor derives a row's STATUS cell alongside the absolute path
// of its latest run's report (empty when never-fired, skipped, or still
// running — the running row itself has no report yet).
func dashboardStatusFor(d *Deps, s *store.Store, r *Routine) (string, string) {
	last, lastErr := s.LastRoutineRun(r.ID)
	reportPath := ""
	if lastErr == nil && last != nil {
		reportPath = last.ReportPath
	}
	live, err := s.LiveRoutineRun(r.ID, func(run store.RoutineRun) bool {
		return routineProcessAlive(d, run.PID, run.ProcStart)
	})
	if err != nil {
		return dashboardIdleStatus(r.Manifest, ""), reportPath
	}
	if live != nil {
		return "running", reportPath
	}
	outcome := ""
	if lastErr == nil && last != nil {
		outcome = last.Outcome
	}
	return dashboardIdleStatus(r.Manifest, outcome), reportPath
}

// dashboardIdleStatus renders the STATUS cell for a Routine that is not live.
// A pause wins over everything; otherwise an idle Routine surfaces its last
// run's terminal outcome (ok/failed) and a never-fired Routine stays plain idle.
func dashboardIdleStatus(m Manifest, lastOutcome string) string {
	if m.Paused {
		return pausedStatusLabel(m.PauseReason)
	}
	switch lastOutcome {
	case store.RoutineRunSucceeded:
		return "ok"
	case store.RoutineRunFailed:
		return "failed"
	}
	return "idle"
}

type dashboardColumns struct {
	natural []int
	widths  []int
	width   int
}

type runsDetailView struct {
	row     DashboardRow
	runs    []store.RoutineRun
	list    *ui.List[store.RoutineRun]
	loading bool
	err     error
	peek    *reportPeek
	status  string
}

type reportPeek struct {
	path    string
	text    string
	loading bool
	err     error
	scroll  int
	status  string
}

type RoutineDashboard struct {
	d         *Deps
	snap      DashboardSnapshot
	allRows   []DashboardRow
	list      *ui.List[DashboardRow]
	cols      *dashboardColumns
	err       error
	width     int
	height    int
	detail    *runsDetailView
	menu      *routineDashboardMenu
	sched     *routineScheduleModal
	statusMsg string
	showHelp  bool
	pendingG  bool

	// copyFunc performs the clipboard write for the `c` verb. Injected so
	// tests can avoid touching the real tmux / /dev/tty. Defaults to
	// ui.CopyToClipboard.
	copyFunc func(string) error
}

// clipboardCopy returns the model's copy function, defaulting to
// ui.CopyToClipboard.
func (m RoutineDashboard) clipboardCopy() func(string) error {
	if m.copyFunc != nil {
		return m.copyFunc
	}
	return ui.CopyToClipboard
}

// noReportStatusMsg is shown when the `c` verb has no report path to copy
// (never-fired routine, or a skipped run).
const noReportStatusMsg = "no report to copy"

// copyReportPath is the shared `c`-verb handler for all three dashboard
// levels: a no-op with a status note when path is empty, otherwise a
// clipboard write through the shared tmux/OSC52 helper with a brief
// confirmation (or the error) as the returned status text.
func (m RoutineDashboard) copyReportPath(path string) string {
	if path == "" {
		return noReportStatusMsg
	}
	if err := m.clipboardCopy()(path); err != nil {
		return fmt.Sprintf("copy failed: %v", err)
	}
	return "copied report path"
}

// routineMenuAction identifies the verb a routine action-menu item dispatches.
type routineMenuAction int

const (
	menuActionFire routineMenuAction = iota
	menuActionPauseResume
	menuActionPreview
	menuActionEditPrompt
	menuActionEditSchedule
	menuActionRefine
	menuActionRuns
)

// routineMenuItem is one verb in the action overlay: the flat shortcut letter it
// keeps, the label shown beside it, and the verb it dispatches.
type routineMenuItem struct {
	key    string
	label  string
	action routineMenuAction
}

// routineDashboardMenu is the layered action overlay opened with `a` over the
// focused routine row. It carries the snapshot of the row it was opened on and
// the verbs applicable to that row on a ui.List whose cursor drives j/k + Enter
// selection.
type routineDashboardMenu struct {
	row  DashboardRow
	list *ui.List[routineMenuItem]
}

// routineMenuItems returns the verbs applicable to row, in a stable order. The
// pause/resume label reflects the row's current paused state. New verbs (e.g.
// the edit-schedule modal added in a later slice) append here.
func routineMenuItems(row DashboardRow) []routineMenuItem {
	pauseLabel := "pause"
	if row.Paused {
		pauseLabel = "resume"
	}
	return []routineMenuItem{
		{key: "i", label: "fire now", action: menuActionFire},
		{key: "a", label: pauseLabel, action: menuActionPauseResume},
		{key: "p", label: "preview pane", action: menuActionPreview},
		{key: "e", label: "edit prompt", action: menuActionEditPrompt},
		{key: "s", label: "edit schedule", action: menuActionEditSchedule},
		{key: "r", label: "refine", action: menuActionRefine},
		{key: "l", label: "runs", action: menuActionRuns},
	}
}

// routineScheduleModal is the text-input overlay opened by the edit-schedule
// verb. It carries the row it edits, the working schedule expression (pre-filled
// with the row's current schedule), and the last parse error so an invalid
// expression keeps the modal open for correction (mirrors the Queue dashboard's
// bind-name text-input stage).
type routineScheduleModal struct {
	row   DashboardRow
	input string
	err   error
}

// newRoutineDashboardMenu opens the action overlay on row, wrapping its verbs in
// a ui.List with j/k wrap-around navigation.
func newRoutineDashboardMenu(row DashboardRow) *routineDashboardMenu {
	return &routineDashboardMenu{
		row:  row,
		list: ui.NewList(routineMenuItems(row), ui.Opts[routineMenuItem]{Wrap: true}),
	}
}

// NewDashboard constructs a Routine dashboard model from a snapshot.
func NewDashboard(d *Deps, snap DashboardSnapshot) RoutineDashboard {
	return newRoutineDashboard(d, snap)
}

func newRoutineDashboard(d *Deps, snap DashboardSnapshot) RoutineDashboard {
	if d == nil {
		d = DefaultDeps()
	}
	cols := &dashboardColumns{}
	cols.syncNatural(snap.Rows)
	list := ui.NewList(snap.Rows, ui.Opts[DashboardRow]{
		Key:    func(r DashboardRow) string { return r.ID },
		Anchor: ui.AnchorTop,
		Cell: func(r DashboardRow, rs ui.RowState) string {
			return ui.TruncateString(
				dashboardTableLine(dashboardRowValues(r), cols.widths),
				dashboardListCellBudget(cols.width),
			)
		},
	})
	return RoutineDashboard{d: d, snap: snap, allRows: snap.Rows, list: list, cols: cols}
}

const (
	dashboardColRoutine = iota
	dashboardColDirectory
	dashboardColSchedule
	dashboardColLastRun
	dashboardColStatus
)

const dashboardColSep = 2

var dashboardColShrinkOrder = []int{
	dashboardColDirectory,
	dashboardColSchedule,
	dashboardColLastRun,
	dashboardColRoutine,
	dashboardColStatus,
}

func dashboardTableHeaders() []string {
	return []string{"ROUTINE", "DIRECTORY", "SCHEDULE", "LAST RUN", "STATUS"}
}

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

func dashboardListCellBudget(termWidth int) int {
	if termWidth > 2 {
		return termWidth - 2
	}
	return termWidth
}

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

const dashboardTableChromeLines = 3

func (m RoutineDashboard) dashboardChromeLines() int {
	return dashboardTableChromeLines
}

func (m RoutineDashboard) syncListRows() {
	m.list.ReplaceItems(m.snap.Rows)
	m.cols.syncNatural(m.snap.Rows)
}

func (m RoutineDashboard) resizeMainList() {
	listH := m.frameSpec().BodyHeight(m.height) - m.dashboardChromeLines()
	if listH < 1 {
		listH = 1
	}
	m.list.SetLinesPerItem(1)
	m.list.Resize(listH)
}

// ViewToggleAllowed reports whether v may switch to the Queue dashboard.
func (m RoutineDashboard) ViewToggleAllowed() bool {
	return m.detail == nil && m.menu == nil && m.sched == nil
}

// ListCursor exposes the main-list cursor index for tests.
func (m RoutineDashboard) ListCursor() int {
	return m.list.Cursor()
}

func (m RoutineDashboard) Init() tea.Cmd {
	return dashboardTick()
}

func (m RoutineDashboard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if kpm, ok := msg.(tea.KeyPressMsg); ok {
			if ui.ToggleHelp(&m.showHelp, kpm) {
				return m, nil
			}
			if m.showHelp {
				return m, nil
			}
		}
		if m.sched != nil {
			return m.updateScheduleModal(msg)
		}
		if m.menu != nil {
			return m.updateMenu(msg)
		}
		if m.detail != nil {
			return m.updateDetailView(msg)
		}
		if msg.String() == "g" {
			if m.pendingG {
				m.pendingG = false
				m.list.SetCursor(0)
			} else {
				m.pendingG = true
			}
			return m, nil
		}
		m.pendingG = false
		switch msg.String() {
		case "ctrl+c", "esc", "h", "left":
			return m, tea.Quit
		case "j", "down":
			m.list.MoveDown()
		case "k", "up":
			m.list.MoveUp()
		case "G":
			m.list.SetCursor(len(m.snap.Rows) - 1)
		case "i":
			row, ok := m.list.Selected()
			if !ok {
				return m, nil
			}
			m.statusMsg = ""
			return m, m.fireRoutine(row)
		case "a":
			row, ok := m.list.Selected()
			if !ok {
				return m, nil
			}
			m.statusMsg = ""
			m.menu = newRoutineDashboardMenu(row)
			return m, nil
		case "p":
			row, ok := m.list.Selected()
			if !ok {
				return m, nil
			}
			m.statusMsg = ""
			return m, m.previewRoutine(row)
		case "l", "enter":
			row, ok := m.list.Selected()
			if !ok {
				return m, nil
			}
			m.err = nil
			m.detail = newRunsDetailView(row)
			return m, m.loadRuns(row)
		case "c":
			row, ok := m.list.Selected()
			if !ok {
				return m, nil
			}
			m.statusMsg = m.copyReportPath(row.LastReportPath)
			return m, nil
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.cols.width = msg.Width
		m.cols.refit()
		m.resizeMainList()
	case dashboardTickMsg:
		cmds := []tea.Cmd{dashboardTick(), m.reload()}
		if m.detail != nil {
			cmds = append(cmds, m.loadRuns(m.detail.row))
		}
		return m, tea.Batch(cmds...)
	case dashboardRowsMsg:
		m.err = msg.err
		if msg.err == nil {
			m.allRows = msg.snap.Rows
			m.snap = msg.snap
			m.syncListRows()
			if m.detail != nil {
				for _, row := range m.snap.Rows {
					if row.ID == m.detail.row.ID {
						m.detail.row = row
						break
					}
				}
			}
		}
	case dashboardFireMsg:
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.statusMsg = fmt.Sprintf("fired %s", msg.id)
		}
		return m, m.reload()
	case dashboardTogglePauseMsg:
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.statusMsg = fmt.Sprintf("toggled pause for %s", msg.id)
		}
		return m, m.reload()
	case dashboardEditMsg:
		if msg.err != nil {
			m.err = msg.err
		} else {
			// Editing prompt.md via the dashboard verb is a run-affecting edit
			// chokepoint: pause with reason `changed` (ADR-0128).
			d := m.d
			if d == nil {
				d = DefaultDeps()
			}
			if err := pauseChanged(d, msg.id); err != nil {
				m.err = err
			} else {
				m.statusMsg = fmt.Sprintf("edited prompt for %s", msg.id)
			}
		}
		return m, m.reload()
	case dashboardRefineMsg:
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.statusMsg = fmt.Sprintf("refined %s", msg.id)
		}
		return m, m.reload()
	case dashboardPreviewMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		return m, tea.Quit
	case dashboardRunsMsg:
		if m.detail == nil {
			return m, nil
		}
		if msg.err != nil {
			m.detail.loading = false
			m.detail.err = msg.err
			return m, nil
		}
		m.detail.loading = false
		m.detail.err = nil
		m.detail.runs = msg.runs
		m.detail.list.ReplaceItems(msg.runs)
	case dashboardReportMsg:
		if m.detail == nil || m.detail.peek == nil {
			return m, nil
		}
		m.detail.peek.loading = false
		m.detail.peek.path = msg.path
		m.detail.peek.text = msg.text
		m.detail.peek.err = msg.err
	}
	return m, nil
}

func (m RoutineDashboard) updateDetailView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.detail != nil && m.detail.peek != nil {
		if msg.String() == "g" {
			if m.pendingG {
				m.pendingG = false
				m.detail.peek.scroll = 0
			} else {
				m.pendingG = true
			}
			return m, nil
		}
		m.pendingG = false
		switch msg.String() {
		case "esc", "h", "left":
			m.detail.peek = nil
		case "ctrl+c":
			return m, tea.Quit
		case "j", "down":
			m.moveReportPeek(1)
		case "k", "up":
			m.moveReportPeek(-1)
		case "G":
			m.detail.peek.scroll = m.maxReportPeekScroll()
		case "c":
			if !m.detail.peek.loading {
				m.detail.peek.status = m.copyReportPath(m.detail.peek.path)
			}
		}
		return m, nil
	}
	if msg.String() == "g" {
		if m.pendingG {
			m.pendingG = false
			if m.detail != nil {
				m.detail.list.SetCursor(0)
			}
		} else {
			m.pendingG = true
		}
		return m, nil
	}
	m.pendingG = false
	switch msg.String() {
	case "esc", "h", "left":
		m.detail = nil
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		if m.detail != nil {
			m.detail.list.MoveDown()
		}
	case "k", "up":
		if m.detail != nil {
			m.detail.list.MoveUp()
		}
	case "G":
		if m.detail != nil {
			m.detail.list.SetCursor(len(m.detail.runs) - 1)
		}
	case "l", "enter":
		if m.detail == nil || m.detail.loading {
			return m, nil
		}
		run, ok := m.detail.list.Selected()
		if !ok {
			return m, nil
		}
		m.detail.peek = &reportPeek{loading: true}
		return m, m.loadReport(m.detail.row.ID, run)
	case "c":
		if m.detail == nil || m.detail.loading {
			return m, nil
		}
		run, ok := m.detail.list.Selected()
		if !ok {
			return m, nil
		}
		m.detail.status = m.copyReportPath(run.ReportPath)
	}
	return m, nil
}

func (m RoutineDashboard) moveReportPeek(delta int) {
	if m.detail == nil || m.detail.peek == nil {
		return
	}
	m.detail.peek.scroll += delta
	max := m.maxReportPeekScroll()
	if m.detail.peek.scroll < 0 {
		m.detail.peek.scroll = 0
	}
	if m.detail.peek.scroll > max {
		m.detail.peek.scroll = max
	}
}

func (m RoutineDashboard) maxReportPeekScroll() int {
	if m.detail == nil || m.detail.peek == nil {
		return 0
	}
	lines := reportPeekLines(m.detail.peek.text)
	page := reportPeekPageSize(m.height, m.detail.peek.path)
	maxScroll := len(lines) - page
	if maxScroll < 0 {
		return 0
	}
	return maxScroll
}

func (m RoutineDashboard) reload() tea.Cmd {
	return func() tea.Msg {
		snap, err := BuildDashboardWith(m.d)
		return dashboardRowsMsg{snap: snap, err: err}
	}
}

func (m RoutineDashboard) fireRoutine(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		d := m.d
		if d == nil {
			d = DefaultDeps()
		}
		err := FirePaneWith(d, row.ID)
		return dashboardFireMsg{id: row.ID, err: err}
	}
}

func (m RoutineDashboard) togglePause(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		d := m.d
		if d == nil {
			d = DefaultDeps()
		}
		var err error
		if row.Paused {
			_, err = ResumeWith(d, row.ID)
		} else {
			_, err = PauseWith(d, row.ID)
		}
		return dashboardTogglePauseMsg{id: row.ID, err: err}
	}
}

// updateMenu drives the action overlay: esc/ctrl+c close it, j/k move the
// highlight, Enter runs the highlighted verb, and any matching verb letter runs
// that verb directly. Non-matching keys are inert while the menu is open.
func (m RoutineDashboard) updateMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.menu == nil {
		return m, nil
	}
	switch msg.String() {
	case "esc", "ctrl+c":
		m.menu = nil
		return m, nil
	case "j", "down":
		m.menu.list.MoveDown()
		return m, nil
	case "k", "up":
		m.menu.list.MoveUp()
		return m, nil
	case "enter":
		return m.invokeMenuItem(m.menu.list.Cursor())
	}
	for i, item := range m.menu.list.Items() {
		if msg.String() == item.key {
			return m.invokeMenuItem(i)
		}
	}
	return m, nil
}

// invokeMenuItem closes the menu and dispatches the verb at idx against the row
// the menu was opened on.
func (m RoutineDashboard) invokeMenuItem(idx int) (tea.Model, tea.Cmd) {
	if m.menu == nil {
		return m, nil
	}
	items := m.menu.list.Items()
	if idx < 0 || idx >= len(items) {
		return m, nil
	}
	item := items[idx]
	row := m.menu.row
	m.menu = nil
	return m.dispatchMenuAction(item.action, row)
}

// dispatchMenuAction runs the selected verb against row.
func (m RoutineDashboard) dispatchMenuAction(action routineMenuAction, row DashboardRow) (tea.Model, tea.Cmd) {
	m.statusMsg = ""
	switch action {
	case menuActionFire:
		return m, m.fireRoutine(row)
	case menuActionPauseResume:
		return m, m.togglePause(row)
	case menuActionPreview:
		return m, m.previewRoutine(row)
	case menuActionEditPrompt:
		return m, m.editPrompt(row)
	case menuActionEditSchedule:
		m.sched = &routineScheduleModal{row: row, input: row.Schedule}
		return m, nil
	case menuActionRefine:
		return m, m.refineRoutine(row)
	case menuActionRuns:
		m.err = nil
		m.detail = newRunsDetailView(row)
		return m, m.loadRuns(row)
	}
	return m, nil
}

// editPrompt suspends the TUI into $EDITOR on the routine's prompt.md via
// tea.ExecProcess, returning a status message on return. There is no gate
// against a live run: the next fire picks up the edited prompt.
func (m RoutineDashboard) editPrompt(row DashboardRow) tea.Cmd {
	d := m.d
	if d == nil {
		d = DefaultDeps()
	}
	cmd := editPromptCommand(d, row.ID)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return dashboardEditMsg{id: row.ID, err: err}
	})
}

// editPromptCommand builds the external editor invocation on the routine's
// prompt.md. The editor is read from $EDITOR, falling back to vi.
func editPromptCommand(d *Deps, id string) *exec.Cmd {
	if d == nil {
		d = DefaultDeps()
	}
	editor := strings.TrimSpace(os.Getenv("EDITOR"))
	if editor == "" {
		editor = "vi"
	}
	promptPath := filepath.Join(routineDir(d, id), promptFileName)
	return exec.Command(editor, promptPath)
}

// refineRoutine suspends the TUI into the Routine refinement gate for the row
// (ADR-0125), the same suspend mechanism the edit-prompt verb uses. The gate is
// a foreground Go loop rather than an external process, so it rides on tea.Exec
// via refineExec: bubbletea hands the resumed terminal's std streams to the
// wrapper, which drives RefineWith and then returns control to the dashboard. On
// return a reload refreshes the rows, since a resume or fire inside the gate
// may have changed the row's pause state or last run.
func (m RoutineDashboard) refineRoutine(row DashboardRow) tea.Cmd {
	d := m.d
	if d == nil {
		d = DefaultDeps()
	}
	return tea.Exec(&refineExec{d: d, id: row.ID}, func(err error) tea.Msg {
		return dashboardRefineMsg{id: row.ID, err: err}
	})
}

// refineExec adapts the RefineWith gate to tea.ExecCommand so bubbletea can run
// it in the foreground with the terminal restored. bubbletea injects the
// resumed terminal's streams via the setters; Run threads them into a shallow
// deps copy (leaving the dashboard's own deps untouched) and marks the session
// interactive, since the suspended TUI implies a TTY.
type refineExec struct {
	d   *Deps
	id  string
	in  io.Reader
	out io.Writer
}

func (e *refineExec) SetStdin(r io.Reader)  { e.in = r }
func (e *refineExec) SetStdout(w io.Writer) { e.out = w }
func (e *refineExec) SetStderr(io.Writer)   {}

func (e *refineExec) Run() error {
	d := *e.d
	d.Stdin = e.in
	d.Stdout = e.out
	d.IsInteractive = func() bool { return true }
	return RefineWith(&d, e.id, "")
}

// updateScheduleModal drives the edit-schedule text-input overlay: esc/ctrl+c
// cancel without writing, backspace deletes the last rune, enter validates and
// persists, and any printable key (spaces included, so "every 6h" and cron
// expressions type cleanly) extends the working expression.
func (m RoutineDashboard) updateScheduleModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.sched == nil {
		return m, nil
	}
	switch msg.String() {
	case "esc", "ctrl+c":
		m.sched = nil
		return m, nil
	case "backspace":
		if r := []rune(m.sched.input); len(r) > 0 {
			m.sched.input = string(r[:len(r)-1])
		}
		return m, nil
	case "enter":
		return m.confirmScheduleModal()
	}
	if kpm, ok := msg.(tea.KeyPressMsg); ok && kpm.Text != "" {
		m.sched.input += kpm.Text
	}
	return m, nil
}

// confirmScheduleModal validates the working expression through the schedule
// parser and, only if it parses, persists it via the shared UpdateScheduleWith
// helper (the same read-modify-write the CLI edit uses). On success the modal
// closes, a status message confirms, and a reload refreshes the row's SCHEDULE
// column. On parse failure the manifest is left untouched and the modal stays
// open showing the inline error for re-editing.
func (m RoutineDashboard) confirmScheduleModal() (tea.Model, tea.Cmd) {
	if m.sched == nil {
		return m, nil
	}
	d := m.d
	if d == nil {
		d = DefaultDeps()
	}
	row := m.sched.row
	mani, err := UpdateScheduleWith(d, row.ID, m.sched.input)
	if err != nil {
		m.sched.err = err
		return m, nil
	}
	m.sched = nil
	m.statusMsg = fmt.Sprintf("updated schedule for %s to %s", row.ID, mani.Schedule)
	return m, m.reload()
}

func (m RoutineDashboard) previewRoutine(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		d := m.d
		if d == nil {
			d = DefaultDeps()
		}
		err := PreviewPaneWith(d, row.ID)
		return dashboardPreviewMsg{err: err}
	}
}

func (m RoutineDashboard) loadRuns(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		d := m.d
		if d == nil {
			d = DefaultDeps()
		}
		s, err := openExecutionStore(d)
		if err != nil {
			return dashboardRunsMsg{err: err}
		}
		defer func() { _ = s.Close() }()
		runs, err := s.ListRoutineRuns(row.ID)
		return dashboardRunsMsg{runs: runs, err: err}
	}
}

func (m RoutineDashboard) loadReport(routineID string, run store.RoutineRun) tea.Cmd {
	return func() tea.Msg {
		d := m.d
		if d == nil {
			d = DefaultDeps()
		}
		path := run.ReportPath
		if path == "" {
			path = filepath.Join(routineDir(d, routineID), runsDirName,
				run.FiredAt.UTC().Format("2006-01-02T15-04-05Z")+".md")
		}
		data, err := d.FS.ReadFile(path)
		if err != nil {
			return dashboardReportMsg{path: path, err: err}
		}
		return dashboardReportMsg{path: path, text: string(data)}
	}
}

func newRunsDetailView(row DashboardRow) *runsDetailView {
	d := &runsDetailView{row: row, loading: true}
	d.list = ui.NewList([]store.RoutineRun{}, ui.Opts[store.RoutineRun]{
		Key: func(r store.RoutineRun) string {
			return fmt.Sprintf("%d", r.ID)
		},
		Anchor: ui.AnchorTop,
		Cell: func(r store.RoutineRun, _ ui.RowState) string {
			return runDetailLine(r)
		},
	})
	return d
}

func runDetailLine(r store.RoutineRun) string {
	stamp := r.FiredAt.UTC().Format("2006-01-02 15:04:05")
	outcome := r.Outcome
	switch r.Outcome {
	case store.RoutineRunSkipped:
		if r.SkipReason != "" {
			outcome += " (" + r.SkipReason + ")"
		}
	case store.RoutineRunFailed:
		if r.FailReason != "" {
			outcome += " (" + r.FailReason + ")"
		}
	}
	return fmt.Sprintf("%s  %s", stamp, outcome)
}

type dashboardTickMsg struct{}
type dashboardRowsMsg struct {
	snap DashboardSnapshot
	err  error
}
type dashboardFireMsg struct {
	id  string
	err error
}
type dashboardTogglePauseMsg struct {
	id  string
	err error
}
type dashboardEditMsg struct {
	id  string
	err error
}
type dashboardRefineMsg struct {
	id  string
	err error
}
type dashboardPreviewMsg struct {
	err error
}
type dashboardRunsMsg struct {
	runs []store.RoutineRun
	err  error
}
type dashboardReportMsg struct {
	path string
	text string
	err  error
}

func dashboardTick() tea.Cmd {
	return tea.Tick(dashboardPollInterval, func(time.Time) tea.Msg { return dashboardTickMsg{} })
}

func (m RoutineDashboard) helpEntries() []ui.HelpEntry {
	if m.sched != nil {
		return []ui.HelpEntry{
			{Key: "type", Desc: "edit schedule"},
			{Key: "enter", Desc: "save schedule"},
			{Key: "esc", Desc: "cancel"},
		}
	}
	if m.menu != nil {
		return []ui.HelpEntry{
			{Key: "i", Desc: "fire now"},
			{Key: "a", Desc: "pause/resume"},
			{Key: "p", Desc: "preview pane"},
			{Key: "e", Desc: "edit prompt"},
			{Key: "s", Desc: "edit schedule"},
			{Key: "r", Desc: "refine"},
			{Key: "l", Desc: "runs"},
			{Key: "j/k", Desc: "navigate"},
			{Key: "enter", Desc: "run action"},
			{Key: "esc", Desc: "close menu"},
		}
	}
	if m.detail != nil && m.detail.peek != nil {
		return []ui.HelpEntry{
			{Key: "j/k", Desc: "scroll report"},
			{Key: "gg", Desc: "top"},
			{Key: "G", Desc: "bottom"},
			{Key: "c", Desc: "copy report path"},
			{Key: "h/esc", Desc: "close report"},
		}
	}
	if m.detail != nil {
		return []ui.HelpEntry{
			{Key: "j/k", Desc: "navigate runs"},
			{Key: "gg", Desc: "first run"},
			{Key: "G", Desc: "last run"},
			{Key: "l/enter", Desc: "view report"},
			{Key: "c", Desc: "copy report path"},
			{Key: "h/esc", Desc: "back to list"},
		}
	}
	return []ui.HelpEntry{
		{Key: "j/k", Desc: "navigate"},
		{Key: "gg", Desc: "first row"},
		{Key: "G", Desc: "last row"},
		{Key: "i", Desc: "fire now"},
		{Key: "a", Desc: "actions"},
		{Key: "p", Desc: "preview pane"},
		{Key: "l/enter", Desc: "open runs"},
		{Key: "c", Desc: "copy report path"},
		{Key: "v", Desc: "queue view"},
		{Key: "C-h", Desc: "toggle help"},
		{Key: "h/esc", Desc: "quit"},
	}
}

func (m RoutineDashboard) View() tea.View {
	if m.showHelp {
		title := "Help · Routines"
		if m.sched != nil {
			title = "Help · Routines · edit schedule"
		} else if m.menu != nil {
			title = "Help · Routines · actions"
		} else if m.detail != nil && m.detail.peek != nil {
			title = "Help · Routines · report"
		} else if m.detail != nil {
			title = "Help · Routines · runs"
		}
		content := ui.RenderHelpOverlay(title, m.helpEntries(), m.width, m.height)
		v := tea.NewView(content)
		v.AltScreen = true
		return v
	}
	if m.detail != nil {
		content := m.viewDetail()
		v := tea.NewView(content)
		v.AltScreen = true
		return v
	}
	m.cols.width = m.width
	m.cols.refit()
	m.resizeMainList()
	if m.sched != nil {
		content := m.viewWithScheduleModal()
		v := tea.NewView(content)
		v.AltScreen = true
		return v
	}
	if m.menu != nil {
		content := m.viewWithMenu()
		v := tea.NewView(content)
		v.AltScreen = true
		return v
	}
	content := m.frameSpec().Render(m.mainBody())
	v := tea.NewView(content)
	v.AltScreen = true
	return v
}

// viewWithMenu renders the action-menu overlay: the summary, the full table with
// the menu block spliced under the cursored row, and a menu footer. It mirrors
// the Queue dashboard's overlay grammar.
func (m RoutineDashboard) viewWithMenu() string {
	var b strings.Builder
	if m.err != nil {
		fmt.Fprintf(&b, "refresh error: %v\n", m.err)
	}
	fmt.Fprintf(&b, "Routines · %d\n\n", len(m.snap.Rows))
	if len(m.snap.Rows) == 0 {
		fmt.Fprintln(&b, emptyListHint)
		writeRoutineFooter(&b, m.height, ui.HintStyle.Render("j/k move · enter/letter run · esc close"))
		return b.String()
	}
	fmt.Fprintln(&b, ui.TruncateString("  "+dashboardTableLine(dashboardTableHeaders(), m.cols.widths), m.width))
	fmt.Fprintln(&b, ui.TruncateString("  "+dashboardTableSeparator(m.cols.widths), m.width))
	cursor := m.list.Cursor()
	for i, row := range m.snap.Rows {
		marker := "  "
		if i == cursor {
			marker = ui.IndicatorStyle.Render("█") + " "
		}
		cell := ui.TruncateString(dashboardTableLine(dashboardRowValues(row), m.cols.widths), dashboardListCellBudget(m.width))
		fmt.Fprintln(&b, marker+cell)
		if i == cursor {
			for _, ml := range routineMenuLines(m.menu, m.width) {
				fmt.Fprintln(&b, ml)
			}
		}
	}
	writeRoutineFooter(&b, m.height, ui.HintStyle.Render("j/k move · enter/letter run · esc close"))
	return b.String()
}

// routineMenuLines renders the action overlay as a block of lines indented to
// nest under the cursored row, with the highlighted item carrying the shared
// cursor block. The first line is a dimmed "actions" caption.
func routineMenuLines(menu *routineDashboardMenu, width int) []string {
	if menu == nil {
		return nil
	}
	lines := []string{ui.TruncateString("    "+ui.HintStyle.Render("actions"), width)}
	cursor := menu.list.Cursor()
	for i, item := range menu.list.Items() {
		marker := "  "
		if i == cursor {
			marker = ui.IndicatorStyle.Render("█") + " "
		}
		line := fmt.Sprintf("    %s%s  %s", marker, item.key, item.label)
		lines = append(lines, ui.TruncateString(line, width))
	}
	return lines
}

// viewWithScheduleModal renders the edit-schedule text-input overlay: the
// summary, the full table with the modal block spliced under the cursored row,
// and a modal footer. It mirrors viewWithMenu's overlay grammar.
func (m RoutineDashboard) viewWithScheduleModal() string {
	var b strings.Builder
	if m.err != nil {
		fmt.Fprintf(&b, "refresh error: %v\n", m.err)
	}
	fmt.Fprintf(&b, "Routines · %d\n\n", len(m.snap.Rows))
	hint := ui.HintStyle.Render("enter save · esc cancel")
	if len(m.snap.Rows) == 0 {
		fmt.Fprintln(&b, emptyListHint)
		writeRoutineFooter(&b, m.height, hint)
		return b.String()
	}
	fmt.Fprintln(&b, ui.TruncateString("  "+dashboardTableLine(dashboardTableHeaders(), m.cols.widths), m.width))
	fmt.Fprintln(&b, ui.TruncateString("  "+dashboardTableSeparator(m.cols.widths), m.width))
	cursor := m.list.Cursor()
	for i, row := range m.snap.Rows {
		marker := "  "
		if i == cursor {
			marker = ui.IndicatorStyle.Render("█") + " "
		}
		cell := ui.TruncateString(dashboardTableLine(dashboardRowValues(row), m.cols.widths), dashboardListCellBudget(m.width))
		fmt.Fprintln(&b, marker+cell)
		if i == cursor {
			for _, ml := range routineScheduleModalLines(m.sched, m.width) {
				fmt.Fprintln(&b, ml)
			}
		}
	}
	writeRoutineFooter(&b, m.height, hint)
	return b.String()
}

// routineScheduleModalLines renders the edit-schedule modal as a block of lines
// nested under the cursored row: a dimmed caption, the working expression, and,
// when the last enter failed to parse, the inline error kept for correction.
func routineScheduleModalLines(modal *routineScheduleModal, width int) []string {
	if modal == nil {
		return nil
	}
	lines := []string{
		ui.TruncateString("    "+ui.HintStyle.Render("edit schedule"), width),
		ui.TruncateString(fmt.Sprintf("    schedule: %s", modal.input), width),
	}
	if modal.err != nil {
		lines = append(lines, ui.TruncateString(fmt.Sprintf("    error: %v", modal.err), width))
	}
	return lines
}

func (m RoutineDashboard) frameSpec() ui.Frame {
	var warnings []string
	if m.err != nil {
		warnings = append(warnings, fmt.Sprintf("refresh error: %v", m.err))
	}
	warnings = append(warnings, m.snap.Warnings...)
	return ui.Frame{
		Width:    m.width,
		TermH:    m.height,
		Header:   fmt.Sprintf("Routines · %d", len(m.snap.Rows)),
		Warnings: warnings,
		Status:   m.statusMsg,
		Hints:    m.mainHint(),
	}
}

func (m RoutineDashboard) mainHint() string {
	return "j/k move · gg/G top/bottom · i fire · a actions · p preview · l/enter runs · c copy report · v queue · C-h help · h/esc quit"
}

func (m RoutineDashboard) mainBody() string {
	if len(m.snap.Rows) == 0 {
		return emptyListHint
	}
	parts := []string{
		"",
		ui.TruncateString("  "+dashboardTableLine(dashboardTableHeaders(), m.cols.widths), m.width),
		ui.TruncateString("  "+dashboardTableSeparator(m.cols.widths), m.width),
	}
	parts = append(parts, m.list.VisibleRows()...)
	return strings.Join(parts, "\n")
}

func (m RoutineDashboard) viewDetail() string {
	if m.detail.peek != nil {
		return m.viewReportPeek()
	}
	return m.viewRunsList()
}

func (m RoutineDashboard) viewRunsList() string {
	d := m.detail
	const backHint = "h/esc back"
	if d.loading {
		return ui.Frame{Width: m.width, TermH: m.height, Header: fmt.Sprintf("Runs · %s", d.row.ID), Hints: backHint}.
			Render(fmt.Sprintf("Loading runs for %s...", d.row.ID))
	}
	if d.err != nil {
		return ui.Frame{Width: m.width, TermH: m.height, Header: fmt.Sprintf("Runs · %s", d.row.ID), Hints: backHint}.
			Render(fmt.Sprintf("error loading runs: %v", d.err))
	}
	if len(d.runs) == 0 {
		return ui.Frame{Width: m.width, TermH: m.height, Header: fmt.Sprintf("Runs · %s", d.row.ID), Hints: backHint}.
			Render("  " + emptyRunsHint)
	}
	frame := ui.Frame{
		Width:  m.width,
		TermH:  m.height,
		Header: fmt.Sprintf("Runs · %s", d.row.ID),
		Status: d.status,
		Hints:  "j/k · gg/G top/bottom · l/enter report · c copy · h/esc back",
	}
	listH := frame.BodyHeight(m.height) - dashboardTableChromeLines
	if listH < 1 {
		listH = 1
	}
	d.list.Resize(listH)
	parts := []string{
		"",
		"  FIRED AT              OUTCOME",
		"  " + strings.Repeat("-", 40),
	}
	parts = append(parts, d.list.VisibleRows()...)
	return frame.Render(strings.Join(parts, "\n"))
}

func (m RoutineDashboard) viewReportPeek() string {
	p := m.detail.peek
	var b strings.Builder
	fmt.Fprintf(&b, "Report · %s\n", m.detail.row.ID)
	if p.loading {
		fmt.Fprintln(&b, "  loading report...")
		writeRoutineFooter(&b, m.height, ui.HintStyle.Render("  h/esc back"))
		return b.String()
	}
	if p.err != nil {
		fmt.Fprintf(&b, "  error loading report: %v\n", p.err)
		if p.path != "" {
			fmt.Fprintf(&b, "  %s\n", p.path)
		}
		writeRoutineFooter(&b, m.height, ui.HintStyle.Render("  h/esc back"))
		return b.String()
	}
	if p.path != "" {
		fmt.Fprintf(&b, "  %s\n\n", p.path)
	}
	lines := reportPeekLines(p.text)
	pageSize := reportPeekPageSize(m.height, p.path)
	maxScroll := len(lines) - pageSize
	if maxScroll < 0 {
		maxScroll = 0
	}
	if p.scroll > maxScroll {
		p.scroll = maxScroll
	}
	if len(lines) == 0 {
		fmt.Fprintln(&b, "  (empty report)")
	} else {
		end := p.scroll + pageSize
		if end > len(lines) {
			end = len(lines)
		}
		for _, line := range lines[p.scroll:end] {
			fmt.Fprintln(&b, ui.TruncateString(line, m.width))
		}
	}
	position := ""
	if maxScroll > 0 {
		position = fmt.Sprintf(" · %d/%d", p.scroll+1, len(lines))
	}
	if p.status != "" {
		fmt.Fprintf(&b, "  %s\n", p.status)
	}
	writeRoutineFooter(&b, m.height, ui.HintStyle.Render("  j/k scroll · gg/G top/bottom · c copy · h/esc back"+position))
	return b.String()
}

func reportPeekLines(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func reportPeekPageSize(height int, path string) int {
	if height <= 0 {
		height = 20
	}
	pathLines := 0
	if path != "" {
		pathLines = 2
	}
	pageSize := height - 1 - 1 - pathLines - 1
	if pageSize < 1 {
		return 1
	}
	return pageSize
}

func writeRoutineFooter(b *strings.Builder, height int, hint string) {
	if height <= 0 {
		fmt.Fprintln(b, hint)
		return
	}
	lines := strings.Count(b.String(), "\n")
	pad := height - lines - 1
	for i := 0; i < pad; i++ {
		fmt.Fprintln(b)
	}
	fmt.Fprintln(b, hint)
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

func dashboardRowValues(row DashboardRow) []string {
	return []string{
		row.ID,
		row.Directory,
		row.Schedule,
		row.LastRun,
		dashboardStatusStyled(row.Status),
	}
}

func dashboardRowNaturalValues(row DashboardRow) []string {
	return []string{
		row.ID,
		row.Directory,
		row.Schedule,
		row.LastRun,
		row.Status,
	}
}

func dashboardStatusStyled(status string) string {
	switch {
	case status == "running", status == "ok":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render(status)
	case status == "failed":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render(status)
	case strings.HasPrefix(status, "paused"):
		return lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render(status)
	default:
		return status
	}
}

// RunDashboard opens the Routine dashboard TUI.
func RunDashboard(d *Deps) error {
	snap, err := BuildDashboard(d)
	if err != nil {
		return err
	}
	m := newRoutineDashboard(d, snap)
	program := tea.NewProgram(m)
	_, err = program.Run()
	return err
}
