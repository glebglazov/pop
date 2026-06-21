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

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/glebglazov/pop/tasks/binding"
	"github.com/glebglazov/pop/tasks/integration"
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
	// repo's listed worktrees (e.g. an execution_base checkout outside it).
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
		merge, awaitingIntegration := mergeabilityForSet(d, repoKey, taskRow.ID)
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

func mergeabilityForSet(d *Deps, repoKey, setID string) (MergeabilityRecord, bool) {
	if d == nil || d.Tasks == nil {
		return MergeabilityRecord{}, false
	}
	rec, ok, err := integration.GetForSet(d.Tasks, repoKey, setID)
	if err != nil || !ok {
		return MergeabilityRecord{}, false
	}
	return mergeabilityRecordFromIntegration(rec), true
}

type dashboardWorktreeView struct {
	label       string
	runtimePath string
}

// dashboardWorktreeMarker prefixes a branch running in a dedicated (non-trunk)
// Worktree binding. The representative checkout (trunk) is left unmarked.
const dashboardWorktreeMarker = "↳ "

func dashboardWorktree(d *Deps, state *DaemonState, repoKey, setID string, rep *projectScan, repBranch string, bare bool) dashboardWorktreeView {
	if b, ok := bindingForSet(d.Tasks, repoKey, setID); ok && strings.TrimSpace(b.RuntimePath) != "" {
		_ = state
			branch := b.Branch
			if branch == "" {
				branch = binding.CurrentBranch(d.Tasks, b.RuntimePath)
			}
		return dashboardWorktreeView{label: formatDashboardWorktree(branch, true), runtimePath: b.RuntimePath}
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
// shown inline; the detail view carries the broader task-set context.
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
	if b, ok := bindingForSet(d.Tasks, repoKey, setID); ok && b.RuntimePath != "" {
		paths[b.RuntimePath] = true
	}
	_ = state
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
type dashboardDetailMsg struct {
	dashRow  DashboardRow
	manifest *tasks.Manifest
	taskRow  *tasks.Row
	err      error
}

type dashboardTaskTextMsg struct {
	taskID string
	path   string
	text   string
	err    error
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
type dashboardDetailOverrideMsg struct {
	taskID string
	verb   string // "complete", "open", or "skip" (for confirmation text)
	err    error
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

// detailView is the full-screen task-set detail that replaces the table. The
// cursor is pinned by task ID so it survives manifest refreshes.
type detailView struct {
	row      DashboardRow
	manifest *tasks.Manifest
	taskRow  *tasks.Row
	cursorID string
	loading  bool
	err      error
	peek     *taskTextPeek
	// statusMsg is a transient one-line message shown above the hint bar.
	// Set to a hint on invalid transition; set to confirmation on success.
	statusMsg string
}

type taskTextPeek struct {
	taskID  string
	path    string
	text    string
	loading bool
	err     error
	scroll  int
}

// cursorIndex returns the index of the cursor task in the manifest, or 0.
func (d *detailView) cursorIndex() int {
	if d.manifest == nil {
		return 0
	}
	for i, t := range d.manifest.Tasks {
		if t.ID == d.cursorID {
			return i
		}
	}
	return 0
}

// moveCursor moves the cursor by delta, clamped to valid task indices.
func (d *detailView) moveCursor(delta int) {
	if d.manifest == nil || len(d.manifest.Tasks) == 0 {
		return
	}
	idx := d.cursorIndex() + delta
	if idx < 0 {
		idx = 0
	}
	if idx >= len(d.manifest.Tasks) {
		idx = len(d.manifest.Tasks) - 1
	}
	d.cursorID = d.manifest.Tasks[idx].ID
}

// syncManifest updates the manifest on a tick refresh, keeping cursor on the
// same task ID when possible.
func (d *detailView) syncManifest(m *tasks.Manifest, row *tasks.Row) {
	d.manifest = m
	d.taskRow = row
	if m == nil || len(m.Tasks) == 0 {
		d.cursorID = ""
		return
	}
	for _, t := range m.Tasks {
		if t.ID == d.cursorID {
			return
		}
	}
	d.cursorID = m.Tasks[0].ID
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
	detail  *detailView

	filterMode  bool
	filterInput textinput.Model
	pendingG    bool
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
			m.pendingG = false
			return m.updateBindModal(msg)
		}
		if m.abandon != nil {
			m.pendingG = false
			return m.updateAbandonModal(msg)
		}
		if m.detail != nil {
			return m.updateDetailView(msg)
		}
		if m.filterMode {
			m.pendingG = false
			return m.updateFilterMode(msg)
		}
		if msg.String() == "g" {
			if m.pendingG {
				m.pendingG = false
				if len(m.snap.Rows) > 0 {
					m.cursor = 0
				}
			} else {
				m.pendingG = true
			}
			return m, nil
		}
		m.pendingG = false
		switch msg.String() {
		case "ctrl+c", "esc", "h", "left":
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
		case "G":
			if len(m.snap.Rows) > 0 {
				m.cursor = len(m.snap.Rows) - 1
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
		case "l", "enter":
			if len(m.snap.Rows) == 0 || m.cursor < 0 || m.cursor >= len(m.snap.Rows) {
				return m, nil
			}
			m.err = nil
			row := m.snap.Rows[m.cursor]
			m.detail = &detailView{row: row, loading: true}
			return m, m.loadDetail(row)
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
		cmds := []tea.Cmd{dashboardTick(), m.reload()}
		if m.detail != nil {
			cmds = append(cmds, m.loadDetail(m.detail.row))
		}
		return m, tea.Batch(cmds...)
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
			if m.detail != nil {
				for _, row := range m.snap.Rows {
					if row.cursorKey == m.detail.row.cursorKey {
						m.detail.row = row
						break
					}
				}
			}
		}
	case dashboardDetailMsg:
		if m.detail == nil {
			return m, nil
		}
		if msg.err != nil {
			m.detail.loading = false
			m.detail.err = msg.err
			return m, nil
		}
		m.detail.syncManifest(msg.manifest, msg.taskRow)
		m.detail.loading = false
		m.detail.err = nil
		return m, nil
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
	case dashboardDetailOverrideMsg:
		if m.detail == nil {
			return m, nil
		}
		if msg.err != nil {
			m.detail.statusMsg = fmt.Sprintf("error: %v", msg.err)
		} else {
			m.detail.statusMsg = fmt.Sprintf("%s applied to %s", msg.verb, msg.taskID)
		}
		return m, m.loadDetail(m.detail.row)
	case dashboardTaskTextMsg:
		if m.detail == nil || m.detail.peek == nil {
			return m, nil
		}
		m.detail.peek.loading = false
		m.detail.peek.taskID = msg.taskID
		m.detail.peek.path = msg.path
		m.detail.peek.text = msg.text
		m.detail.peek.err = msg.err
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

func (m dashboardModel) updateDetailView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
			m.moveTaskTextPeek(1)
		case "k", "up":
			m.moveTaskTextPeek(-1)
		case "ctrl+d":
			m.moveTaskTextPeek(halfPageDelta(m.taskTextPeekPageSize()))
		case "ctrl+u":
			m.moveTaskTextPeek(-halfPageDelta(m.taskTextPeekPageSize()))
		case "G":
			m.detail.peek.scroll = m.maxTaskTextPeekScroll()
		}
		return m, nil
	}
	if msg.String() == "g" {
		if m.pendingG {
			m.pendingG = false
			if m.detail != nil && m.detail.manifest != nil && len(m.detail.manifest.Tasks) > 0 {
				m.detail.cursorID = m.detail.manifest.Tasks[0].ID
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
			m.detail.moveCursor(1)
		}
	case "k", "up":
		if m.detail != nil {
			m.detail.moveCursor(-1)
		}
	case "G":
		if m.detail != nil && m.detail.manifest != nil && len(m.detail.manifest.Tasks) > 0 {
			m.detail.cursorID = m.detail.manifest.Tasks[len(m.detail.manifest.Tasks)-1].ID
		}
	case "l", "enter":
		if m.detail == nil || m.detail.loading || m.detail.manifest == nil {
			return m, nil
		}
		idx := m.detail.cursorIndex()
		if idx < 0 || idx >= len(m.detail.manifest.Tasks) {
			return m, nil
		}
		task := m.detail.manifest.Tasks[idx]
		m.detail.peek = &taskTextPeek{taskID: task.ID, loading: true}
		return m, m.loadTaskText(m.detail.manifest, task)
	case "C", "O", "K":
		return m.handleDetailOverrideKey(msg.String())
	}
	return m, nil
}

// handleDetailOverrideKey applies C/O/K override verbs to the cursored task.
// Invalid transitions set a status-line hint and perform no mutation.
func (m dashboardModel) handleDetailOverrideKey(key string) (tea.Model, tea.Cmd) {
	if m.detail == nil || m.detail.manifest == nil || m.detail.loading {
		return m, nil
	}
	idx := m.detail.cursorIndex()
	if idx < 0 || idx >= len(m.detail.manifest.Tasks) {
		return m, nil
	}
	task := m.detail.manifest.Tasks[idx]

	switch key {
	case "C":
		if task.Status == "done" {
			m.detail.statusMsg = fmt.Sprintf("task %q is already done", task.ID)
			return m, nil
		}
	case "O":
		if task.Status != "failed" && task.Status != "skipped" {
			m.detail.statusMsg = fmt.Sprintf("task %q is %s; open requires failed or skipped", task.ID, task.Status)
			return m, nil
		}
	case "K":
		if task.Status != "open" {
			m.detail.statusMsg = fmt.Sprintf("task %q is %s; skip requires an open task", task.ID, task.Status)
			return m, nil
		}
	}

	m.detail.statusMsg = ""
	return m, m.applyDetailOverride(m.detail.row, task, key)
}

// applyDetailOverride dispatches the C/O/K override verb to the appropriate
// tasks.*With function via the Deps seam.
func (m dashboardModel) applyDetailOverride(row DashboardRow, task tasks.Task, verb string) tea.Cmd {
	d := m.d
	if d == nil {
		d = DefaultDeps()
	}
	taskPath := row.SetID + "/" + task.File
	return func() tea.Msg {
		var err error
		switch verb {
		case "C":
			err = d.completeDetailTask(row.defPath, taskPath)
		case "O":
			err = d.resetDetailTask(row.defPath, taskPath)
		case "K":
			err = d.skipDetailTask(row.defPath, taskPath)
		}
		verbName := map[string]string{"C": "complete", "O": "open", "K": "skip"}[verb]
		return dashboardDetailOverrideMsg{taskID: task.ID, verb: verbName, err: err}
	}
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

func (m dashboardModel) moveTaskTextPeek(delta int) {
	if m.detail == nil || m.detail.peek == nil || delta == 0 {
		return
	}
	m.detail.peek.scroll += delta
	if m.detail.peek.scroll < 0 {
		m.detail.peek.scroll = 0
	}
	if maxScroll := m.maxTaskTextPeekScroll(); m.detail.peek.scroll > maxScroll {
		m.detail.peek.scroll = maxScroll
	}
}

func (m dashboardModel) maxTaskTextPeekScroll() int {
	if m.detail == nil || m.detail.peek == nil {
		return 0
	}
	lines := taskTextPeekLines(m.detail.peek.text)
	maxScroll := len(lines) - m.taskTextPeekPageSize()
	if maxScroll < 0 {
		return 0
	}
	return maxScroll
}

func (m dashboardModel) taskTextPeekPageSize() int {
	if m.detail == nil || m.detail.peek == nil {
		return 1
	}
	return taskTextPeekPageSize(m.height, m.detail.peek.path)
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

func (m dashboardModel) loadDetail(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		d := m.d
		if d == nil {
			d = DefaultDeps()
		}
		if d.Tasks == nil {
			d.Tasks = tasks.DefaultDeps()
		}
		refresh, err := d.refresh(row.defPath)
		if err != nil {
			return dashboardDetailMsg{dashRow: row, err: err}
		}
		taskRow := tasks.FindRow(refresh, row.SetID)
		manifest := refresh.Manifests[row.SetID]
		return dashboardDetailMsg{dashRow: row, manifest: manifest, taskRow: taskRow}
	}
}

func (m dashboardModel) loadTaskText(manifest *tasks.Manifest, task tasks.Task) tea.Cmd {
	return func() tea.Msg {
		if manifest == nil {
			return dashboardTaskTextMsg{taskID: task.ID, err: fmt.Errorf("manifest not loaded")}
		}
		d := m.d
		if d == nil {
			d = DefaultDeps()
		}
		if d.Tasks == nil {
			d.Tasks = tasks.DefaultDeps()
		}
		path := filepath.Join(manifest.Dir, task.File)
		data, err := d.Tasks.FS.ReadFile(path)
		if err != nil {
			return dashboardTaskTextMsg{taskID: task.ID, path: path, err: err}
		}
		return dashboardTaskTextMsg{taskID: task.ID, path: path, text: string(data)}
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
	dec := Decision{
		Project:   repoName(scans, rep),
		TaskSetID: row.SetID,
	}
	if b, ok := bindingForSet(d.Tasks, repoKey, row.SetID); ok && strings.TrimSpace(b.RuntimePath) != "" {
		if err := validateBoundWorktree(d, scans[0].ProjectPath, b); err != nil {
			return DashboardDrainResult{}, fmt.Errorf("bound worktree for %s is invalid (%v); repair git state or run `pop tasks unbind-worktree`", row.SetID, err)
		}
		worktreeReady, configErr := readRepoConfig(d, scans[0].ProjectPath)
		if configErr != "" {
			worktreeReady = false
		}
		sessionName := project.SessionNameWith(d.Project, b.RuntimePath)
		if worktreeReady && rep != nil {
			sessionName = rep.SessionName
		}
		dec.WorktreeReady = worktreeReady
		dec.scan = projectScan{
			Name:           dec.Project,
			ProjectPath:    b.RuntimePath,
			DefinitionPath: scans[0].DefinitionPath,
			RuntimePath:    b.RuntimePath,
			SessionName:    sessionName,
			RepoKey:        repoKey,
		}
	} else {
		if rep == nil {
			if bare {
				return DashboardDrainResult{}, fmt.Errorf("%s", repoScanReason)
			}
			return DashboardDrainResult{}, fmt.Errorf("no execution base checkout")
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
	key := setScopedKey(repoKey, row.SetID)
	if err := binding.Put(d.Tasks, key, binding.Binding{RuntimePath: path, Branch: branch, Project: proj, Provisioned: true}); err != nil {
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
	paths := map[string]bool{}
	if strings.TrimSpace(row.runtimePath) != "" {
		paths[row.runtimePath] = true
	}
	if row.repoKey != "" {
		if b, ok := bindingForSet(d.Tasks, row.repoKey, row.SetID); ok && b.RuntimePath != "" {
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
	if m.detail != nil {
		content := m.viewDetail()
		v := tea.NewView(content)
		v.AltScreen = true
		return v
	}
	var body strings.Builder
	if m.err != nil {
		fmt.Fprintf(&body, "refresh error: %v\n", m.err)
	}
	if len(m.snap.Rows) == 0 {
		if m.filterMode {
			fmt.Fprintln(&body, "No matching task sets.")
		} else {
			fmt.Fprintln(&body, "No queue-actionable task sets.")
		}
		hint := "h/esc quit"
		if m.filterMode {
			ui.WriteInputBox(&body, m.width, m.filterInput.View())
			hint = "esc clear filter"
		}
		writeDashboardFooter(&body, m.height, ui.HintStyle.Render(hint))
		content := body.String()
		v := tea.NewView(content)
		v.AltScreen = true
		return v
	}
	fmt.Fprintf(&body, "Queue · %s\n", dashboardSummary(m.snap.Rows))
	fmt.Fprintln(&body)
	renderDashboardTable(&body, m.snap.Rows, m.cursor, m.width)
	hint := "j/k move · gg/G top/bottom · l/enter status · i drain · p preview · b bind worktree · U unbind worktree · a auto-drain · / filter · h/esc quit"
	footer := true
	if m.bind != nil {
		renderDashboardBindModal(&body, m.bind)
		footer = false
	} else if m.abandon != nil {
		renderDashboardAbandonModal(&body, m.abandon)
		footer = false
	} else if m.filterMode {
		ui.WriteInputBox(&body, m.width, m.filterInput.View())
		hint = "esc clear filter · j/k navigate"
	}
	if footer {
		writeDashboardFooter(&body, m.height, ui.HintStyle.Render(hint))
	}
	content := body.String()
	v := tea.NewView(content)
	v.AltScreen = true
	return v
}

func dashboardSummary(rows []DashboardRow) string {
	total := len(rows)
	ready := 0
	running := 0
	autoDrain := 0
	integration := 0
	for _, row := range rows {
		if row.Status == string(tasks.StatusReady) {
			ready++
		}
		if strings.TrimSpace(row.Drain) != "" {
			running++
		}
		if row.AutoDrain {
			autoDrain++
		}
		if row.integrationBacklog {
			integration++
		}
	}
	parts := []string{countPhrase(total, "task set", "task sets")}
	if ready > 0 {
		parts = append(parts, countPhrase(ready, "ready", "ready"))
	}
	if running > 0 {
		parts = append(parts, countPhrase(running, "running", "running"))
	}
	if autoDrain > 0 {
		parts = append(parts, countPhrase(autoDrain, "auto-drain", "auto-drain"))
	}
	if integration > 0 {
		parts = append(parts, countPhrase(integration, "awaiting integration", "awaiting integration"))
	}
	return strings.Join(parts, " · ")
}

func countPhrase(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}

// viewDetail renders the full-screen task-set detail view.
func (m dashboardModel) viewDetail() string {
	var b strings.Builder
	d := m.detail
	if d.peek != nil {
		renderTaskTextPeek(&b, d, m.height, m.width)
		return b.String()
	}
	if d.loading {
		fmt.Fprintf(&b, "Loading %s...\n", d.row.SetID)
		writeDashboardFooter(&b, m.height, ui.HintStyle.Render("  h/esc back"))
		return b.String()
	}
	if d.err != nil {
		fmt.Fprintf(&b, "error loading %s: %v\n", d.row.SetID, d.err)
		writeDashboardFooter(&b, m.height, ui.HintStyle.Render("  h/esc back"))
		return b.String()
	}
	renderDetailContent(&b, d, m.height)
	return b.String()
}

// renderDetailContent renders the task list with cursor indicators for the
// detail view. The cursor is on the task identified by detailView.cursorID.
func renderDetailContent(b *strings.Builder, d *detailView, height int) {
	manifest := d.manifest
	taskRow := d.taskRow

	status := tasks.DeriveStatus(manifest)
	progress := ""
	if taskRow != nil {
		status = taskRow.Status
		progress = taskRow.Progress
	}

	header := fmt.Sprintf("Task · %s  [%s]", d.row.SetID, status)
	if progress != "" {
		header += "  " + progress
	}
	fmt.Fprintln(b, header)

	if status == tasks.StatusMissing {
		fmt.Fprintln(b, "  registered task set missing")
		writeDashboardFooter(b, height, ui.HintStyle.Render("  h/esc back"))
		return
	}
	if manifest == nil || !manifest.Valid {
		fmt.Fprintln(b, "  malformed manifest")
		if manifest != nil {
			for _, e := range manifest.Errors {
				fmt.Fprintf(b, "  - %s\n", e)
			}
		}
		writeDashboardFooter(b, height, ui.HintStyle.Render("  h/esc back"))
		return
	}

	fmt.Fprintln(b)

	const (
		stW    = 10
		tyW    = 4
		titleW = 40
	)
	idW := len("ID")
	for _, t := range manifest.Tasks {
		if len(t.ID) > idW {
			idW = len(t.ID)
		}
	}

	fmt.Fprintf(b, "  %-*s  %-*s  %-*s  %-*s  %s\n",
		stW, "STATUS", tyW, "TYPE", idW, "ID", titleW, "TITLE", "BLOCKED-BY")
	fmt.Fprintf(b, "  %-*s  %-*s  %-*s  %-*s  %s\n",
		stW, strings.Repeat("-", stW),
		tyW, strings.Repeat("-", tyW),
		idW, strings.Repeat("-", idW),
		titleW, strings.Repeat("-", titleW),
		strings.Repeat("-", 12))

	cursorIdx := d.cursorIndex()
	for i, t := range manifest.Tasks {
		prefix := "  "
		if i == cursorIdx {
			prefix = ui.IndicatorStyle.Render("█") + " "
		}
		title := t.Title
		if len(title) > titleW {
			title = title[:titleW-3] + "..."
		}
		blockedBy := "-"
		if len(t.BlockedBy) > 0 {
			blockedBy = strings.Join(t.BlockedBy, ", ")
		}
		statusCell := t.Status
		if t.Status == "failed" && t.FailedAfter != nil {
			statusCell = fmt.Sprintf("failed(%d)", *t.FailedAfter)
		}
		line := fmt.Sprintf("%-*s  %-*s  %-*s  %-*s  %s",
			stW, statusCell, tyW, t.Type, idW, t.ID, titleW, title, blockedBy)
		fmt.Fprintf(b, "%s%s\n", prefix, line)
	}

	fmt.Fprintln(b)
	if d.statusMsg != "" {
		fmt.Fprintf(b, "  %s\n", d.statusMsg)
	}
	writeDashboardFooter(b, height, ui.HintStyle.Render("  j/k · gg/G top/bottom · l/enter peek · C complete · O open · K skip · h/esc back"))
}

func renderTaskTextPeek(b *strings.Builder, d *detailView, height, width int) {
	p := d.peek
	header := d.row.SetID
	if p.taskID != "" {
		header += " / " + p.taskID
	}
	fmt.Fprintln(b, header)
	if p.loading {
		fmt.Fprintln(b, "  loading task text...")
		writeDashboardFooter(b, height, ui.HintStyle.Render("  h/esc back"))
		return
	}
	if p.err != nil {
		fmt.Fprintf(b, "  error loading task text: %v\n", p.err)
		if p.path != "" {
			fmt.Fprintf(b, "  %s\n", p.path)
		}
		writeDashboardFooter(b, height, ui.HintStyle.Render("  h/esc back"))
		return
	}
	if p.path != "" {
		fmt.Fprintf(b, "  %s\n\n", p.path)
	}
	lines := taskTextPeekLines(p.text)
	pageSize := taskTextPeekPageSize(height, p.path)
	maxScroll := len(lines) - pageSize
	if maxScroll < 0 {
		maxScroll = 0
	}
	if p.scroll > maxScroll {
		p.scroll = maxScroll
	}
	if p.scroll < 0 {
		p.scroll = 0
	}
	if len(lines) == 0 {
		fmt.Fprintln(b, "  (empty task file)")
	} else {
		end := p.scroll + pageSize
		if end > len(lines) {
			end = len(lines)
		}
		for _, line := range lines[p.scroll:end] {
			fmt.Fprintln(b, truncateToWidth(line, width))
		}
	}
	fmt.Fprintln(b)
	position := ""
	if maxScroll > 0 {
		position = fmt.Sprintf(" · %d/%d", p.scroll+1, len(lines))
	}
	writeDashboardFooter(b, height, ui.HintStyle.Render("  j/k · C-d/C-u · gg/G · h/esc back"+position))
}

func taskTextPeekLines(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func taskTextPeekPageSize(height int, path string) int {
	if height <= 0 {
		height = 20
	}
	pathLines := 0
	if path != "" {
		pathLines = 2
	}
	pageSize := height - 1 /* title */ - 1 /* header */ - pathLines - 1 /* hint */
	if pageSize < 1 {
		return 1
	}
	return pageSize
}

func halfPageDelta(pageSize int) int {
	if pageSize <= 1 {
		return 1
	}
	return pageSize / 2
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
	headers := []string{"PROJECT", "TASK SET", "STATUS", "WORKTREE", "DRAIN", ""}
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
	fmt.Fprintf(w, "%s\n", truncateToWidth("  "+dashboardTableSeparator(widths), width))
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

func writeDashboardFooter(b *strings.Builder, height int, hint string) {
	if height > 0 {
		lines := strings.Count(b.String(), "\n")
		for lines < height-1 {
			b.WriteByte('\n')
			lines++
		}
		if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n") {
			b.WriteByte('\n')
		}
	} else if b.Len() > 0 {
		b.WriteByte('\n')
	}
	fmt.Fprint(b, hint)
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

func dashboardTableSeparator(widths []int) string {
	parts := make([]string, len(widths))
	for i, width := range widths {
		parts[i] = strings.Repeat("-", width)
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
