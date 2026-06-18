package queue

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/glebglazov/pop/binding"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
)

const dashboardPollInterval = 2 * time.Second

// DashboardRow is one read-only Queue dashboard table row.
type DashboardRow struct {
	Project  string
	SetID    string
	Status   string
	Worktree string
	Drain    string

	cursorKey string
}

// DashboardSnapshot is the data model for `pop queue dashboard`.
type DashboardSnapshot struct {
	Rows []DashboardRow
}

// BuildDashboard derives the Queue dashboard rows from registered projects and
// on-disk task/queue state. It is read-only except for the same refresh
// auto-registration behavior used by `pop queue status`.
func BuildDashboard(d *Deps, cfg *config.Config) (DashboardSnapshot, error) {
	if d == nil {
		d = DefaultDeps()
	}
	if d.Tasks == nil {
		d.Tasks = tasks.DefaultDeps()
	}
	if d.Project == nil {
		d.Project = project.DefaultDeps()
	}
	state, err := EnsureDaemonState(d.Tasks)
	if err != nil {
		return DashboardSnapshot{}, err
	}
	projects, err := tasks.ListPickerProjectsWith(d.Project, cfg)
	if err != nil {
		return DashboardSnapshot{}, err
	}

	var scans []projectScan
	for _, p := range projects {
		scan, err := resolveScan(d, p)
		if err != nil {
			if outsideQueueScopeResolveError(err) {
				continue
			}
			return DashboardSnapshot{}, err
		}
		scans = append(scans, scan)
	}

	groups := map[string][]projectScan{}
	var order []string
	for _, scan := range scans {
		if _, ok := groups[scan.DefinitionPath]; !ok {
			order = append(order, scan.DefinitionPath)
		}
		groups[scan.DefinitionPath] = append(groups[scan.DefinitionPath], scan)
	}

	var rows []DashboardRow
	for _, key := range order {
		group := groups[key]
		groupRows, err := dashboardRowsForRepo(d, cfg, state, group)
		if err != nil {
			return DashboardSnapshot{}, err
		}
		rows = append(rows, groupRows...)
	}
	sortDashboardRows(rows)
	return DashboardSnapshot{Rows: rows}, nil
}

func sortDashboardRows(rows []DashboardRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Project != rows[j].Project {
			return rows[i].Project < rows[j].Project
		}
		return rows[i].SetID > rows[j].SetID
	})
}

func dashboardRowsForRepo(d *Deps, cfg *config.Config, state *DaemonState, scans []projectScan) ([]DashboardRow, error) {
	if len(scans) == 0 {
		return nil, nil
	}
	refresh, err := d.refresh(scans[0].DefinitionPath)
	if err != nil {
		return nil, err
	}
	repoKey, err := scanRepoKey(d, scans[0])
	if err != nil {
		return nil, err
	}
	rep, bare, err := resolveRepresentative(d, cfg, scans)
	if err != nil {
		return nil, err
	}
	projectName := repoName(scans, rep)
	var rows []DashboardRow
	for _, taskRow := range refresh.Rows {
		merge, awaitingIntegration := mergeabilityForSet(state, repoKey, taskRow.ID)
		if !dashboardShowRow(taskRow, awaitingIntegration) {
			continue
		}
		wt := dashboardWorktree(d, state, repoKey, taskRow.ID, rep, bare)
		drain := dashboardDrain(d, state, repoKey, taskRow.ID, wt.runtimePath)
		status := dashboardStatus(taskRow.Status, merge, awaitingIntegration)
		rows = append(rows, DashboardRow{
			Project:   projectName,
			SetID:     taskRow.ID,
			Status:    status,
			Worktree:  wt.label,
			Drain:     drain,
			cursorKey: projectName + "\x00" + taskRow.ID,
		})
	}
	return rows, nil
}

func dashboardShowRow(row tasks.Row, awaitingIntegration bool) bool {
	return row.Status != tasks.StatusDone || awaitingIntegration
}

func dashboardStatus(status tasks.TaskSetStatus, rec MergeabilityRecord, awaitingIntegration bool) string {
	if status != tasks.StatusDone || !awaitingIntegration {
		return string(status)
	}
	switch rec.Status {
	case MergeabilityClean, MergeabilityConflicts:
		return string(tasks.StatusDone) + " · " + rec.Status
	default:
		return string(tasks.StatusDone) + " · unknown"
	}
}

func mergeabilityForSet(state *DaemonState, repoKey, setID string) (MergeabilityRecord, bool) {
	if state == nil || len(state.Mergeability) == 0 {
		return MergeabilityRecord{}, false
	}
	rec, ok := state.Mergeability[setScopedKey(repoKey, setID)]
	return rec, ok
}

type dashboardWorktreeView struct {
	label       string
	runtimePath string
}

func dashboardWorktree(d *Deps, state *DaemonState, repoKey, setID string, rep *projectScan, bare bool) dashboardWorktreeView {
	if state != nil {
		if b, ok := state.WorktreeBindings[setScopedKey(repoKey, setID)]; ok && strings.TrimSpace(b.RuntimePath) != "" {
			branch := b.Branch
			if branch == "" {
				branch = binding.CurrentBranch(d.Tasks, b.RuntimePath)
			}
			return dashboardWorktreeView{label: formatDashboardWorktree(b.RuntimePath, branch), runtimePath: b.RuntimePath}
		}
	}
	if rep != nil {
		branch := binding.CurrentBranch(d.Tasks, rep.RuntimePath)
		return dashboardWorktreeView{label: formatDashboardWorktree(rep.RuntimePath, branch), runtimePath: rep.RuntimePath}
	}
	if bare {
		return dashboardWorktreeView{label: "(no base)"}
	}
	return dashboardWorktreeView{label: "(no base)"}
}

func formatDashboardWorktree(path, branch string) string {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		branch = "detached"
	}
	return fmt.Sprintf("%s (%s)", path, branch)
}

func dashboardDrain(d *Deps, state *DaemonState, repoKey, setID, runtimePath string) string {
	paths := map[string]bool{}
	if runtimePath != "" {
		paths[runtimePath] = true
	}
	if state != nil {
		if b, ok := state.WorktreeBindings[setScopedKey(repoKey, setID)]; ok && b.RuntimePath != "" {
			paths[b.RuntimePath] = true
		}
	}
	for path := range paths {
		lock := d.readLock(path)
		if lock == nil || !lock.Locked || lock.Metadata == nil {
			continue
		}
		if lock.Metadata.SetID == setID {
			return "picked up"
		}
	}
	return ""
}

type dashboardTickMsg struct{}
type dashboardRowsMsg struct {
	snap DashboardSnapshot
	err  error
}

type dashboardModel struct {
	d      *Deps
	cfg    *config.Config
	snap   DashboardSnapshot
	cursor int
	err    error
	width  int
	height int
}

func newDashboardModel(d *Deps, cfg *config.Config, snap DashboardSnapshot) dashboardModel {
	return dashboardModel{d: d, cfg: cfg, snap: snap}
}

func (m dashboardModel) Init() tea.Cmd {
	return dashboardTick()
}

func (m dashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			return m, tea.Quit
		case "j", "down":
			if len(m.snap.Rows) > 0 && m.cursor < len(m.snap.Rows)-1 {
				m.cursor++
			}
		case "k", "up":
			if len(m.snap.Rows) > 0 && m.cursor > 0 {
				m.cursor--
			}
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case dashboardTickMsg:
		return m, tea.Batch(dashboardTick(), m.reload())
	case dashboardRowsMsg:
		oldKey := ""
		if m.cursor >= 0 && m.cursor < len(m.snap.Rows) {
			oldKey = m.snap.Rows[m.cursor].cursorKey
		}
		m.err = msg.err
		if msg.err == nil {
			m.snap = msg.snap
			m.cursor = dashboardCursorAfterReload(m.snap.Rows, oldKey, m.cursor)
		}
	}
	return m, nil
}

func (m dashboardModel) reload() tea.Cmd {
	return func() tea.Msg {
		snap, err := BuildDashboard(m.d, m.cfg)
		return dashboardRowsMsg{snap: snap, err: err}
	}
}

func dashboardTick() tea.Cmd {
	return tea.Tick(dashboardPollInterval, func(time.Time) tea.Msg { return dashboardTickMsg{} })
}

func dashboardCursorAfterReload(rows []DashboardRow, key string, previous int) int {
	if len(rows) == 0 {
		return 0
	}
	if key != "" {
		for i, row := range rows {
			if row.cursorKey == key {
				return i
			}
		}
	}
	if previous >= len(rows) {
		return len(rows) - 1
	}
	if previous < 0 {
		return 0
	}
	return previous
}

func (m dashboardModel) View() tea.View {
	var b strings.Builder
	title := lipgloss.NewStyle().Bold(true).Render("Queue dashboard")
	fmt.Fprintln(&b, title)
	if m.err != nil {
		fmt.Fprintf(&b, "refresh error: %v\n", m.err)
	}
	if len(m.snap.Rows) == 0 {
		fmt.Fprintln(&b, "No queue-actionable task sets.")
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "q quit")
		return tea.NewView(b.String())
	}
	renderDashboardTable(&b, m.snap.Rows, m.cursor)
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "j/k move · q quit")
	return tea.NewView(b.String())
}

func renderDashboardTable(w io.Writer, rows []DashboardRow, cursor int) {
	headers := []string{"project", "task set", "status", "worktree", "drain"}
	widths := []int{len(headers[0]), len(headers[1]), len(headers[2]), len(headers[3]), len(headers[4])}
	for _, row := range rows {
		values := []string{row.Project, row.SetID, row.Status, row.Worktree, row.Drain}
		for i, v := range values {
			if n := lipgloss.Width(v); n > widths[i] {
				widths[i] = n
			}
		}
	}
	fmt.Fprintf(w, "  %s\n", dashboardTableLine(headers, widths))
	for i, row := range rows {
		prefix := "  "
		if i == cursor {
			prefix = "> "
		}
		values := []string{row.Project, row.SetID, row.Status, row.Worktree, row.Drain}
		fmt.Fprintf(w, "%s%s\n", prefix, dashboardTableLine(values, widths))
	}
}

func dashboardTableLine(values []string, widths []int) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = padDashboardCell(v, widths[i])
	}
	return strings.Join(parts, "  ")
}

func padDashboardCell(s string, width int) string {
	if pad := width - lipgloss.Width(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}

// RunDashboard opens the read-only Queue dashboard TUI.
func RunDashboard(d *Deps, cfg *config.Config) error {
	snap, err := BuildDashboard(d, cfg)
	if err != nil {
		return err
	}
	program := tea.NewProgram(newDashboardModel(d, cfg, snap))
	_, err = program.Run()
	return err
}
