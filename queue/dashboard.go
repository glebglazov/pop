package queue

import (
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
	"github.com/glebglazov/pop/binding"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/ui"
)

const dashboardPollInterval = 2 * time.Second

// DashboardRow is one read-only Queue dashboard table row.
type DashboardRow struct {
	Project   string
	SetID     string
	Status    string
	Worktree  string
	Drain     string
	AutoDrain bool

	defPath            string
	statePath          string
	cursorKey          string
	repoKey            string
	runtimePath        string
	paneID             string
	integrationBacklog bool
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

	// Narrow to the registered projects whose repository has Task storage on
	// disk. The dashboard can only ever render task sets, which live in per-repo
	// Task storage, so resolving git coordinates for projects without any is pure
	// waste — those rows are filtered out downstream regardless. On a large
	// project list this is the difference between forking git for every checkout
	// each poll and forking only for the handful that hold task sets. Matching is
	// by canonical filesystem path, never a git fork (see docs/adr: queue
	// dashboard scoped to repositories with task storage).
	projects, err = dashboardCandidateProjects(d, projects)
	if err != nil {
		return DashboardSnapshot{}, err
	}
	if len(projects) == 0 {
		return DashboardSnapshot{}, nil
	}

	// Memoize idempotent git reads for this build, mirroring Scan: resolveScan
	// and resolveRepresentative re-fork rev-parse/worktree-list against the same
	// directories (once per project, then again per repo group). Wrap a shallow
	// copy so the caller's git is untouched. The dashboard rebuilds on a poll,
	// so the cache is per-build (a point-in-time snapshot), not cross-tick.
	if d.Tasks != nil && d.Tasks.Git != nil {
		buildDeps := *d
		tasksDeps := *d.Tasks
		tasksDeps.Git = newScanGitCache(d.Tasks.Git)
		buildDeps.Tasks = &tasksDeps
		d = &buildDeps
	}

	// Resolve every project's scan concurrently; with many registered checkouts
	// the serial git cost dominates wall-clock. resolveScan only reads, so the
	// bounded fan-out is safe (same pattern as Scan's phase 1).
	scanResults := make([]*projectScan, len(projects))
	scanErrs := make([]error, len(projects))
	sem := make(chan struct{}, scanConcurrency())
	var wg sync.WaitGroup
	for i, p := range projects {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, p project.ExpandedProject) {
			defer wg.Done()
			defer func() { <-sem }()
			scan, err := resolveScan(d, p)
			if err != nil {
				if !outsideQueueScopeResolveError(err) {
					scanErrs[idx] = err
				}
				return
			}
			scanResults[idx] = &scan
		}(i, p)
	}
	wg.Wait()

	var scans []projectScan
	for i := range projects {
		if scanErrs[i] != nil {
			return DashboardSnapshot{}, scanErrs[i]
		}
		if scanResults[i] != nil {
			scans = append(scans, *scanResults[i])
		}
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

// dashboardCandidateProjects keeps only the registered picker projects that
// belong to a repository with Task storage on disk. It resolves no git: each
// storage repo's working-tree root is derived from its marker's common dir and
// compared to canonical project paths by path nesting. A project matches when
// its checkout is the repo root, nests under it (a worktree), or contains it.
func dashboardCandidateProjects(d *Deps, projects []project.ExpandedProject) ([]project.ExpandedProject, error) {
	repos, err := tasks.ListTaskStorageRepos(d.Tasks)
	if err != nil {
		return nil, err
	}
	if len(repos) == 0 {
		return nil, nil
	}
	roots := make([]string, 0, len(repos))
	for _, r := range repos {
		roots = append(roots, storageRepoRoot(d.Tasks, r.RepositoryPath))
	}
	var kept []project.ExpandedProject
	for _, p := range projects {
		canon, err := canonicalCheckoutPath(d.Tasks, p.Path)
		if err != nil {
			canon = p.Path
		}
		for _, root := range roots {
			if pathWithinOrEqual(canon, root) || pathWithinOrEqual(root, canon) {
				kept = append(kept, p)
				break
			}
		}
	}
	return kept, nil
}

// storageRepoRoot derives a repository's working-tree root from the canonical
// git common directory recorded in its marker: a normal repo's common dir is
// `<root>/.git` and a bare-with-worktrees layout's is `<root>/.bare`, so the
// root is the parent; a top-level bare repo's common dir is the repo dir itself.
func storageRepoRoot(d *tasks.Deps, commonDir string) string {
	root := commonDir
	switch filepath.Base(commonDir) {
	case ".git", ".bare":
		root = filepath.Dir(commonDir)
	}
	if canon, err := canonicalCheckoutPath(d, root); err == nil {
		return canon
	}
	return root
}

// pathWithinOrEqual reports whether p is base or a descendant of base.
func pathWithinOrEqual(p, base string) bool {
	return p == base || strings.HasPrefix(p, base+string(filepath.Separator))
}

// worktreeBranchByPath parses `git worktree list --porcelain` into a map from
// each worktree's canonical path to its branch (empty when detached). One
// porcelain read — already cached for the build by scanGitCache — yields every
// checkout's branch, replacing a `git branch --show-current` fork per repo.
func worktreeBranchByPath(d *Deps, fromCheckout string) map[string]string {
	out, err := d.Tasks.Git.CommandInDir(fromCheckout, "worktree", "list", "--porcelain")
	if err != nil {
		return nil
	}
	branches := map[string]string{}
	path := ""
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			path = strings.TrimSpace(strings.TrimPrefix(line, "worktree "))
			if canon, err := canonicalCheckoutPath(d.Tasks, path); err == nil {
				path = canon
			}
		case strings.HasPrefix(line, "branch ") && path != "":
			branches[path] = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case line == "detached" && path != "":
			branches[path] = ""
		case line == "":
			path = ""
		}
	}
	return branches
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
	// The representative is shared by every row that lacks a per-set binding;
	// read its branch from the worktree-list porcelain — already fetched (and
	// cached) while resolving the representative — instead of forking `git branch
	// --show-current` per repo, which scanGitCache cannot memoize. The lookup
	// falls back to that fork only when the representative is not among the
	// repo's listed worktrees (e.g. a queue_base checkout outside it).
	repBranch := ""
	if rep != nil {
		if b, ok := worktreeBranchByPath(d, scans[0].ProjectPath)[rep.RuntimePath]; ok {
			repBranch = b
		} else {
			repBranch = binding.CurrentBranch(d.Tasks, rep.RuntimePath)
		}
	}
	projectName := repoName(scans, rep)
	var rows []DashboardRow
	for _, taskRow := range refresh.Rows {
		merge, awaitingIntegration := mergeabilityForSet(state, repoKey, taskRow.ID)
		if !dashboardShowRow(taskRow, awaitingIntegration) {
			continue
		}
		wt := dashboardWorktree(d, state, repoKey, taskRow.ID, rep, repBranch, bare)
		drain := dashboardDrain(d, state, repoKey, taskRow.ID, wt.runtimePath)
		status := dashboardStatus(taskRow.Status, merge, awaitingIntegration)
		rows = append(rows, DashboardRow{
			Project:            projectName,
			SetID:              taskRow.ID,
			Status:             status,
			Worktree:           wt.label,
			Drain:              drain,
			AutoDrain:          taskRow.AutoDrain,
			defPath:            scans[0].DefinitionPath,
			statePath:          tasks.StatePathFor(scans[0].DefinitionPath),
			cursorKey:          projectName + "\x00" + taskRow.ID,
			repoKey:            repoKey,
			runtimePath:        wt.runtimePath,
			paneID:             dashboardPaneID(state, repoKey, taskRow.ID),
			integrationBacklog: taskRow.Status == tasks.StatusDone && awaitingIntegration,
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

// dashboardWorktreeMarker prefixes a branch running in a dedicated (non-trunk)
// Worktree binding. The representative checkout (trunk) is left unmarked.
const dashboardWorktreeMarker = "↳ "

func dashboardWorktree(d *Deps, state *DaemonState, repoKey, setID string, rep *projectScan, repBranch string, bare bool) dashboardWorktreeView {
	if state != nil {
		if b, ok := state.WorktreeBindings[setScopedKey(repoKey, setID)]; ok && strings.TrimSpace(b.RuntimePath) != "" {
			branch := b.Branch
			if branch == "" {
				branch = binding.CurrentBranch(d.Tasks, b.RuntimePath)
			}
			return dashboardWorktreeView{label: formatDashboardWorktree(branch, true), runtimePath: b.RuntimePath}
		}
	}
	if rep != nil {
		return dashboardWorktreeView{label: formatDashboardWorktree(repBranch, false), runtimePath: rep.RuntimePath}
	}
	if bare {
		return dashboardWorktreeView{label: "(no base)"}
	}
	return dashboardWorktreeView{label: "(no base)"}
}

// formatDashboardWorktree renders a row's checkout as its branch, marking a
// dedicated (non-trunk) worktree with a leading glyph. The on-disk path is not
// shown inline — it lives in the `s` inspect modal.
func formatDashboardWorktree(branch string, worktree bool) string {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		branch = "detached"
	}
	if worktree {
		return dashboardWorktreeMarker + branch
	}
	return branch
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

func dashboardPaneID(state *DaemonState, repoKey, setID string) string {
	if state == nil || state.DrainPanes == nil {
		return ""
	}
	if pane, ok := state.DrainPanes[setScopedKey(repoKey, setID)]; ok {
		return pane.PaneID
	}
	return ""
}

type dashboardTickMsg struct{}
type dashboardRowsMsg struct {
	snap DashboardSnapshot
	err  error
}
type dashboardToggleMsg struct {
	key       string
	autoDrain bool
	err       error
}
type dashboardDrainMsg struct {
	err error
}
type dashboardPreviewMsg struct {
	err error
}
type dashboardStatusMsg struct {
	row   DashboardRow
	lines []string
	err   error
}
type dashboardBindListMsg struct {
	row     DashboardRow
	entries []dashboardBindEntry
	err     error
}
type dashboardBindRefsMsg struct {
	refs []string
	err  error
}
type dashboardBindMsg struct {
	err error
}
type dashboardAbandonMsg struct {
	err error
}

type dashboardBindStage int

const (
	dashboardBindStageWorktree dashboardBindStage = iota
	dashboardBindStageBaseRef
	dashboardBindStageName
)

type dashboardBindEntry struct {
	Label  string
	Path   string
	Branch string
	Create bool
}

type dashboardBindModal struct {
	row     DashboardRow
	stage   dashboardBindStage
	entries []dashboardBindEntry
	refs    []string
	cursor  int
	baseRef string
	name    string
	loading bool
}

type dashboardAbandonModal struct {
	row     DashboardRow
	loading bool
}

type dashboardStatusModal struct {
	row     DashboardRow
	lines   []string
	scroll  int
	loading bool
	err     error
}

type dashboardModel struct {
	d       *Deps
	cfg     *config.Config
	snap    DashboardSnapshot
	allRows []DashboardRow // source of truth; snap.Rows is the filtered view
	cursor  int
	err     error
	width   int
	height  int
	bind    *dashboardBindModal
	abandon *dashboardAbandonModal
	status  *dashboardStatusModal

	filterMode  bool
	filterInput textinput.Model
}

func newDashboardModel(d *Deps, cfg *config.Config, snap DashboardSnapshot) dashboardModel {
	if d == nil {
		d = DefaultDeps()
	}
	return dashboardModel{d: d, cfg: cfg, snap: snap, allRows: snap.Rows}
}

func (m dashboardModel) Init() tea.Cmd {
	return dashboardTick()
}

func (m dashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.bind != nil {
			return m.updateBindModal(msg)
		}
		if m.abandon != nil {
			return m.updateAbandonModal(msg)
		}
		if m.status != nil {
			return m.updateStatusModal(msg)
		}
		if m.filterMode {
			return m.updateFilterMode(msg)
		}
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			return m, tea.Quit
		case "/":
			m.filterMode = true
			ti := textinput.New()
			ti.Prompt = "> "
			ti.Focus()
			m.filterInput = ti
			return m, nil
		case "j", "down":
			if len(m.snap.Rows) > 0 && m.cursor < len(m.snap.Rows)-1 {
				m.cursor++
			}
		case "k", "up":
			if len(m.snap.Rows) > 0 && m.cursor > 0 {
				m.cursor--
			}
		case "a":
			if len(m.snap.Rows) == 0 || m.cursor < 0 || m.cursor >= len(m.snap.Rows) {
				return m, nil
			}
			row := m.snap.Rows[m.cursor]
			m.snap.Rows[m.cursor].AutoDrain = !m.snap.Rows[m.cursor].AutoDrain
			m.err = nil
			return m, m.toggleAutoDrain(row)
		case "i":
			if len(m.snap.Rows) == 0 || m.cursor < 0 || m.cursor >= len(m.snap.Rows) {
				return m, nil
			}
			m.err = nil
			return m, m.launchDrain(m.snap.Rows[m.cursor])
		case "I":
			if !m.selectedRowCanIntegrate() {
				return m, nil
			}
			m.err = nil
			return m, m.launchIntegrate(m.snap.Rows[m.cursor])
		case "p":
			if len(m.snap.Rows) == 0 || m.cursor < 0 || m.cursor >= len(m.snap.Rows) {
				return m, nil
			}
			m.err = nil
			return m, m.previewDrain(m.snap.Rows[m.cursor])
		case "s":
			if len(m.snap.Rows) == 0 || m.cursor < 0 || m.cursor >= len(m.snap.Rows) {
				return m, nil
			}
			m.err = nil
			row := m.snap.Rows[m.cursor]
			m.status = &dashboardStatusModal{row: row, loading: true}
			return m, m.loadStatusDetail(row)
		case "b":
			if len(m.snap.Rows) == 0 || m.cursor < 0 || m.cursor >= len(m.snap.Rows) {
				return m, nil
			}
			m.err = nil
			row := m.snap.Rows[m.cursor]
			m.bind = &dashboardBindModal{row: row, loading: true}
			return m, m.loadBindWorktrees(row)
		case "U":
			if len(m.snap.Rows) == 0 || m.cursor < 0 || m.cursor >= len(m.snap.Rows) {
				return m, nil
			}
			m.err = nil
			m.abandon = &dashboardAbandonModal{row: m.snap.Rows[m.cursor]}
			return m, nil
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
			m.allRows = msg.snap.Rows
			m.snap = msg.snap
			if m.filterMode {
				m.snap.Rows = filterDashboardRows(m.allRows, m.filterInput.Value())
			}
			m.cursor = dashboardCursorAfterReload(m.snap.Rows, oldKey, m.cursor)
		}
	case dashboardToggleMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, m.reload()
		}
		for i := range m.snap.Rows {
			if m.snap.Rows[i].cursorKey == msg.key {
				m.snap.Rows[i].AutoDrain = msg.autoDrain
				break
			}
		}
	case dashboardDrainMsg:
		if msg.err != nil {
			m.err = msg.err
		}
		return m, m.reload()
	case dashboardPreviewMsg:
		if msg.err != nil {
			m.err = msg.err
		}
	case dashboardStatusMsg:
		if m.status == nil {
			return m, nil
		}
		m.status = &dashboardStatusModal{row: msg.row, lines: msg.lines, err: msg.err}
	case dashboardBindListMsg:
		if msg.err != nil {
			m.err = msg.err
			m.bind = nil
			return m, nil
		}
		m.bind = &dashboardBindModal{row: msg.row, entries: msg.entries}
	case dashboardBindRefsMsg:
		if m.bind == nil {
			return m, nil
		}
		if msg.err != nil {
			m.err = msg.err
			m.bind = nil
			return m, nil
		}
		m.bind.stage = dashboardBindStageBaseRef
		m.bind.refs = msg.refs
		m.bind.cursor = 0
		m.bind.loading = false
	case dashboardBindMsg:
		if msg.err != nil {
			m.err = msg.err
			m.bind = nil
			return m, m.reload()
		}
		m.bind = nil
		return m, m.reload()
	case dashboardAbandonMsg:
		if msg.err != nil {
			m.err = msg.err
			m.abandon = nil
			return m, m.reload()
		}
		m.abandon = nil
		return m, m.reload()
	}
	return m, nil
}

func (m dashboardModel) selectedRowCanIntegrate() bool {
	return len(m.snap.Rows) > 0 && m.cursor >= 0 && m.cursor < len(m.snap.Rows) && DashboardRowCanIntegrate(m.snap.Rows[m.cursor])
}

func (m dashboardModel) updateBindModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.bind = nil
		return m, nil
	case "j", "down":
		m.moveBindCursor(1)
		return m, nil
	case "k", "up":
		m.moveBindCursor(-1)
		return m, nil
	case "backspace":
		if m.bind.stage == dashboardBindStageName && len(m.bind.name) > 0 {
			m.bind.name = m.bind.name[:len(m.bind.name)-1]
		}
		return m, nil
	case "enter":
		return m.confirmBindModal()
	}
	if m.bind.stage == dashboardBindStageName {
		if s := msg.String(); len([]rune(s)) == 1 {
			m.bind.name += s
		}
	}
	return m, nil
}

func (m dashboardModel) updateAbandonModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c", "n":
		m.abandon = nil
		return m, nil
	case "enter", "y":
		if m.abandon == nil || m.abandon.loading {
			return m, nil
		}
		m.abandon.loading = true
		return m, m.abandonWorktree(m.abandon.row)
	}
	return m, nil
}

func (m dashboardModel) updateStatusModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.status = nil
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		m.moveStatusScroll(1)
	case "k", "up":
		m.moveStatusScroll(-1)
	case "pgdown":
		m.moveStatusScroll(m.statusVisibleLines())
	case "pgup":
		m.moveStatusScroll(-m.statusVisibleLines())
	case "home":
		m.status.scroll = 0
	case "end":
		max := m.statusMaxScroll()
		m.status.scroll = max
	}
	return m, nil
}

func (m dashboardModel) updateFilterMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.filterMode = false
		m.filterInput = textinput.Model{}
		m.snap.Rows = m.allRows
		m.cursor = dashboardCursorAfterReload(m.snap.Rows, "", m.cursor)
		return m, nil
	case "j", "down":
		if len(m.snap.Rows) > 0 && m.cursor < len(m.snap.Rows)-1 {
			m.cursor++
		}
		return m, nil
	case "k", "up":
		if len(m.snap.Rows) > 0 && m.cursor > 0 {
			m.cursor--
		}
		return m, nil
	default:
		oldKey := ""
		if m.cursor >= 0 && m.cursor < len(m.snap.Rows) {
			oldKey = m.snap.Rows[m.cursor].cursorKey
		}
		var cmd tea.Cmd
		m.filterInput, cmd = m.filterInput.Update(msg)
		m.snap.Rows = filterDashboardRows(m.allRows, m.filterInput.Value())
		m.cursor = dashboardCursorAfterReload(m.snap.Rows, oldKey, m.cursor)
		return m, cmd
	}
}

// filterDashboardRows returns rows whose Project or SetID contain query
// (case-insensitive). Returns allRows unchanged when query is empty.
func filterDashboardRows(rows []DashboardRow, query string) []DashboardRow {
	if query == "" {
		return rows
	}
	q := strings.ToLower(query)
	var filtered []DashboardRow
	for _, row := range rows {
		if strings.Contains(strings.ToLower(row.Project), q) ||
			strings.Contains(strings.ToLower(row.SetID), q) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func (m *dashboardModel) moveBindCursor(delta int) {
	if m.bind == nil {
		return
	}
	limit := len(m.bind.entries)
	if m.bind.stage == dashboardBindStageBaseRef {
		limit = len(m.bind.refs)
	}
	if limit == 0 {
		return
	}
	m.bind.cursor += delta
	if m.bind.cursor < 0 {
		m.bind.cursor = limit - 1
	}
	if m.bind.cursor >= limit {
		m.bind.cursor = 0
	}
}

func (m *dashboardModel) moveStatusScroll(delta int) {
	if m.status == nil {
		return
	}
	m.status.scroll += delta
	if m.status.scroll < 0 {
		m.status.scroll = 0
	}
	if max := m.statusMaxScroll(); m.status.scroll > max {
		m.status.scroll = max
	}
}

func (m dashboardModel) statusVisibleLines() int {
	if m.height <= 0 {
		return 16
	}
	n := m.height - 10
	if n < 4 {
		return 4
	}
	return n
}

func (m dashboardModel) statusMaxScroll() int {
	if m.status == nil {
		return 0
	}
	if max := len(m.status.lines) - m.statusVisibleLines(); max > 0 {
		return max
	}
	return 0
}

func (m dashboardModel) confirmBindModal() (tea.Model, tea.Cmd) {
	if m.bind == nil || m.bind.loading {
		return m, nil
	}
	switch m.bind.stage {
	case dashboardBindStageWorktree:
		if len(m.bind.entries) == 0 || m.bind.cursor < 0 || m.bind.cursor >= len(m.bind.entries) {
			return m, nil
		}
		entry := m.bind.entries[m.bind.cursor]
		if entry.Create {
			m.bind.loading = true
			return m, m.loadBindRefs(m.bind.row)
		}
		m.bind.loading = true
		return m, m.adoptBindWorktree(m.bind.row, entry.Path)
	case dashboardBindStageBaseRef:
		if len(m.bind.refs) == 0 || m.bind.cursor < 0 || m.bind.cursor >= len(m.bind.refs) {
			return m, nil
		}
		m.bind.baseRef = m.bind.refs[m.bind.cursor]
		m.bind.stage = dashboardBindStageName
		m.bind.cursor = 0
		return m, nil
	case dashboardBindStageName:
		name := strings.TrimSpace(m.bind.name)
		if name == "" {
			m.err = fmt.Errorf("worktree name is required")
			return m, nil
		}
		m.bind.loading = true
		return m, m.createBindWorktree(m.bind.row, m.bind.baseRef, name)
	}
	return m, nil
}

func (m dashboardModel) reload() tea.Cmd {
	return func() tea.Msg {
		snap, err := BuildDashboard(m.d, m.cfg)
		return dashboardRowsMsg{snap: snap, err: err}
	}
}

func (m dashboardModel) toggleAutoDrain(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		result, err := m.d.toggleAutoDrain(row.defPath, row.statePath, row.SetID)
		if err != nil {
			return dashboardToggleMsg{key: row.cursorKey, err: err}
		}
		return dashboardToggleMsg{key: row.cursorKey, autoDrain: result.AutoDrain}
	}
}

func (m dashboardModel) launchDrain(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		_, err := LaunchDashboardDrain(m.d, m.cfg, row)
		return dashboardDrainMsg{err: err}
	}
}

func (m dashboardModel) launchIntegrate(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		_, err := LaunchDashboardIntegrate(m.d, m.cfg, row)
		return dashboardDrainMsg{err: err}
	}
}

func (m dashboardModel) previewDrain(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		err := PreviewDashboardDrain(m.d, row)
		return dashboardPreviewMsg{err: err}
	}
}

func (m dashboardModel) loadStatusDetail(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		lines, err := DashboardStatusDetailLines(m.d, row)
		return dashboardStatusMsg{row: row, lines: lines, err: err}
	}
}

func (m dashboardModel) loadBindWorktrees(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		entries, err := DashboardBindWorktreeEntries(m.d, m.cfg, row)
		return dashboardBindListMsg{row: row, entries: entries, err: err}
	}
}

// DashboardStatusDetailLines renders the same per-set task status detail as
// `pop tasks status <set>` for a dashboard row.
func DashboardStatusDetailLines(d *Deps, row DashboardRow) ([]string, error) {
	if d == nil {
		d = DefaultDeps()
	}
	if d.Tasks == nil {
		d.Tasks = tasks.DefaultDeps()
	}
	refresh, err := d.refresh(row.defPath)
	if err != nil {
		return nil, err
	}
	detailRow := tasks.FindRow(refresh, row.SetID)
	var buf bytes.Buffer
	tasks.RenderTaskSetDetail(&buf, row.SetID, detailRow, refresh.Manifests[row.SetID])
	text := strings.TrimRight(buf.String(), "\n")
	if text == "" {
		text = fmt.Sprintf("%s: no status detail available", row.SetID)
	}
	lines := strings.Split(text, "\n")
	if strings.TrimSpace(row.runtimePath) != "" {
		lines = append([]string{"checkout: " + row.runtimePath, ""}, lines...)
	}
	return lines, nil
}

func (m dashboardModel) loadBindRefs(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		refs, err := DashboardBindBaseRefs(m.d, m.cfg, row)
		return dashboardBindRefsMsg{refs: refs, err: err}
	}
}

func (m dashboardModel) adoptBindWorktree(row DashboardRow, checkoutPath string) tea.Cmd {
	return func() tea.Msg {
		_, err := DashboardAdoptWorktree(m.d, m.cfg, row, checkoutPath)
		return dashboardBindMsg{err: err}
	}
}

func (m dashboardModel) createBindWorktree(row DashboardRow, baseRef, name string) tea.Cmd {
	return func() tea.Msg {
		_, err := DashboardCreateWorktree(m.d, m.cfg, row, baseRef, name)
		return dashboardBindMsg{err: err}
	}
}

func (m dashboardModel) abandonWorktree(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		_, err := DashboardUnbindWorktree(m.d, m.cfg, row)
		return dashboardAbandonMsg{err: err}
	}
}

type DashboardDrainResult struct {
	PaneID      string
	RuntimePath string
}

type DashboardIntegrateResult struct {
	PaneID      string
	RuntimePath string
}

// LaunchDashboardDrain manually launches the highlighted dashboard row through
// the same Queue provisioning and tmux spawn path used by the supervisor.
func LaunchDashboardDrain(d *Deps, cfg *config.Config, row DashboardRow) (DashboardDrainResult, error) {
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
		return DashboardDrainResult{}, err
	}
	scans, err := dashboardScansForDefinition(d, cfg, row.defPath)
	if err != nil {
		return DashboardDrainResult{}, err
	}
	if len(scans) == 0 {
		return DashboardDrainResult{}, fmt.Errorf("task set %s is no longer in a registered queue project", row.SetID)
	}
	repoKey, err := scanRepoKey(d, scans[0])
	if err != nil {
		return DashboardDrainResult{}, err
	}
	rep, bare, err := resolveRepresentative(d, cfg, scans)
	if err != nil {
		return DashboardDrainResult{}, err
	}
	qcfg, err := resolvedQueueConfig(cfg)
	if err != nil {
		return DashboardDrainResult{}, err
	}
	defaultAgent, _, notes, ok := selectDefaultAgent(d, qcfg.Agents, state, d.now().UTC())
	if !ok {
		for _, note := range notes {
			if note.Reason != "" {
				return DashboardDrainResult{}, fmt.Errorf("no available agent: %s: %s", note.Agent, note.Reason)
			}
		}
		return DashboardDrainResult{}, fmt.Errorf("no available agent")
	}

	dec := Decision{
		Project:      repoName(scans, rep),
		TaskSetID:    row.SetID,
		DefaultAgent: defaultAgent,
	}
	key := setScopedKey(repoKey, row.SetID)
	if binding, ok := state.WorktreeBindings[key]; ok && strings.TrimSpace(binding.RuntimePath) != "" {
		if err := validateBoundWorktree(d, scans[0].ProjectPath, binding); err != nil {
			return DashboardDrainResult{}, fmt.Errorf("bound worktree for %s is invalid (%v); repair git state or run `pop tasks unbind-worktree`", row.SetID, err)
		}
		worktreeReady, configErr := readRepoConfig(d, scans[0].ProjectPath)
		if configErr != "" {
			worktreeReady = false
		}
		sessionName := project.SessionNameWith(d.Project, binding.RuntimePath)
		if worktreeReady && rep != nil {
			sessionName = rep.SessionName
		}
		dec.WorktreeReady = worktreeReady
		dec.scan = projectScan{
			Name:           dec.Project,
			ProjectPath:    binding.RuntimePath,
			DefinitionPath: scans[0].DefinitionPath,
			RuntimePath:    binding.RuntimePath,
			SessionName:    sessionName,
			RepoKey:        repoKey,
		}
	} else {
		if rep == nil {
			if bare {
				return DashboardDrainResult{}, fmt.Errorf("%s", repoScanReason)
			}
			return DashboardDrainResult{}, fmt.Errorf("no queue base checkout")
		}
		dec.scan = *rep
		dec.WorktreeReady, _ = readRepoConfig(d, rep.ProjectPath)
		if dec.WorktreeReady {
			dec = prepareWorktreeDrain(d, io.Discard, dec)
			if !dec.Actionable() {
				return DashboardDrainResult{}, fmt.Errorf("%s", dec.Reason)
			}
		}
	}

	spawn, err := SpawnWithResult(d, dec)
	if err != nil {
		_ = AppendJournalEntry(d.Tasks, JournalEntry{
			Event:       JournalEventSpawnFailed,
			Project:     dec.Project,
			SetID:       dec.TaskSetID,
			RuntimePath: dec.scan.RuntimePath,
			Source:      "dashboard",
			Reason:      err.Error(),
		})
		return DashboardDrainResult{}, err
	}
	if err := recordDrainPane(d, dec, spawn.PaneID, "dashboard"); err != nil {
		return DashboardDrainResult{}, err
	}
	if err := AppendJournalEntry(d.Tasks, JournalEntry{
		Event:       JournalEventSpawn,
		Project:     dec.Project,
		SetID:       dec.TaskSetID,
		RuntimePath: dec.scan.RuntimePath,
		Source:      "dashboard",
	}); err != nil {
		return DashboardDrainResult{}, err
	}
	return DashboardDrainResult{PaneID: spawn.PaneID, RuntimePath: dec.scan.RuntimePath}, nil
}

// DashboardRowCanIntegrate reports whether the dashboard's `I` action is
// enabled. Only DONE sets that have a mergeability record are in the
// Integration backlog; all other rows intentionally no-op.
func DashboardRowCanIntegrate(row DashboardRow) bool {
	return row.integrationBacklog
}

// LaunchDashboardIntegrate opens the existing `pop tasks integrate <set>`
// wizard in the shared pop-queue window and switches the current tmux client to
// that pane so the attended conflict path has a TTY.
func LaunchDashboardIntegrate(d *Deps, cfg *config.Config, row DashboardRow) (DashboardIntegrateResult, error) {
	if !DashboardRowCanIntegrate(row) {
		return DashboardIntegrateResult{}, nil
	}
	if d == nil {
		d = DefaultDeps()
	}
	if d.Tasks == nil {
		d.Tasks = tasks.DefaultDeps()
	}
	if d.Project == nil {
		d.Project = project.DefaultDeps()
	}
	if d.Tmux == nil {
		d.Tmux = deps.NewRealTmux()
	}
	session, dir, err := dashboardIntegrateTarget(d, cfg, row)
	if err != nil {
		return DashboardIntegrateResult{}, err
	}
	paneID, err := spawnIntegrateWizard(d.Tmux, session, dir, "pop tasks integrate "+shellQuote(row.SetID))
	if err != nil {
		return DashboardIntegrateResult{}, err
	}
	return DashboardIntegrateResult{PaneID: paneID, RuntimePath: dir}, nil
}

func dashboardIntegrateTarget(d *Deps, cfg *config.Config, row DashboardRow) (session, dir string, err error) {
	scans, err := dashboardScansForDefinition(d, cfg, row.defPath)
	if err != nil {
		return "", "", err
	}
	if len(scans) == 0 {
		return "", "", fmt.Errorf("task set %s is no longer in a registered queue project", row.SetID)
	}
	rep, _, err := resolveRepresentative(d, cfg, scans)
	if err != nil {
		return "", "", err
	}
	if rep != nil {
		return rep.SessionName, rep.ProjectPath, nil
	}
	if strings.TrimSpace(row.runtimePath) != "" {
		dir := row.runtimePath
		return project.SessionNameWith(d.Project, dir), dir, nil
	}
	return "", "", fmt.Errorf("no checkout available for integrating %s", row.SetID)
}

func spawnIntegrateWizard(tmux deps.Tmux, session, dir, command string) (string, error) {
	if !tmux.HasSession(session) {
		if err := tmux.NewSession(session, dir); err != nil {
			return "", fmt.Errorf("create session %q: %w", session, err)
		}
	}
	windowTarget, freshPaneID, err := resolveDrainWindowTarget(tmux, session, dir)
	if err != nil {
		return "", err
	}
	paneID := freshPaneID
	if paneID == "" {
		out, err := tmux.Command("split-window", "-d", "-P", "-F", "#{pane_id}", "-t", windowTarget, "-c", dir)
		if err != nil {
			return "", fmt.Errorf("create integrate pane: %w", err)
		}
		paneID = strings.TrimSpace(out)
		if paneID == "" {
			return "", fmt.Errorf("create integrate pane: tmux returned no pane id")
		}
		if _, err := tmux.Command("select-layout", "-t", windowTarget, "tiled"); err != nil {
			return "", fmt.Errorf("retile queue window: %w", err)
		}
	}
	if _, err := tmux.Command("send-keys", "-t", paneID, command, "Enter"); err != nil {
		return "", fmt.Errorf("send integrate command: %w", err)
	}
	if _, err := tmux.Command("select-pane", "-t", paneID); err != nil {
		return "", err
	}
	if _, err := tmux.Command("switch-client", "-t", paneID); err != nil {
		return "", err
	}
	return paneID, nil
}

func dashboardScansForDefinition(d *Deps, cfg *config.Config, defPath string) ([]projectScan, error) {
	projects, err := tasks.ListPickerProjectsWith(d.Project, cfg)
	if err != nil {
		return nil, err
	}
	var scans []projectScan
	for _, p := range projects {
		scan, err := resolveScan(d, p)
		if err != nil {
			if outsideQueueScopeResolveError(err) {
				continue
			}
			return nil, err
		}
		if scan.DefinitionPath == defPath {
			scans = append(scans, scan)
		}
	}
	return scans, nil
}

// PreviewDashboardDrain switches the active tmux client to the pane associated
// with the highlighted row. Rows without a recorded pane intentionally no-op.
func PreviewDashboardDrain(d *Deps, row DashboardRow) error {
	if strings.TrimSpace(row.paneID) == "" {
		return nil
	}
	if d == nil {
		d = DefaultDeps()
	}
	if d.Tmux == nil {
		d.Tmux = deps.NewRealTmux()
	}
	if _, err := d.Tmux.Command("select-pane", "-t", row.paneID); err != nil {
		return err
	}
	_, err := d.Tmux.Command("switch-client", "-t", row.paneID)
	return err
}

// DashboardUnbindWorktree releases the highlighted set's worktree binding
// through the same unbind implementation used by `pop tasks unbind-worktree`.
// The dashboard supplies its own inline confirmation, so the command-level
// prompt is skipped here.
func DashboardUnbindWorktree(d *Deps, cfg *config.Config, row DashboardRow) (AbandonResult, error) {
	key := ""
	if strings.TrimSpace(row.repoKey) != "" {
		key = setScopedKey(row.repoKey, row.SetID)
	}
	return AbandonBindingWithOptions(d, cfg, key, row.SetID, io.Discard, AbandonOptions{Yes: true, In: tasks.NonInteractiveReader{}})
}

// DashboardBindWorktreeEntries returns the inline bind picker entries for the
// highlighted dashboard row: every existing worktree in the row's repository,
// followed by the pop-native creation entry.
func DashboardBindWorktreeEntries(d *Deps, cfg *config.Config, row DashboardRow) ([]dashboardBindEntry, error) {
	scans, _, err := dashboardBindContext(d, cfg, row)
	if err != nil {
		return nil, err
	}
	out, err := d.Tasks.Git.CommandInDir(scans[0].ProjectPath, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("list worktrees: %w", err)
	}
	worktrees := parseDashboardWorktrees(out)
	entries := make([]dashboardBindEntry, 0, len(worktrees)+1)
	for _, wt := range worktrees {
		label := wt.Name
		if wt.Branch != "" {
			label = fmt.Sprintf("%s (%s)", wt.Name, wt.Branch)
		}
		entries = append(entries, dashboardBindEntry{Label: label, Path: wt.Path, Branch: wt.Branch})
	}
	entries = append(entries, dashboardBindEntry{Label: "＋ Create new worktree", Create: true})
	return entries, nil
}

// DashboardBindBaseRefs lists local and remote branch refs for the create-new
// flow, with main/master variants first.
func DashboardBindBaseRefs(d *Deps, cfg *config.Config, row DashboardRow) ([]string, error) {
	scans, _, err := dashboardBindContext(d, cfg, row)
	if err != nil {
		return nil, err
	}
	out, err := d.Tasks.Git.CommandInDir(scans[0].ProjectPath, "for-each-ref", "--format=%(refname:short)", "refs/heads", "refs/remotes")
	if err != nil {
		return nil, fmt.Errorf("list base refs: %w", err)
	}
	refs := parseDashboardBaseRefs(out)
	if len(refs) == 0 {
		return nil, fmt.Errorf("no local or remote branches found")
	}
	return refs, nil
}

// DashboardAdoptWorktree binds row.SetID to an existing checkout. The dashboard
// action is deliberate, so idle re-pointing uses Force without a second prompt.
func DashboardAdoptWorktree(d *Deps, cfg *config.Config, row DashboardRow, checkoutPath string) (BindWorktreeResult, error) {
	if err := refuseDashboardBindWhileLocked(d, row); err != nil {
		return BindWorktreeResult{}, err
	}
	return BindWorktree(d, cfg, row.SetID, checkoutPath, BindWorktreeOptions{Force: true}, io.Discard)
}

type DashboardCreateWorktreeResult struct {
	SetID       string
	RuntimePath string
	Branch      string
	BaseRef     string
}

// DashboardCreateWorktree creates a pop-managed worktree on a fresh branch and
// records a provisioned binding. It never opens or attaches a tmux session.
func DashboardCreateWorktree(d *Deps, cfg *config.Config, row DashboardRow, baseRef, name string) (DashboardCreateWorktreeResult, error) {
	baseRef = strings.TrimSpace(baseRef)
	name = strings.TrimSpace(name)
	if baseRef == "" {
		return DashboardCreateWorktreeResult{}, fmt.Errorf("base ref is required")
	}
	if name == "" {
		return DashboardCreateWorktreeResult{}, fmt.Errorf("worktree name is required")
	}
	scans, repoKey, err := dashboardBindContext(d, cfg, row)
	if err != nil {
		return DashboardCreateWorktreeResult{}, err
	}
	if err := refuseDashboardBindWhileLocked(d, row); err != nil {
		return DashboardCreateWorktreeResult{}, err
	}
	branch := name
	path := filepath.Join(QueueDataDir(d.Tasks), "worktrees", repoKey, binding.SafeComponent(name))
	if err := d.Tasks.FS.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return DashboardCreateWorktreeResult{}, fmt.Errorf("create worktree parent: %w", err)
	}
	if _, err := d.Tasks.Git.CommandInDir(scans[0].ProjectPath, "worktree", "add", "-b", branch, path, baseRef); err != nil {
		return DashboardCreateWorktreeResult{}, fmt.Errorf("git worktree add: %w", err)
	}
	proj := repoName(scans, nil)
	if rep, _, err := resolveRepresentative(d, cfg, scans); err == nil {
		proj = repoName(scans, rep)
	}
	state, err := EnsureDaemonState(d.Tasks)
	if err != nil {
		return DashboardCreateWorktreeResult{}, err
	}
	if state.WorktreeBindings == nil {
		state.WorktreeBindings = map[string]WorktreeBinding{}
	}
	key := setScopedKey(repoKey, row.SetID)
	state.WorktreeBindings[key] = binding.Binding{RuntimePath: path, Branch: branch, Project: proj, Provisioned: true}
	if err := WriteDaemonState(d.Tasks, state); err != nil {
		return DashboardCreateWorktreeResult{}, err
	}
	if err := AppendJournalEntry(d.Tasks, JournalEntry{
		Event:       JournalEventBound,
		Project:     proj,
		SetID:       row.SetID,
		RuntimePath: path,
		SourceRef:   branch,
		Source:      "dashboard",
	}); err != nil {
		return DashboardCreateWorktreeResult{}, err
	}
	return DashboardCreateWorktreeResult{SetID: row.SetID, RuntimePath: path, Branch: branch, BaseRef: baseRef}, nil
}

func dashboardBindContext(d *Deps, cfg *config.Config, row DashboardRow) ([]projectScan, string, error) {
	if d == nil {
		d = DefaultDeps()
	}
	if d.Tasks == nil {
		d.Tasks = tasks.DefaultDeps()
	}
	if d.Project == nil {
		d.Project = project.DefaultDeps()
	}
	scans, err := dashboardScansForDefinition(d, cfg, row.defPath)
	if err != nil {
		return nil, "", err
	}
	if len(scans) == 0 {
		return nil, "", fmt.Errorf("task set %s is no longer in a registered queue project", row.SetID)
	}
	repoKey := row.repoKey
	if repoKey == "" {
		repoKey, err = scanRepoKey(d, scans[0])
		if err != nil {
			return nil, "", err
		}
	}
	return scans, repoKey, nil
}

func refuseDashboardBindWhileLocked(d *Deps, row DashboardRow) error {
	if d == nil {
		d = DefaultDeps()
	}
	if d.Tasks == nil {
		d.Tasks = tasks.DefaultDeps()
	}
	state, err := EnsureDaemonState(d.Tasks)
	if err != nil {
		return err
	}
	paths := map[string]bool{}
	if strings.TrimSpace(row.runtimePath) != "" {
		paths[row.runtimePath] = true
	}
	if row.repoKey != "" {
		if b, ok := state.WorktreeBindings[setScopedKey(row.repoKey, row.SetID)]; ok && b.RuntimePath != "" {
			paths[b.RuntimePath] = true
		}
	}
	for path := range paths {
		lock := d.readLock(path)
		if lock == nil || !lock.Locked {
			continue
		}
		if lock.Metadata == nil || lock.Metadata.SetID == "" || lock.Metadata.SetID == row.SetID {
			return fmt.Errorf("refusing bind-worktree: %s is currently executing", row.SetID)
		}
	}
	return nil
}

func parseDashboardWorktrees(output string) []project.Worktree {
	var worktrees []project.Worktree
	var current project.Worktree
	isBare := false
	for _, line := range strings.Split(output, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			current.Path = strings.TrimPrefix(line, "worktree ")
			current.Name = filepath.Base(current.Path)
		case strings.HasPrefix(line, "branch "):
			current.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case line == "detached":
			current.Branch = "detached"
		case line == "bare":
			isBare = true
		case line == "":
			if current.Path != "" && !isBare {
				worktrees = append(worktrees, current)
			}
			current = project.Worktree{}
			isBare = false
		}
	}
	if current.Path != "" && !isBare {
		worktrees = append(worktrees, current)
	}
	return worktrees
}

func parseDashboardBaseRefs(output string) []string {
	seen := map[string]bool{}
	var refs []string
	for _, line := range strings.Split(output, "\n") {
		ref := strings.TrimSpace(line)
		if ref == "" || strings.HasSuffix(ref, "/HEAD") || seen[ref] {
			continue
		}
		seen[ref] = true
		refs = append(refs, ref)
	}
	sort.SliceStable(refs, func(i, j int) bool {
		ri, rj := dashboardBaseRefRank(refs[i]), dashboardBaseRefRank(refs[j])
		if ri != rj {
			return ri < rj
		}
		return refs[i] < refs[j]
	})
	return refs
}

func dashboardBaseRefRank(ref string) int {
	switch ref {
	case "main":
		return 0
	case "master":
		return 1
	}
	if strings.HasSuffix(ref, "/main") {
		return 2
	}
	if strings.HasSuffix(ref, "/master") {
		return 3
	}
	return 4
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
	var body strings.Builder
	title := lipgloss.NewStyle().Bold(true).Render("Queue dashboard")
	fmt.Fprintln(&body, title)
	if m.err != nil {
		fmt.Fprintf(&body, "refresh error: %v\n", m.err)
	}
	if len(m.snap.Rows) == 0 {
		if m.filterMode {
			fmt.Fprintln(&body, "No matching task sets.")
		} else {
			fmt.Fprintln(&body, "No queue-actionable task sets.")
		}
		fmt.Fprintln(&body)
		if m.filterMode {
			ui.WriteInputBox(&body, m.width, m.filterInput.View())
			fmt.Fprint(&body, ui.HintStyle.Render("esc clear filter"))
		} else {
			fmt.Fprint(&body, ui.HintStyle.Render("q quit"))
		}
		content := body.String()
		v := tea.NewView(dashboardBottomAnchor(m.height, content))
		v.AltScreen = true
		return v
	}
	renderDashboardTable(&body, m.snap.Rows, m.cursor, m.width)
	fmt.Fprintln(&body)
	if m.bind != nil {
		renderDashboardBindModal(&body, m.bind)
	} else if m.abandon != nil {
		renderDashboardAbandonModal(&body, m.abandon)
	} else if m.status != nil {
		renderDashboardStatusModal(&body, m.status, m.statusVisibleLines())
	} else if m.filterMode {
		ui.WriteInputBox(&body, m.width, m.filterInput.View())
		fmt.Fprint(&body, ui.HintStyle.Render("esc clear filter · j/k navigate"))
	} else {
		fmt.Fprint(&body, ui.HintStyle.Render("j/k move · s status · i drain · p preview · b bind worktree · U unbind worktree · a auto-drain · / filter · q quit"))
	}
	content := body.String()
	v := tea.NewView(dashboardBottomAnchor(m.height, content))
	v.AltScreen = true
	return v
}

// dashboardBottomAnchor prepends empty lines so that content is pushed to the
// bottom of the terminal — the same anchoring used by the project/worktree picker.
// When height is not yet known (0) the content is returned unchanged.
func dashboardBottomAnchor(height int, content string) string {
	if height <= 0 {
		return content
	}
	lines := strings.Count(content, "\n")
	if emptyLines := height - lines; emptyLines > 0 {
		return strings.Repeat("\n", emptyLines) + content
	}
	return content
}

func renderDashboardStatusModal(w io.Writer, modal *dashboardStatusModal, visibleLines int) {
	if modal == nil {
		return
	}
	fmt.Fprintf(w, "Task status: %s\n", modal.row.SetID)
	if modal.loading {
		fmt.Fprintln(w, "  loading...")
		fmt.Fprint(w, ui.HintStyle.Render("esc/q close"))
		return
	}
	if modal.err != nil {
		fmt.Fprintf(w, "  error: %v\n", modal.err)
		fmt.Fprint(w, ui.HintStyle.Render("esc/q close"))
		return
	}
	if visibleLines <= 0 {
		visibleLines = len(modal.lines)
	}
	start := modal.scroll
	if start < 0 {
		start = 0
	}
	if start > len(modal.lines) {
		start = len(modal.lines)
	}
	end := start + visibleLines
	if end > len(modal.lines) {
		end = len(modal.lines)
	}
	for _, line := range modal.lines[start:end] {
		fmt.Fprintf(w, "  %s\n", line)
	}
	if len(modal.lines) > visibleLines {
		fmt.Fprint(w, ui.HintStyle.Render(fmt.Sprintf("showing %d-%d of %d · j/k scroll · esc/q close", start+1, end, len(modal.lines))))
		return
	}
	fmt.Fprint(w, ui.HintStyle.Render("esc/q close"))
}

func renderDashboardBindModal(w io.Writer, modal *dashboardBindModal) {
	if modal == nil {
		return
	}
	fmt.Fprintln(w, "Bind worktree")
	if modal.loading {
		fmt.Fprintln(w, "  loading...")
		return
	}
	switch modal.stage {
	case dashboardBindStageWorktree:
		for i, entry := range modal.entries {
			prefix := "  "
			if i == modal.cursor {
				prefix = ui.IndicatorStyle.Render("█") + " "
			}
			label := entry.Label
			if label == "" {
				label = entry.Path
			}
			fmt.Fprintf(w, "%s%s\n", prefix, label)
		}
		fmt.Fprint(w, ui.HintStyle.Render("enter select · esc cancel"))
	case dashboardBindStageBaseRef:
		fmt.Fprintln(w, "Base ref")
		for i, ref := range modal.refs {
			prefix := "  "
			if i == modal.cursor {
				prefix = ui.IndicatorStyle.Render("█") + " "
			}
			fmt.Fprintf(w, "%s%s\n", prefix, ref)
		}
		fmt.Fprint(w, ui.HintStyle.Render("enter select · esc cancel"))
	case dashboardBindStageName:
		fmt.Fprintf(w, "Base: %s\n", modal.baseRef)
		fmt.Fprintf(w, "Name: %s\n", modal.name)
		fmt.Fprint(w, ui.HintStyle.Render("enter create · esc cancel"))
	}
}

func renderDashboardAbandonModal(w io.Writer, modal *dashboardAbandonModal) {
	if modal == nil {
		return
	}
	fmt.Fprintf(w, "Unbind worktree for %s\n", modal.row.SetID)
	if modal.loading {
		fmt.Fprintln(w, "  unbinding...")
		return
	}
	fmt.Fprintln(w, "This releases the binding without integrating. Task statuses are unchanged.")
	fmt.Fprint(w, ui.HintStyle.Render("enter/y confirm · n/esc cancel"))
}

func renderDashboardTable(w io.Writer, rows []DashboardRow, cursor, width int) {
	headers := []string{"project", "task set", "status", "worktree", "drain", ""}
	widths := []int{len(headers[0]), len(headers[1]), len(headers[2]), len(headers[3]), len(headers[4])}
	widths = append(widths, len(headers[5]))
	for _, row := range rows {
		values := dashboardRowValues(row)
		for i, v := range values {
			if n := lipgloss.Width(v); n > widths[i] {
				widths[i] = n
			}
		}
	}
	fmt.Fprintf(w, "%s\n", truncateToWidth("  "+dashboardTableLine(headers, widths), width))
	for i, row := range rows {
		var prefix string
		if i == cursor {
			prefix = ui.IndicatorStyle.Render("█") + " "
		} else {
			prefix = "  "
		}
		line := truncateToWidth(prefix+dashboardTableLine(dashboardRowValues(row), widths), width)
		fmt.Fprintf(w, "%s\n", line)
	}
}

// truncateToWidth clips a rendered line that overflows the viewport, replacing
// the tail with an ellipsis. A non-positive width (no WindowSizeMsg yet) leaves
// the line untouched.
func truncateToWidth(s string, width int) string {
	if width <= 0 || lipgloss.Width(s) <= width {
		return s
	}
	if width <= 1 {
		return "…"
	}
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes))+1 > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}

func dashboardRowValues(row DashboardRow) []string {
	badge := ""
	if row.AutoDrain {
		badge = "Auto-drain"
	}
	return []string{row.Project, row.SetID, row.Status, row.Worktree, row.Drain, badge}
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
