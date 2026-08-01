package queue

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/glebglazov/pop/config"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
	"github.com/glebglazov/pop/ui"
	"github.com/glebglazov/pop/wayfinder"
	"github.com/glebglazov/pop/work"
)

const dashboardPollInterval = 2 * time.Second

// dashboardHandoffPending is shown from the moment a Handoff verb dispatches
// until it either quits the dashboard or reports why it could not. A handoff
// ends the surface that could report progress, so without this a slow one is
// indistinguishable from a key that did nothing — which is exactly how the
// pre-ADR-0167 drain latency was experienced (ADR-0167).
const dashboardHandoffPending = "handing off…"

// The Work dashboard's row model and pure derivation live in the top-level work
// package (ADR-0143); queue keeps these aliases so its row building, TUI model,
// and static status render read one set of types. The exports drop the Dashboard
// prefix on the way into work (work.Row / work.Snapshot / work.SetRef); the
// aliases preserve queue's local vocabulary and its exported surface
// (queue.DashboardRow / DashboardSnapshot / SetRef) for consumers like
// dashboardshell and cmd.
type (
	SetRef            = work.SetRef
	DashboardRow      = work.Row
	DashboardSnapshot = work.Snapshot
)

// dashboardRepresentative resolves a repo group's integration target without
// forking git (ADR-0060): a per-checkout `trunk = true` override wins (bare or
// not), else a non-bare repo's target is the main worktree — the parent of the
// common directory — and a bare repo with no declared trunk has none (bare=true,
// rep=nil). A renamed execution key surfaces as a fatal config finding, matching
// resolveRepresentative's contract.
func dashboardRepresentative(d *Deps, cfg *config.Config, commonDir string, scans []projectScan) (*projectScan, bool, error) {
	if cfg != nil && len(scans) > 0 {
		if _, err := resolveRepoConfigFor(d, cfg, scans[0].ProjectPath); err != nil {
			var f config.Finding
			if errors.As(err, &f) {
				return nil, false, err
			}
		}
	}

	// 1. explicit trunk = true checkout (config-only, no git).
	for i := range scans {
		rc, err := resolveRepoConfigFor(d, cfg, scans[i].ProjectPath)
		if err == nil && rc.Trunk {
			return &scans[i], false, nil
		}
	}

	// 2. non-bare repo → main worktree = parent of the common directory. A normal
	// repo's common dir is `<root>/.git`; only that layout has a derivable main
	// worktree fork-free. Anything else (`.bare`, top-level bare) is bare.
	if filepath.Base(commonDir) == ".git" {
		return dashboardScanForCheckout(d, scans, filepath.Dir(commonDir)), false, nil
	}

	// 3. bare repo with no declared trunk → no integration target.
	return nil, true, nil
}

// dashboardScanForCheckout returns the scan whose checkout canonicalizes to
// checkoutPath, or synthesizes one (fork-free) when the target — e.g. a main
// worktree that is not itself a picker Project — is not among the group's scans.
func dashboardScanForCheckout(d *Deps, scans []projectScan, checkoutPath string) *projectScan {
	canon, err := canonicalCheckoutPath(d.Tasks, checkoutPath)
	if err != nil {
		canon = checkoutPath
	}
	for i := range scans {
		if c, err := canonicalCheckoutPath(d.Tasks, scans[i].ProjectPath); err == nil && c == canon {
			return &scans[i]
		}
	}
	name, label := "", ""
	if len(scans) > 0 {
		name = scans[0].Name
		label = scans[0].ProjectLabel
	}
	// SessionName is left unset for the same reason as in dashboardRepoStatics:
	// deriving it forks git and the build path never reads it.
	return &projectScan{
		Name:         name,
		ProjectLabel: label,
		ProjectPath:  canon,
		RuntimePath:  canon,
	}
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

// sortDashboardRows applies the shared Queue surface order to a dashboard
// build's rows. The comparator — the ADR-0121 membership tiers, status bands,
// intra-project status order, and SetID tiebreak — lives in the work data core
// (ADR-0143); this is the queue-side seam both BuildDashboard and the static
// status render key on.
func sortDashboardRows(rows []DashboardRow) {
	work.SortRows(rows)
}

// WorkDeps projects the supervisor's Deps onto the Work data core's Deps
// (ADR-0143): queue is a consumer of work, so it forwards the store handle, the
// config loader, the Done-inclusion flag, and every build seam the dashboard
// tests inject, letting work.BuildSnapshot derive the same rows the removed
// queue-side builder did. The borrow is by pointer — work never closes the
// process-cached store handle it reaches through Tasks (ADR-0140).
func (d *Deps) WorkDeps() *work.Deps {
	if d == nil {
		return work.DefaultDeps()
	}
	return &work.Deps{
		Tasks:          d.Tasks,
		Project:        d.Project,
		LoadConfig:     d.LoadConfig,
		IncludeDone:    d.IncludeDone,
		Refresh:        d.Refresh,
		LiveDrains:     d.LiveDrains,
		Reconcile:      d.Reconcile,
		ReconcileOut:   d.ReconcileOut,
		Now:            d.Now,
		ProbeDirective: d.ProbeDirective,
	}
}

type dashboardTickMsg struct{}

// dashboardLivePrimeMsg carries the open-time live-pane cache, ahead of the
// first poll's full snapshot rebuild.
type dashboardLivePrimeMsg struct {
	live livePaneCache
}
type dashboardRowsMsg struct {
	snap DashboardSnapshot
	live livePaneCache
	err  error
}

func (m QueueDashboard) liveCache() livePaneCache {
	if m.live == nil {
		return livePaneCache{}
	}
	return *m.live
}
type dashboardToggleMsg struct {
	key       string
	autoDrain bool
	err       error
}
type dashboardHandoffMsg struct {
	// quit is set when focus succeeded inside tmux; the dashboard exits.
	quit bool
	// status explains why the dashboard stays open (outside tmux, nothing to
	// focus). Empty when quitting or when err is set.
	status string
	err    error
}
type dashboardUnparkMsg struct {
	setID string
	err   error
}
type dashboardArchiveMsg struct {
	setID string
	err   error
}
type dashboardStatusMsg struct {
	setID string
	verb  string
	err   error
}
type dashboardDetailMsg struct {
	dashRow  DashboardRow
	manifest *tasks.Manifest
	taskRow  *tasks.Row
	wfMap    *wayfinder.Map
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
type dashboardDrainListMsg struct {
	row     DashboardRow
	entries []dashboardDrainEntry
	err     error
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
	Label   string
	Path    string
	Branch  string
	Create  bool
	Managed bool
}

type dashboardBindModal struct {
	row   DashboardRow
	stage dashboardBindStage
	// list drives the worktree-pick and base-ref-pick stages (both wrapping).
	// Base refs are held as entries with only Label set. The name stage is a
	// plain text input and does not use the list.
	list    *ui.List[dashboardBindEntry]
	baseRef string
	name    string
	loading bool
}

// bindEntryCell renders one bind-modal row: the worktree label (falling back to
// the checkout path) or, in the base-ref stage, the ref held in Label.
func bindEntryCell(e dashboardBindEntry, _ ui.RowState) string {
	if e.Label != "" {
		return e.Label
	}
	return e.Path
}

// newBindEntryList builds the wrapping list backing a bind-modal list stage.
func newBindEntryList(entries []dashboardBindEntry) *ui.List[dashboardBindEntry] {
	return ui.NewList(entries, ui.Opts[dashboardBindEntry]{
		Wrap: true,
		Cell: bindEntryCell,
	})
}

// bindRefEntries wraps base refs as bind entries so the base-ref stage reuses
// the same wrapping list as the worktree-pick stage.
func bindRefEntries(refs []string) []dashboardBindEntry {
	entries := make([]dashboardBindEntry, len(refs))
	for i, ref := range refs {
		entries[i] = dashboardBindEntry{Label: ref}
	}
	return entries
}

// dashboardDrainTargetKind identifies one Drain target picker option (ADR-0052).
type dashboardDrainTargetKind int

const (
	// drainTargetWorktree adopts an existing non-managed, unbound worktree.
	drainTargetWorktree dashboardDrainTargetKind = iota
	// drainTargetNewManaged provisions a managed worktree forked from the trunk.
	drainTargetNewManaged
	// drainTargetTrunk drains inline in the trunk worktree with no binding.
	drainTargetTrunk
)

type dashboardDrainEntry struct {
	Label  string
	Kind   dashboardDrainTargetKind
	Path   string // adopt target checkout (drainTargetWorktree only)
	Branch string
}

// dashboardDrainModal is the Drain target picker shown when `i` is pressed on an
// unbound set: pick an existing worktree to adopt, a new managed worktree to
// provision off the trunk (the default cursor), or the trunk itself — then bind
// (or stay unbound for trunk) and drain in one action. A bound set skips the
// picker and resumes in its binding (ADR-0052).
type dashboardDrainModal struct {
	row     DashboardRow
	list    *ui.List[dashboardDrainEntry]
	loading bool
}

// newDashboardDrainModal builds the Drain target picker with a wrapping list,
// positioning the cursor on "new managed worktree" — the frictionless default.
func newDashboardDrainModal(row DashboardRow, entries []dashboardDrainEntry) *dashboardDrainModal {
	list := ui.NewList(entries, ui.Opts[dashboardDrainEntry]{
		Wrap: true,
		Cell: func(e dashboardDrainEntry, _ ui.RowState) string {
			if e.Label != "" {
				return e.Label
			}
			return e.Path
		},
	})
	list.SetCursor(defaultDrainCursor(entries))
	return &dashboardDrainModal{row: row, list: list}
}

type dashboardAbandonModal struct {
	row     DashboardRow
	loading bool
}

// dashboardMenuAction identifies the verb a menu item dispatches.
type dashboardMenuAction int

const (
	menuActionDrain dashboardMenuAction = iota
	menuActionVerify
	menuActionBind
	menuActionUnbind
	menuActionAutoDrain
	menuActionStatusSubmenu
	menuActionAssist
	menuActionFold
	menuActionUnpark
	menuActionShell
	menuActionArchive
	menuActionCopyName
)

// dashboardMenuItem is one verb in the action menu overlay: the flat shortcut
// letter it keeps, the label shown beside it, and the verb it dispatches.
type dashboardMenuItem struct {
	key    string
	label  string
	action dashboardMenuAction
}

// dashboardStatusAction identifies a task-set status verb in the status submenu.
type dashboardStatusAction int

const (
	statusActionComplete dashboardStatusAction = iota
	statusActionOpen
	statusActionSkip
	statusActionArchive
	statusActionUnarchive
)

// dashboardStatusMenuItem is one verb in the status submenu.
type dashboardStatusMenuItem struct {
	key    string
	label  string
	action dashboardStatusAction
	verb   string // pop tasks subcommand
}

// dashboardStatusMenu is the nested status overlay opened with `s` from the
// action menu. Status verbs write in-process (ADR-0158) — complete/open/skip
// apply every unlocked task in the set; archive/unarchive flip the set flag.
type dashboardStatusMenu struct {
	row  DashboardRow
	list *ui.List[dashboardStatusMenuItem]
}

func dashboardStatusMenuItems() []dashboardStatusMenuItem {
	return []dashboardStatusMenuItem{
		{key: "c", label: "complete", action: statusActionComplete, verb: "complete"},
		{key: "o", label: "open", action: statusActionOpen, verb: "open"},
		{key: "k", label: "skip", action: statusActionSkip, verb: "skip"},
		{key: "x", label: "archive", action: statusActionArchive, verb: "archive"},
		{key: "u", label: "unarchive", action: statusActionUnarchive, verb: "unarchive"},
	}
}

func newDashboardStatusMenu(row DashboardRow) *dashboardStatusMenu {
	return &dashboardStatusMenu{
		row:  row,
		list: ui.NewList(dashboardStatusMenuItems(), ui.Opts[dashboardStatusMenuItem]{Wrap: true}),
	}
}

// dashboardMenu is the layered action overlay opened with `a` (one-shot) or `A`
// (pinned) over the focused row. It carries the snapshot of the row it was
// opened on and the verbs applicable to that row on a ui.List whose cursor
// drives j/k + Enter selection. When pinned, J/K move the dashboard row cursor
// beneath the menu and the verb list re-filters to each row's context. When
// status is non-nil the status submenu is open over the action menu.
type dashboardMenu struct {
	row    DashboardRow
	list   *ui.List[dashboardMenuItem]
	status *dashboardStatusMenu
	pinned bool
}

// Row-verb key case (ADR-0158): uppercase = handoff (spawns/focuses a pane, quits
// the dashboard); lowercase = in-place (acts and leaves the dashboard standing).
// Mode and navigation keys — action menu `a`, filter `/` and `f`, search, `G`/`gg`
// top/bottom — are outside this rule and keep their own casing.
//
// dashboardMenuItems returns the verbs applicable to row, in a stable order.
// Conditional verbs are filtered to the row's context: verify only for
// NEEDS-VERIFY / VERIFY-FAILED rows with no live drain, unbind only for bound
// rows, auto-drain only for non-orphaned rows, and unpark only for parked rows.
// Drain, bind, the runtime shell, and archive apply to every Task-set
// row regardless of status. Map rows carry only copy name — queue verbs
// (drain/bind/…) stay inert on them (ADR-0130).
func dashboardMenuItems(row DashboardRow) []dashboardMenuItem {
	if row.IsMap {
		return []dashboardMenuItem{
			{key: "y", label: "copy name", action: menuActionCopyName},
		}
	}
	items := []dashboardMenuItem{
		{key: "I", label: "drain", action: menuActionDrain},
	}
	// Verify is the lighter, explicit Verifier force (ADR-0123): offered only on
	// rows a verdict can move (NEEDS-VERIFY / VERIFY-FAILED) and hidden while a
	// live drain holds the set — a plain verify is not quiescence-gated, so the
	// running drain verifies itself instead.
	if dashboardVerifyEligible(row) {
		items = append(items, dashboardMenuItem{key: "V", label: "verify", action: menuActionVerify})
	}
	items = append(items, dashboardMenuItem{key: "b", label: "bind worktree", action: menuActionBind})
	if row.Bound {
		items = append(items, dashboardMenuItem{key: "u", label: "unbind worktree", action: menuActionUnbind})
	}
	if !row.Orphaned {
		items = append(items, dashboardMenuItem{key: "a", label: "auto-drain", action: menuActionAutoDrain})
	}
	items = append(items, dashboardMenuItem{key: "s", label: "status ▸", action: menuActionStatusSubmenu})
	items = append(items, dashboardMenuItem{key: "S", label: "assist", action: menuActionAssist})
	if dashboardFoldEligible(row) {
		items = append(items, dashboardMenuItem{key: "F", label: "fold", action: menuActionFold})
	}
	if row.Parked {
		items = append(items, dashboardMenuItem{key: "r", label: "unpark", action: menuActionUnpark})
	}
	items = append(items, dashboardMenuItem{key: "O", label: "shell", action: menuActionShell})
	items = append(items, dashboardMenuItem{key: "x", label: "archive", action: menuActionArchive})
	items = append(items, dashboardMenuItem{key: "y", label: "copy name", action: menuActionCopyName})
	return items
}

// dashboardVerifyEligible reports whether the verify verb applies to row: a set
// a verdict can still move (NEEDS-VERIFY or VERIFY-FAILED) that no live drain
// holds (ADR-0123). It is the single guard shared by the menu (inclusion) and
// dispatch (self-containment).
func dashboardVerifyEligible(row DashboardRow) bool {
	if row.LiveDrain {
		return false
	}
	return row.RawStatus == tasks.StatusNeedsVerify || row.RawStatus == tasks.StatusVerifyFailed
}

// dashboardFoldEligible reports whether the fold verb applies to row: a bound DONE
// or AWAITING-APPROVAL set (ADR-0148, ADR-0156).
func dashboardFoldEligible(row DashboardRow) bool {
	return row.Bound && tasks.FoldEligibleStatus(row.RawStatus)
}

// dashboardMenuActionHandoff reports whether action is a handoff verb (ADR-0158).
func dashboardMenuActionHandoff(action dashboardMenuAction) bool {
	switch action {
	case menuActionDrain, menuActionVerify, menuActionAssist, menuActionFold, menuActionShell:
		return true
	default:
		return false
	}
}

// newDashboardMenu opens the action overlay on row, wrapping its verbs in a
// ui.List with j/k wrap-around navigation. When pinned is true the menu survives
// in-place verbs and J/K move the row cursor beneath it.
func newDashboardMenu(row DashboardRow, pinned bool) *dashboardMenu {
	return &dashboardMenu{
		row:    row,
		pinned: pinned,
		list:   ui.NewList(dashboardMenuItems(row), ui.Opts[dashboardMenuItem]{Wrap: true}),
	}
}

// syncPinnedMenuRow re-filters the pinned menu to the dashboard's cursored row.
func (m QueueDashboard) syncPinnedMenuRow() (tea.Model, tea.Cmd) {
	if m.menu == nil || !m.menu.pinned {
		return m, nil
	}
	row, ok := m.list.Selected()
	if !ok || len(dashboardMenuItems(row)) == 0 {
		m.menu = nil
		return m, nil
	}
	m.menu.row = row
	m.menu.status = nil
	m.menu.list = ui.NewList(dashboardMenuItems(row), ui.Opts[dashboardMenuItem]{Wrap: true})
	return m, nil
}

// dashboardFilterToggle identifies one row-inclusion view filter the filter
// menu flips. Today the menu carries a single toggle (Show done, wired to the
// ADR-0121 Done-inclusion flag); the enum and the item list are the extension
// point for future inclusion filters (by status, by project).
type dashboardFilterToggle int

const (
	filterToggleShowDone dashboardFilterToggle = iota
)

// dashboardFilterItem is one toggle in the filter menu: the flat shortcut letter
// it keeps, the label shown beside its checkbox, and the view filter it flips.
type dashboardFilterItem struct {
	key    string
	label  string
	toggle dashboardFilterToggle
}

// dashboardFilterMenu is the modal opened with `f` over the Work dashboard. It
// is a sibling of the `a` action menu but holds row-inclusion toggles rather
// than row verbs, so it is not anchored to the cursored row. The toggle state
// lives on the model (m.d.IncludeDone), not the menu — the menu only renders it
// and dispatches flips — so the checkbox reflects the live view every frame.
type dashboardFilterMenu struct {
	list *ui.List[dashboardFilterItem]
}

// dashboardFilterItems returns the inclusion toggles, in a stable order. New
// inclusion filters append here.
func dashboardFilterItems() []dashboardFilterItem {
	return []dashboardFilterItem{
		{key: "d", label: "show done", toggle: filterToggleShowDone},
	}
}

// newDashboardFilterMenu opens the filter modal with j/k wrap-around navigation.
func newDashboardFilterMenu() *dashboardFilterMenu {
	return &dashboardFilterMenu{
		list: ui.NewList(dashboardFilterItems(), ui.Opts[dashboardFilterItem]{Wrap: true}),
	}
}

// taskMenuItem is one verb in the task-level action menu: the flat shortcut
// letter it keeps (also the verb code passed to applyDetailOverride) and the
// label shown beside it.
type taskMenuItem struct {
	key   string
	label string
}

// taskMenu is the action overlay opened with `a` over a single task — in the
// task-set detail view (over the cursored task) or the task text peek (over the
// previewed task). It carries the task snapshot and the verbs applicable to that
// task's status on a ui.List whose cursor drives j/k + Enter selection. inPeek
// marks which view it was opened from so the renderer can place it correctly.
type taskMenu struct {
	task   tasks.Task
	list   *ui.List[taskMenuItem]
	inPeek bool
}

// taskMenuItems returns the task verbs applicable to task, filtered to its
// status: Complete for open/failed/skipped (anything not already done), Open for
// any non-open task (done/failed/skipped, mirroring CanReopen), and Skip for
// open. A done task yields Open.
func taskMenuItems(task tasks.Task) []taskMenuItem {
	var items []taskMenuItem
	if task.Status != tasks.TaskDone {
		items = append(items, taskMenuItem{key: "c", label: "complete"})
	}
	if tasks.CanReopen(task.Status) {
		items = append(items, taskMenuItem{key: "o", label: "open"})
	}
	if task.Status == tasks.TaskOpen {
		items = append(items, taskMenuItem{key: "k", label: "skip"})
	}
	items = append(items, taskMenuItem{key: "y", label: "copy name"})
	return items
}

// newTaskMenu wraps pre-filtered task verbs in a ui.List with j/k wrap-around
// navigation. inPeek records which detail view opened it for placement.
func newTaskMenu(task tasks.Task, items []taskMenuItem, inPeek bool) *taskMenu {
	return &taskMenu{
		task:   task,
		list:   ui.NewList(items, ui.Opts[taskMenuItem]{Wrap: true}),
		inPeek: inPeek,
	}
}

// detailView is the full-screen task-set or Map detail that replaces the table.
// Task-set rows use list keyed by task ID; Map rows use ticketList keyed by
// ticket ID. ReplaceItems re-anchors the cursor by key on refresh (ADR-0079).
type detailView struct {
	row        DashboardRow
	manifest   *tasks.Manifest
	taskRow    *tasks.Row
	list       *ui.List[tasks.Task]
	cols       *detailColumns
	wfMap      *wayfinder.Map
	ticketList *ui.List[wayfinder.Ticket]
	ticketCols *detailTicketColumns
	frontier   map[string]bool
	loading    bool
	err        error
	peek       *taskTextPeek
	// statusMsg is a transient one-line message shown above the hint bar.
	// Set to a hint on invalid transition; set to confirmation on success.
	statusMsg string
}

// detailColumns holds the detail task list's ID-column width, precomputed over the
// manifest tasks (the status/type/title columns are fixed). The List's Cell closure
// closes over a pointer to it so a manifest refresh updates the width in place,
// matching the house pattern (dashboardColumns / pickerCell).
type detailColumns struct {
	idW int
}

// detailTicketColumns holds the Map detail ticket-table name-column width.
type detailTicketColumns struct {
	nameW int
}

// Detail task-table column widths. Status, type, and title are fixed; the ID
// column grows to the widest task ID (floored at the "ID" header).
const (
	detailStatusW = 10
	detailTypeW   = 4
	detailTitleW  = 40
)

// detailTableChromeLines is the number of body lines above the detail List rows:
// the blank line under the header, the column header, and the separator.
const detailTableChromeLines = 3

// newDetailView builds a loading detail view for row. Task sets get an empty task
// List; Maps get an empty ticket List. Content arrives via syncManifest or syncMap.
func newDetailView(row DashboardRow) *detailView {
	if row.IsMap {
		return newMapDetailView(row)
	}
	cols := &detailColumns{idW: len("ID")}
	d := &detailView{row: row, loading: true, cols: cols}
	d.list = ui.NewList([]tasks.Task{}, ui.Opts[tasks.Task]{
		Key:    func(t tasks.Task) string { return t.ID },
		Anchor: ui.AnchorTop,
		Cell: func(t tasks.Task, _ ui.RowState) string {
			return detailTaskLine(t, cols.idW)
		},
	})
	return d
}

func newMapDetailView(row DashboardRow) *detailView {
	cols := &detailTicketColumns{nameW: len("TICKET")}
	d := &detailView{
		row:        row,
		loading:    true,
		ticketCols: cols,
		frontier:   map[string]bool{},
	}
	d.ticketList = ui.NewList([]wayfinder.Ticket{}, ui.Opts[wayfinder.Ticket]{
		Key:    func(t wayfinder.Ticket) string { return t.ID },
		Anchor: ui.AnchorTop,
		Cell: func(t wayfinder.Ticket, _ ui.RowState) string {
			return styledDetailMapTicketLine(t, cols.nameW, d.frontier)
		},
	})
	return d
}

type taskTextPeek struct {
	taskID  string
	path    string
	text    string
	loading bool
	err     error
	scroll  int
	// statusMsg is a transient one-line message shown above the hint bar.
	statusMsg string
}

// ticketByDisplayName returns the Map ticket whose display name matches name.
func (d *detailView) ticketByDisplayName(name string) (wayfinder.Ticket, bool) {
	if d.wfMap == nil {
		return wayfinder.Ticket{}, false
	}
	for _, t := range d.wfMap.Tickets {
		if detailTicketName(t) == name {
			return t, true
		}
	}
	return wayfinder.Ticket{}, false
}

// taskByID returns the manifest task with the given ID, or false if absent.
func (d *detailView) taskByID(id string) (tasks.Task, bool) {
	if d.manifest == nil {
		return tasks.Task{}, false
	}
	for _, t := range d.manifest.Tasks {
		if t.ID == id {
			return t, true
		}
	}
	return tasks.Task{}, false
}

// syncManifest updates the manifest on load or a tick refresh, feeding the tasks
// to the List (which re-anchors the cursor by task ID) and recomputing the ID
// column width. It clears the loading/error state, since a synced manifest is a
// completed load.
func (d *detailView) syncManifest(m *tasks.Manifest, row *tasks.Row) {
	d.manifest = m
	d.taskRow = row
	d.loading = false
	d.err = nil
	var items []tasks.Task
	if m != nil {
		items = m.Tasks
	}
	d.cols.idW = detailIDWidth(items)
	d.list.ReplaceItems(items)
}

// syncMap updates the Map on load or tick refresh, feeding tickets to ticketList
// and recomputing frontier highlighting state.
func (d *detailView) syncMap(m *wayfinder.Map) {
	d.wfMap = m
	d.loading = false
	d.err = nil
	var items []wayfinder.Ticket
	if m != nil {
		items = m.Tickets
	}
	frontier := wayfinder.Frontier(items)
	d.frontier = make(map[string]bool, len(frontier))
	for _, t := range frontier {
		d.frontier[t.ID] = true
	}
	d.ticketCols.nameW = detailTicketNameWidth(items)
	d.ticketList.ReplaceItems(items)
}

// dashboardColumns holds the task-set table's natural column widths (derived from
// content) and the fitted widths clamped to the terminal budget. The List's Cell
// closure closes over a pointer to it so a reload or filter can update widths in
// place without rebuilding the list — matching the house pattern of pickerCell
// closing over its picker.
type QueueDashboard struct {
	d         *Deps
	cfg       *config.Config
	snap      DashboardSnapshot
	allRows   []DashboardRow // source of truth; snap.Rows is the filtered view
	list      *ui.List[DashboardRow]
	cols      *dashboardColumns
	err       error
	// actionErr holds the error from a row verb (unbind, drain, bind, auto-drain
	// toggle, …). Unlike err — the refresh error, which each reload re-evaluates —
	// actionErr is sticky: the periodic reload never touches it, so a refused
	// action stays readable until the operator's next keypress clears it or a
	// newer action result replaces it.
	actionErr error
	width     int
	height    int
	bind      *dashboardBindModal
	drainPick *dashboardDrainModal
	abandon   *dashboardAbandonModal
	detail    *detailView
	menu      *dashboardMenu
	taskMenu  *taskMenu
	filter    *dashboardFilterMenu

	filterMode  bool
	filterInput ui.TextField
	pendingG    bool
	statusMsg   string
	showHelp    bool
	// openCheckout is the bound checkout path chosen with Ctrl-g on the main
	// list. It is set alongside a tea.Quit so RunDashboard can surface it out of
	// the program and the command layer runs the workbench-aware open after the
	// TUI exits (task 02).
	openCheckout string

	// live is the per-poll live-pane affordance cache (ADR-0158): handoff-verb
	// key colours and the row activity cluster read from it. Rebuilt from one
	// tmux ListActivityPanes query per reload — never from DrainPane. A pointer
	// so the List cell closure sees updates on each poll.
	live *livePaneCache

	// copyFunc performs the clipboard write for the `y` copy-name verb. Injected
	// so tests can avoid touching the real tmux / /dev/tty. Defaults to
	// ui.CopyToClipboard.
	copyFunc func(string) error
}

// clipboardCopy returns the model's copy function, defaulting to
// ui.CopyToClipboard.
func (m QueueDashboard) clipboardCopy() func(string) error {
	if m.copyFunc != nil {
		return m.copyFunc
	}
	return ui.CopyToClipboard
}

// rowCopyNamePayload is the bare identifier the copy-name verb writes for a
// dashboard row: the task-set directory name or the Wayfinder map id.
func rowCopyNamePayload(row DashboardRow) string {
	return row.SetID
}

// copyClipboard writes payload via Clipboard copy and returns a transient status
// message naming what was copied, or the error.
func (m QueueDashboard) copyClipboard(payload string) string {
	if err := m.clipboardCopy()(payload); err != nil {
		return fmt.Sprintf("copy failed: %v", err)
	}
	return fmt.Sprintf("copied %s", payload)
}

// taskRefCopyPayload is the paste-ready Task target reference for a task:
// the <task-set>/<file>.md form accepted by pop tasks implement/complete/open.
func taskRefCopyPayload(setID string, task tasks.Task) string {
	return setID + "/" + task.File
}

// ticketCopyNamePayload is the bare ticket id the copy-name verb writes for a
// Map detail row.
func ticketCopyNamePayload(ticket wayfinder.Ticket) string {
	return ticket.ID
}

// copyRowName copies the cursored row's identifier via Clipboard copy and
// returns a transient status message naming what was copied, or the error.
func (m QueueDashboard) copyRowName(row DashboardRow) string {
	return m.copyClipboard(rowCopyNamePayload(row))
}

// TestDashboardRow builds a minimal dashboard row for tests outside the queue
// package. The cursor key mirrors production derivation from project and set ID.
func TestDashboardRow(project, setID string, ref SetRef) DashboardRow {
	if ref.SetID == "" {
		ref.SetID = setID
	}
	return DashboardRow{
		Project:   project,
		CursorKey: project + "\x00" + ref.SetID,
		SetRef:    ref,
	}
}

// NewDashboard constructs a Work dashboard model from a snapshot.
func NewDashboard(d *Deps, cfg *config.Config, snap DashboardSnapshot) QueueDashboard {
	return newQueueDashboard(d, cfg, snap)
}

func newQueueDashboard(d *Deps, cfg *config.Config, snap DashboardSnapshot) QueueDashboard {
	if d == nil {
		d = DefaultDeps()
	}
	cols := &dashboardColumns{}
	cols.syncNatural(snap.Rows)
	live := &livePaneCache{}
	var list *ui.List[DashboardRow]
	list = ui.NewList(snap.Rows, ui.Opts[DashboardRow]{
		Key:    func(r DashboardRow) string { return r.CursorKey },
		Anchor: ui.AnchorTop,
		Cell: func(r DashboardRow, rs ui.RowState) string {
			budget := dashboardListCellBudget(cols.width)
			cache := livePaneCache{}
			if live != nil {
				cache = *live
			}
			if list.LinesPerItem() == 2 {
				line1Widths := dashboardTwoLineFitWidths(dashboardTwoLineNaturalWidths(list.Items()), budget)
				if rs.LineIndex == 1 {
					return ui.TruncateString(dashboardTwoLineRowLine2(r, line1Widths), budget)
				}
				return ui.TruncateString(dashboardTwoLineRowLine1(r, line1Widths, cache), budget)
			}
			return ui.TruncateString(dashboardTableLine(dashboardRowValues(r, cache), cols.widths), budget)
		},
	})
	return QueueDashboard{d: d, cfg: cfg, snap: snap, allRows: snap.Rows, list: list, cols: cols, live: live}
}

// dashboardChromeLines returns the chrome height above the List rows for the
// current render mode.
func (m QueueDashboard) dashboardChromeLines() int {
	if dashboardTwoLineMode(m.snap.Rows, m.width, m.height) {
		return dashboardTwoLineChromeLines
	}
	return dashboardTableChromeLines
}

// syncListRows feeds the current filtered rows to the List (re-anchoring the
// cursor by CursorKey) and recomputes the column widths over them.
func (m QueueDashboard) syncListRows() {
	m.list.ReplaceItems(m.snap.Rows)
	m.cols.syncNatural(m.snap.Rows)
}

// resizeMainList sizes the List to the body budget the Frame leaves, minus the
// table's own header chrome, so the table clamps to the terminal instead of
// overflowing. In two-line mode each List item renders two terminal lines, so
// the List's LinesPerItem is set to 2 and the physical body budget is unchanged.
func (m QueueDashboard) resizeMainList() {
	listH := m.frameSpec().BodyHeight(m.height) - m.dashboardChromeLines()
	if listH < 1 {
		listH = 1
	}
	if dashboardTwoLineMode(m.snap.Rows, m.width, m.height) {
		m.list.SetLinesPerItem(2)
	} else {
		m.list.SetLinesPerItem(1)
	}
	m.list.Resize(listH)
}

// ViewToggleAllowed reports whether v may switch to the Routine dashboard.
func (m QueueDashboard) ViewToggleAllowed() bool {
	return m.bind == nil && m.drainPick == nil && m.abandon == nil &&
		m.detail == nil && m.menu == nil && m.taskMenu == nil && m.filter == nil
}

// OpenCheckout returns the checkout path chosen with Ctrl-g before quit.
func (m QueueDashboard) OpenCheckout() string {
	return m.openCheckout
}

// ListCursor exposes the main-list cursor index for tests.
func (m QueueDashboard) ListCursor() int {
	return m.list.Cursor()
}

// FilterActive reports whether the main-list filter is engaged.
func (m QueueDashboard) FilterActive() bool {
	return m.filterMode
}

// Init starts the poll and, alongside it, primes the live-pane affordance cache.
// The model is constructed with a snapshot but an empty cache, so without this
// the first paint renders every handoff key dark — telling the operator "this
// key spawns" about a set whose pane is already running, and only correcting
// itself a poll later. The priming is one tmux list query, not a snapshot
// rebuild, so it costs the open nothing measurable.
func (m QueueDashboard) Init() tea.Cmd {
	return tea.Batch(dashboardTick(), m.primeLiveCache())
}

// primeLiveCache loads the live-pane cache without rebuilding the snapshot,
// reusing the same message the poll delivers so there is one apply path.
func (m QueueDashboard) primeLiveCache() tea.Cmd {
	return func() tea.Msg {
		return dashboardLivePrimeMsg{live: loadLivePaneCache(m.d)}
	}
}

func (m QueueDashboard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Any keypress is a deliberate interaction, so it clears the sticky action
		// error. A keypress that triggers a fresh verb repopulates actionErr when
		// that verb's result message arrives.
		m.actionErr = nil
		if kpm, ok := msg.(tea.KeyPressMsg); ok {
			if ui.ToggleHelp(&m.showHelp, kpm) {
				return m, nil
			}
			if m.showHelp {
				return m, nil
			}
		}
		if m.bind != nil {
			m.pendingG = false
			return m.updateBindModal(msg)
		}
		if m.drainPick != nil {
			m.pendingG = false
			return m.updateDrainModal(msg)
		}
		if m.abandon != nil {
			m.pendingG = false
			return m.updateAbandonModal(msg)
		}
		if m.detail != nil {
			return m.updateDetailView(msg)
		}
		if m.menu != nil {
			m.pendingG = false
			return m.updateMenu(msg)
		}
		if m.filter != nil {
			m.pendingG = false
			return m.updateFilterMenu(msg)
		}
		if m.filterMode {
			m.pendingG = false
			return m.updateFilterMode(msg)
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
		case "/":
			m.filterMode = true
			m.filterInput = ui.NewTextField()
			return m, nil
		case "j", "down":
			m.list.MoveDown()
		case "k", "up":
			m.list.MoveUp()
		case "G":
			m.list.SetCursor(len(m.snap.Rows) - 1)
		case "ctrl+g":
			// Open the highlighted row's bound checkout in pop (task 02). A row
			// with a bound checkout surfaces its path on quit so the command
			// layer runs the workbench-aware open after the TUI exits; a row with
			// no checkout shows an inline status and keeps the dashboard running
			// (mirroring the shell action).
			row, ok := m.list.Selected()
			if !ok {
				return m, nil
			}
			if strings.TrimSpace(row.RuntimePath) == "" {
				m.statusMsg = "no checkout bound to this task set"
				return m, nil
			}
			m.statusMsg = ""
			m.openCheckout = row.RuntimePath
			return m, tea.Quit
		case "a", "A":
			row, ok := m.list.Selected()
			if !ok {
				return m, nil
			}
			if len(dashboardMenuItems(row)) == 0 {
				return m, nil
			}
			m.menu = newDashboardMenu(row, msg.String() == "A")
			m.err = nil
			m.statusMsg = ""
			return m, nil
		case "f":
			// Open the row-inclusion filter menu (ADR-0121). Unlike `/` (a transient
			// fuzzy query over the already-included rows) this modal flips which rows
			// are included at all; the two are independent concepts.
			m.filter = newDashboardFilterMenu()
			m.err = nil
			m.statusMsg = ""
			return m, nil
		case "I":
			row, ok := m.list.Selected()
			if !ok || !row.IsMap {
				return m, nil
			}
			m.err = nil
			return m, m.launchWayfinderSession(row, "")
		case "l", "enter":
			row, ok := m.list.Selected()
			if !ok {
				return m, nil
			}
			m.err = nil
			m.detail = newDetailView(row)
			return m, m.loadDetail(row)
		case "y":
			row, ok := m.list.Selected()
			if !ok {
				return m, nil
			}
			m.statusMsg = m.copyRowName(row)
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
			cmds = append(cmds, m.loadDetail(m.detail.row))
		}
		return m, tea.Batch(cmds...)
	case ui.SpinnerTickMsg:
		return m, nil
	case dashboardLivePrimeMsg:
		if m.live == nil {
			m.live = &livePaneCache{}
		}
		*m.live = msg.live
		return m, nil
	case dashboardRowsMsg:
		m.err = msg.err
		if msg.err == nil {
			m.allRows = msg.snap.Rows
			m.snap = msg.snap
			if m.live == nil {
				m.live = &livePaneCache{}
			}
			*m.live = msg.live
			if m.filterMode {
				m.snap.Rows = filterDashboardRows(m.allRows, m.filterInput.Value())
			}
			m.syncListRows()
			if m.detail != nil {
				for _, row := range m.snap.Rows {
					if row.CursorKey == m.detail.row.CursorKey {
						m.detail.row = row
						break
					}
				}
			}
			if m.menu != nil {
				if m.menu.pinned {
					if row, ok := m.list.Selected(); ok {
						m.menu.row = row
						m.menu.list = ui.NewList(dashboardMenuItems(row), ui.Opts[dashboardMenuItem]{Wrap: true})
					}
				} else {
					for _, row := range m.snap.Rows {
						if row.CursorKey == m.menu.row.CursorKey {
							m.menu.row = row
							break
						}
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
		if m.detail.row.IsMap {
			m.detail.syncMap(msg.wfMap)
		} else {
			m.detail.syncManifest(msg.manifest, msg.taskRow)
		}
		return m, nil
	case dashboardToggleMsg:
		if msg.err != nil {
			m.actionErr = msg.err
			return m, m.reload()
		}
		for i := range m.snap.Rows {
			if m.snap.Rows[i].CursorKey == msg.key {
				m.snap.Rows[i].AutoDrain = msg.autoDrain
				break
			}
		}
		m.cols.syncNatural(m.snap.Rows)
	case dashboardHandoffMsg:
		m.drainPick = nil
		if msg.err != nil {
			if errors.Is(msg.err, wayfinder.ErrEmptyFrontier) {
				m.statusMsg = dashboardWayfinderEmptyFrontierMessage()
				return m, nil
			}
			m.actionErr = msg.err
			return m, nil
		}
		if msg.quit {
			// Handoff focused the operator onto the spawned/reused pane; close
			// the dashboard rather than leaving it stranded behind that pane.
			return m, tea.Quit
		}
		if msg.status != "" {
			m.statusMsg = msg.status
		}
		return m, nil
	case dashboardUnparkMsg:
		if msg.err != nil {
			m.actionErr = msg.err
		} else {
			m.statusMsg = fmt.Sprintf("%s unparked", msg.setID)
		}
		return m, m.reload()
	case dashboardArchiveMsg:
		if msg.err != nil {
			m.actionErr = msg.err
		} else {
			m.statusMsg = fmt.Sprintf("%s archived", msg.setID)
		}
		return m, m.reload()
	case dashboardDrainListMsg:
		if msg.err != nil {
			m.actionErr = msg.err
			return m, nil
		}
		if len(msg.entries) == 0 {
			m.actionErr = fmt.Errorf("no drain target available for %s", msg.row.SetID)
			return m, nil
		}
		m.drainPick = newDashboardDrainModal(msg.row, msg.entries)
		return m, nil
	case dashboardStatusMsg:
		if msg.err != nil {
			m.actionErr = msg.err
		} else {
			m.statusMsg = fmt.Sprintf("%s: %s applied", msg.setID, msg.verb)
		}
		return m, m.reload()
	case dashboardBindListMsg:
		if msg.err != nil {
			m.actionErr = msg.err
			m.bind = nil
			return m, nil
		}
		m.bind = &dashboardBindModal{row: msg.row, list: newBindEntryList(msg.entries)}
	case dashboardBindRefsMsg:
		if m.bind == nil {
			return m, nil
		}
		if msg.err != nil {
			m.actionErr = msg.err
			m.bind = nil
			return m, nil
		}
		m.bind.stage = dashboardBindStageBaseRef
		m.bind.list = newBindEntryList(bindRefEntries(msg.refs))
		m.bind.loading = false
	case dashboardBindMsg:
		if msg.err != nil {
			m.actionErr = msg.err
			m.bind = nil
			return m, m.reload()
		}
		m.bind = nil
		return m, m.reload()
	case dashboardAbandonMsg:
		if msg.err != nil {
			m.actionErr = msg.err
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

func (m QueueDashboard) updateBindModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.bind = nil
		return m, nil
	case "j", "down":
		if m.bind.stage != dashboardBindStageName && m.bind.list != nil {
			m.bind.list.MoveDown()
		}
		return m, nil
	case "k", "up":
		if m.bind.stage != dashboardBindStageName && m.bind.list != nil {
			m.bind.list.MoveUp()
		}
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

func (m QueueDashboard) updateAbandonModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c", "n", "enter":
		m.abandon = nil
		return m, nil
	case "y":
		if m.abandon == nil || m.abandon.loading {
			return m, nil
		}
		m.abandon.loading = true
		return m, m.abandonWorktree(m.abandon.row)
	}
	return m, nil
}

// updateMenu drives the action overlay: esc/ctrl+c close it, j/k move the
// highlight, Enter runs the highlighted verb, and any matching verb letter runs
// that verb directly. Non-matching keys are inert while the menu is open. When
// the status submenu is open, esc returns to the action menu instead.
func (m QueueDashboard) updateMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.menu == nil {
		return m, nil
	}
	if m.menu.status != nil {
		return m.updateStatusMenu(msg)
	}
	switch msg.String() {
	case "esc", "ctrl+c":
		m.menu = nil
		return m, nil
	case "J":
		if m.menu.pinned {
			m.list.MoveDown()
			return m.syncPinnedMenuRow()
		}
	case "K":
		if m.menu.pinned {
			m.list.MoveUp()
			return m.syncPinnedMenuRow()
		}
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

// updateStatusMenu drives the nested status submenu: esc returns to the action
// menu, j/k move the highlight, Enter runs the highlighted verb, and any
// matching verb letter runs that verb directly.
func (m QueueDashboard) updateStatusMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.menu == nil || m.menu.status == nil {
		return m, nil
	}
	for i, item := range m.menu.status.list.Items() {
		if msg.String() == item.key {
			return m.invokeStatusMenuItem(i)
		}
	}
	switch msg.String() {
	case "esc", "ctrl+c":
		m.menu.status = nil
		return m, nil
	case "j", "down":
		m.menu.status.list.MoveDown()
		return m, nil
	case "k", "up":
		m.menu.status.list.MoveUp()
		return m, nil
	case "enter":
		return m.invokeStatusMenuItem(m.menu.status.list.Cursor())
	}
	return m, nil
}

// invokeMenuItem closes the menu and dispatches the verb at idx against the row
// the menu was opened on, except for the status submenu opener which nests.
func (m QueueDashboard) invokeMenuItem(idx int) (tea.Model, tea.Cmd) {
	if m.menu == nil {
		return m, nil
	}
	items := m.menu.list.Items()
	if idx < 0 || idx >= len(items) {
		return m, nil
	}
	item := items[idx]
	row := m.menu.row
	if item.action == menuActionStatusSubmenu {
		m.menu.status = newDashboardStatusMenu(row)
		return m, nil
	}
	pinned := m.menu.pinned && !dashboardMenuActionHandoff(item.action)
	if !pinned {
		m.menu = nil
	}
	return m.dispatchMenuAction(item.action, row)
}

// invokeStatusMenuItem closes both menus and applies the status verb at idx
// in-process (ADR-0158).
func (m QueueDashboard) invokeStatusMenuItem(idx int) (tea.Model, tea.Cmd) {
	if m.menu == nil || m.menu.status == nil {
		return m, nil
	}
	items := m.menu.status.list.Items()
	if idx < 0 || idx >= len(items) {
		return m, nil
	}
	item := items[idx]
	row := m.menu.row
	if m.menu.pinned {
		m.menu.status = nil
	} else {
		m.menu = nil
	}
	return m, m.launchStatusVerb(row, item.verb)
}

// updateFilterMenu drives the row-inclusion filter modal: esc/ctrl+c/f close it,
// j/k move the highlight, Enter/space toggles the highlighted filter, and any
// matching toggle letter flips that filter directly. The menu stays open across
// a toggle so the checkbox flip is visible and successive toggles are cheap;
// non-matching keys are inert while it is open (v is gated off by
// ViewToggleAllowed, so it lands here and is ignored).
func (m QueueDashboard) updateFilterMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.filter == nil {
		return m, nil
	}
	switch msg.String() {
	case "esc", "ctrl+c", "f":
		m.filter = nil
		return m, nil
	case "j", "down":
		m.filter.list.MoveDown()
		return m, nil
	case "k", "up":
		m.filter.list.MoveUp()
		return m, nil
	case "enter", "space":
		return m.invokeFilterItem(m.filter.list.Cursor())
	}
	for i, item := range m.filter.list.Items() {
		if msg.String() == item.key {
			return m.invokeFilterItem(i)
		}
	}
	return m, nil
}

// invokeFilterItem flips the inclusion filter at idx and rebuilds the row set.
// The menu stays open. Flipping mutates the session view flag on the model's
// Deps (m.d.IncludeDone) and returns a reload: BuildDashboard re-derives the
// rows honoring the new flag and re-sorts them (ADR-0121), and the reload's
// dashboardRowsMsg re-applies any active `/` fuzzy query, so the two filters
// stay independent. The flag is session-only — a fresh Deps on relaunch resets
// it to the launch seed (`--include-done`).
func (m QueueDashboard) invokeFilterItem(idx int) (tea.Model, tea.Cmd) {
	if m.filter == nil {
		return m, nil
	}
	items := m.filter.list.Items()
	if idx < 0 || idx >= len(items) {
		return m, nil
	}
	switch items[idx].toggle {
	case filterToggleShowDone:
		m.d.IncludeDone = !m.d.IncludeDone
		return m, m.reload()
	}
	return m, nil
}

// filterToggleOn reports the current on/off state of an inclusion filter, read
// from the live view flags so the menu checkbox tracks the actual view.
func (m QueueDashboard) filterToggleOn(toggle dashboardFilterToggle) bool {
	switch toggle {
	case filterToggleShowDone:
		return m.d != nil && m.d.IncludeDone
	}
	return false
}

// dispatchMenuAction runs the verb. The conditional guards mirror
// dashboardMenuItems' context filtering — an item present in the menu always
// passes its guard, but the guards keep dispatch self-contained.
func (m QueueDashboard) dispatchMenuAction(action dashboardMenuAction, row DashboardRow) (tea.Model, tea.Cmd) {
	m.err = nil
	switch action {
	case menuActionDrain:
		m.statusMsg = dashboardHandoffPending
		return m, m.launchDrain(row)
	case menuActionVerify:
		if !dashboardVerifyEligible(row) {
			return m, nil
		}
		m.statusMsg = dashboardHandoffPending
		return m, m.launchVerify(row)
	case menuActionBind:
		m.bind = &dashboardBindModal{row: row, loading: true}
		return m, m.loadBindWorktrees(row)
	case menuActionUnbind:
		if !row.Bound {
			return m, nil
		}
		m.abandon = &dashboardAbandonModal{row: row}
		return m, nil
	case menuActionAutoDrain:
		if row.Orphaned {
			return m, nil
		}
		for i := range m.snap.Rows {
			if m.snap.Rows[i].CursorKey == row.CursorKey {
				m.snap.Rows[i].AutoDrain = !m.snap.Rows[i].AutoDrain
				break
			}
		}
		m.cols.syncNatural(m.snap.Rows)
		return m, m.toggleAutoDrain(row)
	case menuActionAssist:
		m.statusMsg = dashboardHandoffPending
		return m, m.launchAssist(row)
	case menuActionFold:
		if !dashboardFoldEligible(row) {
			return m, nil
		}
		if err := PreflightFold(m.d, m.cfg, row.SetRef); err != nil {
			m.actionErr = err
			return m, nil
		}
		m.statusMsg = dashboardHandoffPending
		return m, m.launchFold(row)
	case menuActionUnpark:
		if !row.Parked {
			m.statusMsg = "task set is not parked"
			return m, nil
		}
		m.statusMsg = ""
		return m, m.unparkSet(row)
	case menuActionShell:
		if strings.TrimSpace(row.RuntimePath) == "" {
			m.statusMsg = "no checkout bound to this task set"
			return m, nil
		}
		m.statusMsg = dashboardHandoffPending
		return m, m.launchShell(row)
	case menuActionArchive:
		m.statusMsg = ""
		return m, m.archiveSet(row)
	case menuActionCopyName:
		m.statusMsg = m.copyRowName(row)
		return m, nil
	}
	return m, nil
}

func (m QueueDashboard) updateDetailView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.taskMenu != nil {
		m.pendingG = false
		return m.updateTaskMenu(msg)
	}
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
		case "a":
			if m.detail.row.IsMap {
				return m, nil
			}
			task, ok := m.detail.taskByID(m.detail.peek.taskID)
			if !ok {
				return m, nil
			}
			items := taskMenuItems(task)
			if len(items) == 0 {
				return m, nil
			}
			m.taskMenu = newTaskMenu(task, items, true)
		case "y":
			if m.detail.row.IsMap {
				ticket, ok := m.detail.ticketByDisplayName(m.detail.peek.taskID)
				if !ok {
					return m, nil
				}
				m.detail.peek.statusMsg = m.copyClipboard(ticketCopyNamePayload(ticket))
				return m, nil
			}
			task, ok := m.detail.taskByID(m.detail.peek.taskID)
			if !ok {
				return m, nil
			}
			m.detail.peek.statusMsg = m.copyClipboard(taskRefCopyPayload(m.detail.row.SetID, task))
			return m, nil
		}
		return m, nil
	}
	if msg.String() == "g" {
		if m.pendingG {
			m.pendingG = false
			if m.detail != nil {
				if m.detail.row.IsMap {
					m.detail.ticketList.SetCursor(0)
				} else {
					m.detail.list.SetCursor(0)
				}
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
	case "ctrl+g":
		// Open the detail's task set checkout in pop, mirroring the main-list
		// Ctrl-g: surface the bound checkout on quit so the command layer runs the
		// workbench-aware open after the TUI exits; an unbound set shows an inline
		// status and keeps the dashboard running. Map rows have no checkout.
		if m.detail == nil || m.detail.row.IsMap {
			return m, nil
		}
		if strings.TrimSpace(m.detail.row.RuntimePath) == "" {
			m.detail.statusMsg = "no checkout bound to this task set"
			return m, nil
		}
		m.detail.statusMsg = ""
		m.openCheckout = m.detail.row.RuntimePath
		return m, tea.Quit
	case "j", "down":
		if m.detail != nil {
			if m.detail.row.IsMap {
				m.detail.ticketList.MoveDown()
			} else {
				m.detail.list.MoveDown()
			}
		}
	case "k", "up":
		if m.detail != nil {
			if m.detail.row.IsMap {
				m.detail.ticketList.MoveUp()
			} else {
				m.detail.list.MoveUp()
			}
		}
	case "G":
		if m.detail != nil {
			if m.detail.row.IsMap && m.detail.wfMap != nil {
				m.detail.ticketList.SetCursor(len(m.detail.wfMap.Tickets) - 1)
			} else if m.detail.manifest != nil {
				m.detail.list.SetCursor(len(m.detail.manifest.Tasks) - 1)
			}
		}
	case "I":
		if m.detail == nil || m.detail.loading || !m.detail.row.IsMap || m.detail.wfMap == nil {
			return m, nil
		}
		ticket, ok := m.detail.ticketList.Selected()
		if !ok {
			return m, nil
		}
		if !detailTicketOnFrontier(*m.detail.wfMap, ticket) {
			m.detail.statusMsg = "only frontier tickets can be worked"
			return m, nil
		}
		m.detail.statusMsg = ""
		return m, m.launchWayfinderSession(m.detail.row, ticket.ID)
	case "l", "enter":
		if m.detail == nil || m.detail.loading {
			return m, nil
		}
		if m.detail.row.IsMap {
			ticket, ok := m.detail.ticketList.Selected()
			if !ok {
				return m, nil
			}
			if msg.String() == "enter" && detailTicketOnFrontier(*m.detail.wfMap, ticket) {
				m.detail.statusMsg = ""
				return m, m.launchWayfinderSession(m.detail.row, ticket.ID)
			}
			m.detail.peek = &taskTextPeek{taskID: detailTicketName(ticket), loading: true}
			return m, m.loadTicketText(m.detail.wfMap, ticket)
		}
		task, ok := m.detail.list.Selected()
		if !ok {
			return m, nil
		}
		m.detail.peek = &taskTextPeek{taskID: task.ID, loading: true}
		return m, m.loadTaskText(m.detail.manifest, task)
	case "a":
		if m.detail == nil || m.detail.loading || m.detail.row.IsMap {
			return m, nil
		}
		task, ok := m.detail.list.Selected()
		if !ok {
			return m, nil
		}
		items := taskMenuItems(task)
		if len(items) == 0 {
			return m, nil
		}
		m.detail.statusMsg = ""
		m.taskMenu = newTaskMenu(task, items, false)
		return m, nil
	case "y":
		if m.detail == nil || m.detail.loading {
			return m, nil
		}
		if m.detail.row.IsMap {
			ticket, ok := m.detail.ticketList.Selected()
			if !ok {
				return m, nil
			}
			m.detail.statusMsg = m.copyClipboard(ticketCopyNamePayload(ticket))
			return m, nil
		}
		task, ok := m.detail.list.Selected()
		if !ok {
			return m, nil
		}
		m.detail.statusMsg = m.copyClipboard(taskRefCopyPayload(m.detail.row.SetID, task))
		return m, nil
	}
	return m, nil
}

// updateTaskMenu drives the task-level action overlay: esc/ctrl+c close it, j/k
// move the highlight, Enter runs the highlighted verb, and any matching verb
// letter runs that verb directly. Non-matching keys are inert while open.
func (m QueueDashboard) updateTaskMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.taskMenu == nil {
		return m, nil
	}
	for i, item := range m.taskMenu.list.Items() {
		if msg.String() == item.key {
			return m.invokeTaskMenuItem(i)
		}
	}
	switch msg.String() {
	case "esc", "ctrl+c":
		m.taskMenu = nil
		return m, nil
	case "j", "down":
		m.taskMenu.list.MoveDown()
		return m, nil
	case "k", "up":
		m.taskMenu.list.MoveUp()
		return m, nil
	case "enter":
		return m.invokeTaskMenuItem(m.taskMenu.list.Cursor())
	}
	return m, nil
}

// invokeTaskMenuItem closes the menu and dispatches the verb at idx against the
// task the menu was opened on. The items are pre-filtered to valid transitions
// (taskMenuItems), so the verb applies without a separate confirmation.
func (m QueueDashboard) invokeTaskMenuItem(idx int) (tea.Model, tea.Cmd) {
	if m.taskMenu == nil {
		return m, nil
	}
	items := m.taskMenu.list.Items()
	if idx < 0 || idx >= len(items) {
		return m, nil
	}
	if m.detail == nil {
		m.taskMenu = nil
		return m, nil
	}
	item := items[idx]
	task := m.taskMenu.task
	inPeek := m.taskMenu.inPeek
	m.taskMenu = nil
	if item.key == "y" {
		msg := m.copyClipboard(taskRefCopyPayload(m.detail.row.SetID, task))
		if inPeek {
			m.detail.peek.statusMsg = msg
		} else {
			m.detail.statusMsg = msg
		}
		return m, nil
	}
	m.detail.statusMsg = ""
	return m, m.applyDetailOverride(m.detail.row, task, item.key)
}

// applyDetailOverride dispatches the c/o/k override verb to the appropriate
// tasks.*With function via the Deps seam.
func (m QueueDashboard) applyDetailOverride(row DashboardRow, task tasks.Task, verb string) tea.Cmd {
	d := m.d
	if d == nil {
		d = DefaultDeps()
	}
	taskPath := row.SetID + "/" + task.File
	return func() tea.Msg {
		var err error
		switch verb {
		case "c":
			err = d.completeDetailTask(row.DefPath, taskPath)
		case "o":
			err = d.resetDetailTask(row.DefPath, taskPath)
		case "k":
			err = d.skipDetailTask(row.DefPath, taskPath)
		}
		verbName := map[string]string{"c": "complete", "o": "open", "k": "skip"}[verb]
		return dashboardDetailOverrideMsg{taskID: task.ID, verb: verbName, err: err}
	}
}

func (m QueueDashboard) updateFilterMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.filterMode = false
		m.filterInput = ui.TextField{}
		m.snap.Rows = m.allRows
		m.syncListRows()
		return m, nil
	case "j", "down":
		m.list.MoveDown()
		return m, nil
	case "k", "up":
		m.list.MoveUp()
		return m, nil
	default:
		m.filterInput.Update(msg)
		m.snap.Rows = filterDashboardRows(m.allRows, m.filterInput.Value())
		m.syncListRows()
		return m, nil
	}
}

func (m QueueDashboard) moveTaskTextPeek(delta int) {
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

func (m QueueDashboard) maxTaskTextPeekScroll() int {
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

func (m QueueDashboard) taskTextPeekPageSize() int {
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

func (m QueueDashboard) confirmBindModal() (tea.Model, tea.Cmd) {
	if m.bind == nil || m.bind.loading {
		return m, nil
	}
	switch m.bind.stage {
	case dashboardBindStageWorktree:
		entry, ok := m.bind.list.Selected()
		if !ok {
			return m, nil
		}
		if entry.Managed {
			m.bind.loading = true
			return m, m.bindManagedWorktree(m.bind.row)
		}
		if entry.Create {
			m.bind.loading = true
			return m, m.loadBindRefs(m.bind.row)
		}
		m.bind.loading = true
		return m, m.adoptBindWorktree(m.bind.row, entry.Path)
	case dashboardBindStageBaseRef:
		entry, ok := m.bind.list.Selected()
		if !ok {
			return m, nil
		}
		m.bind.baseRef = entry.Label
		m.bind.stage = dashboardBindStageName
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

func (m QueueDashboard) reload() tea.Cmd {
	return func() tea.Msg {
		snap, err := work.BuildSnapshot(m.d.WorkDeps(), m.cfg)
		live := loadLivePaneCache(m.d)
		return dashboardRowsMsg{snap: snap, live: live, err: err}
	}
}

// unparkSet handles the `P` key: it writes a durable park-clear event for the
// selected parked set so the daemon may auto-spawn it again (ADR-0055).
func (m QueueDashboard) unparkSet(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		err := UnparkSet(m.d, row.SetRef)
		return dashboardUnparkMsg{setID: row.SetID, err: err}
	}
}

// archiveSet sets the reversible archived flag on the cursored set through the
// existing archive flag-write path. It touches only Task state, leaving the
// set's Worktree binding intact; the archived row drops out on the next build,
// which excludes Archived sets. Archiving is fully reversible, so no
// confirmation is required (ADR cleanup path for Done and Orphaned sets alike).
func (m QueueDashboard) archiveSet(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		err := m.d.archiveSet(row.DefPath, row.SetID)
		return dashboardArchiveMsg{setID: row.SetID, err: err}
	}
}

func (m QueueDashboard) toggleAutoDrain(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		result, err := m.d.toggleAutoDrain(row.DefPath, row.StatePath, row.SetID)
		if err != nil {
			return dashboardToggleMsg{key: row.CursorKey, err: err}
		}
		return dashboardToggleMsg{key: row.CursorKey, autoDrain: result.AutoDrain}
	}
}

// launchDrain handles the `i` key. A set that already holds a Worktree binding
// resumes in it immediately (no picker). An unbound set opens the Drain target
// picker so the operator chooses where the drain lands (ADR-0052). Bound drains
// and confirmed picker drains both finish through the shared handoff path.
func (m QueueDashboard) launchDrain(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		bound, err := dashboardSetBound(m.d, m.cfg, row.SetRef)
		if err != nil {
			return dashboardDrainListMsg{row: row, err: err}
		}
		if bound {
			result, err := LaunchDrain(m.d, m.cfg, row.SetRef)
			return handoffAfterLaunch(m.d, result, err)
		}
		entries, err := DrainTargetEntries(m.d, m.cfg, row.SetRef)
		return dashboardDrainListMsg{row: row, entries: entries, err: err}
	}
}

func (m QueueDashboard) updateDrainModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.drainPick = nil
		return m, nil
	case "j", "down":
		if m.drainPick.list != nil {
			m.drainPick.list.MoveDown()
		}
		return m, nil
	case "k", "up":
		if m.drainPick.list != nil {
			m.drainPick.list.MoveUp()
		}
		return m, nil
	case "enter":
		return m.confirmDrainModal()
	}
	return m, nil
}

func (m QueueDashboard) confirmDrainModal() (tea.Model, tea.Cmd) {
	if m.drainPick == nil || m.drainPick.loading {
		return m, nil
	}
	entry, ok := m.drainPick.list.Selected()
	if !ok {
		return m, nil
	}
	row := m.drainPick.row
	m.drainPick.loading = true
	return m, m.launchDrainTarget(row, entry)
}

// launchDrainTarget binds the chosen target (adopt, provision, or leave unbound
// for trunk) and drains in one action, then hands off through the shared path.
func (m QueueDashboard) launchDrainTarget(row DashboardRow, target dashboardDrainEntry) tea.Cmd {
	return func() tea.Msg {
		result, err := LaunchDrainTarget(m.d, m.cfg, row.SetRef, target)
		return handoffAfterLaunch(m.d, result, err)
	}
}

// defaultDrainCursor positions the picker on "new managed worktree" — the
// frictionless default that provisions an isolated checkout. It falls back to the
// first entry when no trunk is resolvable (the option is absent).
func defaultDrainCursor(entries []dashboardDrainEntry) int {
	for i, e := range entries {
		if e.Kind == drainTargetNewManaged {
			return i
		}
	}
	return 0
}

// launchVerify spawns a Verifier pane on the focused set (ADR-0123) and hands
// off through the shared path. It records no lock, spawn intent, or DrainPane —
// verify is not a drain — so the verdict surfaces through the next poll's
// ApplyVerifyVerdicts re-derivation when the dashboard stays open.
func (m QueueDashboard) launchVerify(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		result, err := LaunchVerify(m.d, m.cfg, row.SetRef)
		return handoffAfterLaunch(m.d, result, err)
	}
}

func (m QueueDashboard) launchWayfinderSession(row DashboardRow, ticketID string) tea.Cmd {
	return func() tea.Msg {
		result, err := LaunchWayfinderSession(m.d, m.cfg, row, ticketID)
		return handoffAfterLaunch(m.d, result, err)
	}
}

func dashboardWayfinderEmptyFrontierMessage() string {
	return "no frontier tickets — open tickets are blocked or claimed"
}

func detailTicketOnFrontier(wfMap wayfinder.Map, ticket wayfinder.Ticket) bool {
	for _, t := range wayfinder.Frontier(wfMap.Tickets) {
		if t.ID == ticket.ID {
			return true
		}
	}
	return false
}

func (m QueueDashboard) launchAssist(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		result, err := LaunchAssist(m.d, m.cfg, row.SetRef)
		return handoffAfterLaunch(m.d, result, err)
	}
}

func (m QueueDashboard) launchFold(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		result, err := LaunchFold(m.d, m.cfg, row.SetRef)
		return handoffAfterLaunch(m.d, result, err)
	}
}

func (m QueueDashboard) launchShell(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		result, err := LaunchShell(m.d, m.cfg, row.SetRef)
		return handoffAfterLaunch(m.d, result, err)
	}
}

// handoffAfterLaunch is the single post-spawn path for drain, verify, fold,
// assist, shell, and wayfinder (ADR-0158): focus the pane when inside tmux and
// signal quit, or stay open with a status line explaining why focus was
// unavailable / nothing moved.
func handoffAfterLaunch(d *Deps, result DashboardDrainResult, err error) dashboardHandoffMsg {
	if err != nil {
		return dashboardHandoffMsg{err: err}
	}
	if strings.TrimSpace(result.PaneID) == "" {
		return dashboardHandoffMsg{status: "nothing to hand off to"}
	}
	if d == nil {
		d = DefaultDeps()
	}
	if d.Tmux == nil {
		d.Tmux = tmuxmod.New()
	}
	session := strings.TrimSpace(result.Session)
	if session == "" {
		if s, serr := d.Tmux.PaneSession(result.PaneID); serr == nil {
			session = s
		}
	}
	if !d.Tmux.InTmux() {
		status := "not inside tmux"
		if session != "" {
			status = fmt.Sprintf("pane opened in session %s (not inside tmux)", session)
		}
		return dashboardHandoffMsg{status: status}
	}
	if ferr := tmuxmod.FocusPane(d.Tmux, result.PaneID); ferr != nil {
		return dashboardHandoffMsg{err: ferr}
	}
	return dashboardHandoffMsg{quit: true}
}

func (m QueueDashboard) launchStatusVerb(row DashboardRow, verb string) tea.Cmd {
	return func() tea.Msg {
		err := applyDashboardStatusVerb(m.d, row, verb)
		return dashboardStatusMsg{setID: row.SetID, verb: verb, err: err}
	}
}

func (m QueueDashboard) loadDetail(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		d := m.d
		if d == nil {
			d = DefaultDeps()
		}
		if row.IsMap {
			return loadMapDetailMsg(d, row)
		}
		if d.Tasks == nil {
			d.Tasks = tasks.DefaultDeps()
		}
		refresh, err := d.refresh(row.DefPath)
		if err != nil {
			return dashboardDetailMsg{dashRow: row, err: err}
		}
		// The detail overlay needs only the per-set binding for the verify-verdict
		// runtime resolution; read the bindings directly (the same if-exists borrow
		// the snapshot build takes) rather than reopening the whole snapshot.
		bindings, _ := binding.AllBindings(d.Tasks)
		var cfg *config.Config
		if d.LoadConfig != nil {
			cfg, _ = d.LoadConfig(config.DefaultConfigPath())
		}
		tasks.ApplyVerifyVerdictsWith(d.Tasks, refresh, cfg, func(setID string) string {
			return binding.RuntimeForSet(bindings, row.RepoKey, setID)
		})
		taskRow := tasks.FindRow(refresh, row.SetID)
		manifest := refresh.Manifests[row.SetID]
		return dashboardDetailMsg{dashRow: row, manifest: manifest, taskRow: taskRow}
	}
}

func loadMapDetailMsg(d *Deps, row DashboardRow) dashboardDetailMsg {
	if d.Tasks == nil {
		d.Tasks = tasks.DefaultDeps()
	}
	storageDir := dashboardRowStorageDir(row)
	if storageDir == "" {
		return dashboardDetailMsg{dashRow: row, err: fmt.Errorf("no storage dir for map %q", row.SetID)}
	}
	wd := &wayfinder.Deps{FS: d.Tasks.FS, Tasks: d.Tasks}
	maps, err := wayfinder.ScanMapsInStorage(wd, storageDir)
	if err != nil {
		return dashboardDetailMsg{dashRow: row, err: err}
	}
	for i := range maps {
		if maps[i].ID == row.SetID {
			cp := maps[i]
			return dashboardDetailMsg{dashRow: row, wfMap: &cp}
		}
	}
	return dashboardDetailMsg{dashRow: row, err: fmt.Errorf("map %q not found", row.SetID)}
}

func dashboardRowStorageDir(row DashboardRow) string {
	if row.DefPath != "" {
		return filepath.Dir(row.DefPath)
	}
	return ""
}

func (m QueueDashboard) loadTaskText(manifest *tasks.Manifest, task tasks.Task) tea.Cmd {
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

func (m QueueDashboard) loadTicketText(wfMap *wayfinder.Map, ticket wayfinder.Ticket) tea.Cmd {
	label := detailTicketName(ticket)
	return func() tea.Msg {
		if wfMap == nil {
			return dashboardTaskTextMsg{taskID: label, err: fmt.Errorf("map not loaded")}
		}
		d := m.d
		if d == nil {
			d = DefaultDeps()
		}
		if d.Tasks == nil {
			d.Tasks = tasks.DefaultDeps()
		}
		path := filepath.Join(wfMap.Dir, "issues", detailTicketFilename(ticket))
		data, err := d.Tasks.FS.ReadFile(path)
		if err != nil {
			return dashboardTaskTextMsg{taskID: label, path: path, err: err}
		}
		return dashboardTaskTextMsg{taskID: label, path: path, text: string(data)}
	}
}

func (m QueueDashboard) loadBindWorktrees(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		entries, err := BindWorktreeEntries(m.d, m.cfg, row.SetRef)
		return dashboardBindListMsg{row: row, entries: entries, err: err}
	}
}

func (m QueueDashboard) loadBindRefs(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		refs, err := BindBaseRefs(m.d, m.cfg, row.SetRef)
		return dashboardBindRefsMsg{refs: refs, err: err}
	}
}

func (m QueueDashboard) adoptBindWorktree(row DashboardRow, checkoutPath string) tea.Cmd {
	return func() tea.Msg {
		_, err := AdoptWorktree(m.d, m.cfg, row.SetRef, checkoutPath)
		return dashboardBindMsg{err: err}
	}
}

func (m QueueDashboard) bindManagedWorktree(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		_, err := BindManagedWorktree(m.d, m.cfg, row.SetRef)
		return dashboardBindMsg{err: err}
	}
}

func (m QueueDashboard) createBindWorktree(row DashboardRow, baseRef, name string) tea.Cmd {
	return func() tea.Msg {
		_, err := CreateWorktree(m.d, m.cfg, row.SetRef, baseRef, name)
		return dashboardBindMsg{err: err}
	}
}

func (m QueueDashboard) abandonWorktree(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		_, err := UnbindWorktree(m.d, m.cfg, row.SetRef)
		return dashboardAbandonMsg{err: err}
	}
}

// dashboardSetBound reports whether the row's set already holds a Worktree
// binding. The Drain target picker only opens for unbound sets; a bound set
// resumes in its binding (ADR-0052).
func dashboardSetBound(d *Deps, cfg *config.Config, ref SetRef) (bool, error) {
	d = ensureQueueDeps(d)
	repoKey := ref.RepoKey
	if repoKey == "" {
		_, rk, err := dashboardBindContext(d, cfg, ref)
		if err != nil {
			return false, err
		}
		repoKey = rk
	}
	b, ok := bindingForSet(d.Tasks, repoKey, ref.SetID)
	return ok && strings.TrimSpace(b.RuntimePath) != "", nil
}

// boundCheckoutPaths returns the canonicalized set of every checkout currently
// bound to a set, across all repos. The Drain target picker excludes these from
// its adopt list so a checkout never binds to two sets at once.
func boundCheckoutPaths(d *Deps) (map[string]bool, error) {
	bindings, err := binding.AllBindings(d.Tasks)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, b := range bindings {
		path := strings.TrimSpace(b.RuntimePath)
		if path == "" {
			continue
		}
		out[bestEffortCanon(d, path)] = true
	}
	return out, nil
}

// bestEffortCanon canonicalizes path for reliable comparison, falling back to a
// cleaned absolute path when the target does not exist (so EvalSymlinks fails).
func bestEffortCanon(d *Deps, path string) string {
	if c, err := canonicalCheckoutPath(d.Tasks, path); err == nil {
		return c
	}
	if abs, err := filepath.Abs(path); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(path)
}

// pathUnder reports whether path is root or lives beneath it. Both arguments are
// expected to be canonicalized.
func pathUnder(path, root string) bool {
	if root == "" {
		return false
	}
	if path == root {
		return true
	}
	return strings.HasPrefix(path, root+string(filepath.Separator))
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

func (m QueueDashboard) helpEntries() []ui.HelpEntry {
	// Determine current mode and return contextual help entries
	switch {
	case m.bind != nil:
		// Bind modal - contextual based on stage
		switch m.bind.stage {
		case dashboardBindStageWorktree:
			return []ui.HelpEntry{
				{Key: "j/k", Desc: "navigate worktrees"},
				{Key: "enter", Desc: "select"},
				{Key: "esc", Desc: "cancel"},
			}
		case dashboardBindStageBaseRef:
			return []ui.HelpEntry{
				{Key: "j/k", Desc: "navigate refs"},
				{Key: "enter", Desc: "select base ref"},
				{Key: "esc", Desc: "cancel"},
			}
		case dashboardBindStageName:
			return []ui.HelpEntry{
				{Key: "typing", Desc: "enter worktree name"},
				{Key: "backspace", Desc: "delete character"},
				{Key: "enter", Desc: "create worktree"},
				{Key: "esc", Desc: "cancel"},
			}
		}
	case m.drainPick != nil:
		// Drain picker modal
		return []ui.HelpEntry{
			{Key: "j/k", Desc: "navigate targets"},
			{Key: "enter", Desc: "drain to selected"},
			{Key: "esc", Desc: "cancel"},
		}
	case m.abandon != nil:
		// Abandon/unbind confirmation modal
		return []ui.HelpEntry{
			{Key: "y", Desc: "confirm unbind"},
			{Key: "enter/n/esc", Desc: "cancel"},
		}
	case m.taskMenu != nil:
		// Task-level action menu (in detail or peek)
		return []ui.HelpEntry{
			{Key: "c", Desc: "complete task"},
			{Key: "o", Desc: "open/reopen task"},
			{Key: "k", Desc: "skip task"},
			{Key: "y", Desc: "copy name"},
			{Key: "j/k", Desc: "navigate"},
			{Key: "enter", Desc: "run action"},
			{Key: "esc", Desc: "close menu"},
		}
	case m.menu != nil && m.menu.status != nil:
		return []ui.HelpEntry{
			{Key: "c", Desc: "complete"},
			{Key: "o", Desc: "open (reopen)"},
			{Key: "k", Desc: "skip"},
			{Key: "x", Desc: "archive"},
			{Key: "u", Desc: "unarchive"},
			{Key: "j/k", Desc: "navigate"},
			{Key: "enter", Desc: "run action"},
			{Key: "esc", Desc: "back to action menu"},
		}
	case m.menu != nil:
		// Dashboard action menu
		entries := []ui.HelpEntry{
			{Key: "I", Desc: "drain"},
			{Key: "V", Desc: "verify"},
			{Key: "b", Desc: "bind worktree"},
			{Key: "u", Desc: "unbind worktree"},
			{Key: "a", Desc: "toggle auto-drain"},
			{Key: "s", Desc: "status submenu"},
			{Key: "S", Desc: "assist"},
			{Key: "F", Desc: "fold"},
			{Key: "r", Desc: "unpark"},
			{Key: "O", Desc: "shell"},
			{Key: "x", Desc: "archive"},
			{Key: "y", Desc: "copy name"},
			{Key: "j/k", Desc: "navigate"},
			{Key: "enter", Desc: "run action"},
			{Key: "esc", Desc: "close menu"},
		}
		if m.menu.pinned {
			entries = append(entries, ui.HelpEntry{Key: "J/K", Desc: "move row cursor"})
		}
		return entries
	case m.filter != nil:
		// Row-inclusion filter menu
		return []ui.HelpEntry{
			{Key: "d", Desc: "toggle show done"},
			{Key: "j/k", Desc: "navigate"},
			{Key: "enter/space", Desc: "toggle filter"},
			{Key: "esc", Desc: "close menu"},
		}
	case m.detail != nil && m.detail.peek != nil:
		// Detail peek view (task set or Map ticket)
		entries := []ui.HelpEntry{
			{Key: "j/k", Desc: "scroll line"},
			{Key: "ctrl+d", Desc: "page down"},
			{Key: "ctrl+u", Desc: "page up"},
			{Key: "gg", Desc: "top"},
			{Key: "G", Desc: "bottom"},
			{Key: "y", Desc: "copy name"},
			{Key: "h/esc", Desc: "close peek"},
		}
		if m.detail != nil && !m.detail.row.IsMap {
			entries = append(entries, ui.HelpEntry{Key: "a", Desc: "task actions"})
		}
		return entries
	case m.detail != nil && m.detail.row.IsMap:
		// Map detail view (ticket list)
		return []ui.HelpEntry{
			{Key: "j/k", Desc: "navigate tickets"},
			{Key: "gg", Desc: "first ticket"},
			{Key: "G", Desc: "last ticket"},
			{Key: "I/enter", Desc: "work frontier ticket"},
			{Key: "l", Desc: "peek ticket text"},
			{Key: "y", Desc: "copy name"},
			{Key: "h/esc", Desc: "back to list"},
		}
	case m.detail != nil:
		// Detail view (task list)
		return []ui.HelpEntry{
			{Key: "j/k", Desc: "navigate tasks"},
			{Key: "gg", Desc: "first task"},
			{Key: "G", Desc: "last task"},
			{Key: "l/enter", Desc: "peek task text"},
			{Key: "a", Desc: "task actions"},
			{Key: "y", Desc: "copy name"},
			{Key: "ctrl+g", Desc: "open worktree"},
			{Key: "h/esc", Desc: "back to list"},
		}
	case m.filterMode:
		// Filter mode
		return []ui.HelpEntry{
			{Key: "typing", Desc: "filter rows"},
			{Key: "j/k", Desc: "navigate filtered"},
			{Key: "v", Desc: "routines view"},
			{Key: "esc", Desc: "clear filter"},
		}
	default:
		// Main list view
		entries := []ui.HelpEntry{
			{Key: "j/k", Desc: "navigate"},
			{Key: "gg", Desc: "first row"},
			{Key: "G", Desc: "last row"},
			{Key: "l/enter", Desc: "open detail"},
			{Key: "y", Desc: "copy name"},
			{Key: "a", Desc: "action menu"},
			{Key: "A", Desc: "pinned action menu"},
			{Key: "ctrl+g", Desc: "open worktree"},
			{Key: "/", Desc: "filter"},
			{Key: "f", Desc: "filter menu"},
			{Key: "v", Desc: "routines view"},
			{Key: "h/esc", Desc: "quit"},
		}
		if row, ok := m.list.Selected(); ok && row.IsMap {
			entries = append(entries, ui.HelpEntry{Key: "I", Desc: "work next frontier ticket"})
		}
		return entries
	}
	return nil
}

func (m QueueDashboard) View() tea.View {
	if m.showHelp {
		title := "Help · Queue"
		if m.filterMode {
			title = "Help · Queue · filter"
		} else if m.detail != nil && m.detail.peek != nil {
			title = "Help · Queue · peek"
		} else if m.detail != nil {
			title = "Help · Queue · detail"
		} else if m.menu != nil && m.menu.status != nil {
			title = "Help · Queue · status submenu"
		} else if m.menu != nil && m.menu.pinned {
			title = "Help · Queue · pinned action menu"
		} else if m.menu != nil {
			title = "Help · Queue · action menu"
		} else if m.filter != nil {
			title = "Help · Queue · filter menu"
		} else if m.taskMenu != nil {
			title = "Help · Queue · task menu"
		} else if m.bind != nil {
			title = "Help · Queue · bind"
		} else if m.drainPick != nil {
			title = "Queue · drain"
		} else if m.abandon != nil {
			title = "Queue · unbind"
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

	var content string
	switch {
	case m.menu != nil:
		content = m.viewWithMenu()
	case m.filter != nil:
		content = m.viewWithFilterMenu()
	case m.bind != nil || m.drainPick != nil || m.abandon != nil:
		content = m.viewWithModal()
	default:
		content = m.frameSpec().Render(m.mainBody())
	}
	v := tea.NewView(content)
	v.AltScreen = true
	return v
}

// frameSpec builds the Frame describing the main task-set view's chrome: the
// Queue · N summary (Header), the filter input (InputBox), the refresh error
// (Warnings), the transient statusMsg (Status), and the footer hint (Hints). The
// same Frame drives both the body-height budget and the render, so the reserved
// line count can never drift from what is drawn (ADR-0079).
// dashboardActionErrorLine formats a sticky row-verb error for display. It keeps
// the full message (no column-math truncation): the ⚠ warning region and the
// menu/modal bodies render it un-clipped, so a long refusal wraps in the
// terminal rather than being cut into meaninglessness — the informative head is
// always visible.
func dashboardActionErrorLine(err error) string {
	return fmt.Sprintf("action failed: %v", err)
}

func (m QueueDashboard) frameSpec() ui.Frame {
	var warnings []string
	if m.err != nil {
		warnings = append(warnings, fmt.Sprintf("refresh error: %v", m.err))
	}
	if m.actionErr != nil {
		warnings = append(warnings, dashboardActionErrorLine(m.actionErr))
	}
	header := ""
	if len(m.snap.Rows) > 0 {
		header = "Queue · " + dashboardSummary(m.snap.Rows)
	}
	inputBox := ""
	if m.filterMode {
		inputBox = m.filterInput.View()
	}
	return ui.Frame{
		Width:    m.width,
		TermH:    m.height,
		Header:   header,
		InputBox: inputBox,
		Warnings: warnings,
		Status:   m.statusMsg,
		Footnote: m.modelSkipFootnote(),
		Hints:    m.mainHint(),
	}
}

// modelSkipFootnote renders the standing Effort model skip line (ADR-0168) —
// `skipped: cursor/claude-opus-5-thinking-high 47m · kimi/k2.7-code-highspeed ∞`
// — clipped to the terminal width. It is empty when nothing is skipped, which is
// the steady state, and also in a pane shorter than the two-line-mode floor: at
// that height ADR-0107 already trades completeness for visible-row density, and
// the diagnostic is available from `pop tasks agents`.
func (m QueueDashboard) modelSkipFootnote() string {
	if len(m.snap.ModelSkips) == 0 || m.height < dashboardTwoLineHeightFloor {
		return ""
	}
	// The Frame indents the footnote region two columns, as it does the status
	// line, so the budget for the text itself is that much narrower.
	return ui.TruncateString(formatModelSkipFootnote(m.snap.ModelSkips, time.Now()), m.width-2)
}

// formatModelSkipFootnote groups the snapshot's skips by preset — each preset
// named once, its skipped models comma-separated after the slash — and joins the
// groups with the dashboard's · separator. The snapshot orders skips by preset
// then model, so adjacent runs are the groups.
func formatModelSkipFootnote(skips []work.ModelSkip, now time.Time) string {
	var groups []string
	for i := 0; i < len(skips); {
		preset := skips[i].Preset
		var entries []string
		for ; i < len(skips) && skips[i].Preset == preset; i++ {
			entries = append(entries, skips[i].Model+" "+tasks.FormatModelSkipRemaining(skips[i].Until, now))
		}
		groups = append(groups, preset+"/"+strings.Join(entries, ", "))
	}
	return "skipped: " + strings.Join(groups, " · ")
}

// mainHint returns the footer hint for the main (non-modal, non-menu) view.
func (m QueueDashboard) mainHint() string {
	if len(m.snap.Rows) == 0 {
		if m.filterMode {
			return "esc clear filter · v routines · C-h help"
		}
		return "v routines · C-h help · h/esc quit"
	}
	if m.filterMode {
		return "esc clear filter · j/k navigate · v routines · C-h help"
	}
	return "j/k move · gg/G top/bottom · l/enter status · y copy name · a actions · / filter · f filters · v routines · C-h help · h/esc quit"
}

// mainBody renders the table body (a blank line, the column header, the
// separator, then the List's scroll window) or the empty-state message. It is
// the body the Frame composes its chrome around.
func (m QueueDashboard) mainBody() string {
	if len(m.snap.Rows) == 0 {
		if m.filterMode {
			return "No matching task sets."
		}
		return "No queue-actionable task sets."
	}
	var parts []string
	if dashboardTwoLineMode(m.snap.Rows, m.width, m.height) {
		line1Widths := dashboardTwoLineFitWidths(dashboardTwoLineNaturalWidths(m.snap.Rows), dashboardTableBodyBudget(m.width))
		parts = []string{
			"",
			ui.TruncateString("  "+dashboardTwoLineTableHeader(line1Widths), m.width),
			ui.TruncateString("  "+dashboardTwoLineStatusHeader(line1Widths), m.width),
			ui.TruncateString("  "+dashboardTwoLineTableSeparator(line1Widths), m.width),
		}
	} else {
		parts = []string{
			"",
			ui.TruncateString("  "+dashboardTableLine(dashboardTableHeaders(), m.cols.widths), m.width),
			ui.TruncateString("  "+dashboardTableSeparator(m.cols.widths), m.width),
		}
	}
	parts = append(parts, m.list.VisibleRows()...)
	return strings.Join(parts, "\n")
}

// viewWithMenu renders the action-menu overlay: the summary, the full table with
// the menu spliced next to the cursored row (bespoke overlay placement, ADR-0079),
// and the menu footer. The menu overlay's own body is ported onto List in a later
// slice; here it keeps the current rendering with the cursor read from the List.
func (m QueueDashboard) viewWithMenu() string {
	var body strings.Builder
	if m.err != nil {
		fmt.Fprintf(&body, "refresh error: %v\n", m.err)
	}
	if m.actionErr != nil {
		fmt.Fprintf(&body, "%s\n", dashboardActionErrorLine(m.actionErr))
	}
	fmt.Fprintf(&body, "Queue · %s\n", dashboardSummary(m.snap.Rows))
	fmt.Fprintln(&body)
	renderDashboardTableWithMenu(&body, m.snap.Rows, m.list.Cursor(), m.width, m.height, m.menu, m.liveCache())
	if m.statusMsg != "" {
		fmt.Fprintf(&body, "  %s\n", m.statusMsg)
	}
	hint := "j/k move · enter/letter run · esc close"
	if m.menu.pinned {
		hint = "j/k move · J/K row · enter/letter run · esc close"
	}
	if m.menu.status != nil {
		hint = "j/k move · enter/letter run · esc back"
		if m.menu.pinned {
			hint = "j/k move · J/K row · enter/letter run · esc back"
		}
	}
	writeDashboardFooter(&body, m.height, ui.HintStyle.Render(hint))
	return body.String()
}

// viewWithFilterMenu renders the row-inclusion filter modal: the summary, the
// full table, and the filter toggles below it, replacing the footer. It mirrors
// viewWithMenu's chrome — a sibling modal — but the menu is not row-anchored, so
// it sits below the table rather than splicing next to the cursor.
func (m QueueDashboard) viewWithFilterMenu() string {
	var body strings.Builder
	if m.err != nil {
		fmt.Fprintf(&body, "refresh error: %v\n", m.err)
	}
	if m.actionErr != nil {
		fmt.Fprintf(&body, "%s\n", dashboardActionErrorLine(m.actionErr))
	}
	fmt.Fprintf(&body, "Queue · %s\n", dashboardSummary(m.snap.Rows))
	fmt.Fprintln(&body)
	renderDashboardTable(&body, m.snap.Rows, m.list.Cursor(), m.width, m.height, m.liveCache())
	for _, ml := range m.dashboardFilterMenuLines() {
		fmt.Fprintf(&body, "%s\n", ml)
	}
	writeDashboardFooter(&body, m.height, ui.HintStyle.Render("j/k move · enter/space toggle · esc close"))
	return body.String()
}

// dashboardFilterMenuLines renders the filter overlay as a block of lines: a
// dimmed "filters" caption, then one checkbox line per toggle with the
// highlighted item carrying the shared cursor block. The checkbox state is read
// live from the model's view flags (filterToggleOn), so it always reflects the
// current view.
func (m QueueDashboard) dashboardFilterMenuLines() []string {
	if m.filter == nil {
		return nil
	}
	lines := []string{ui.TruncateString("    "+ui.HintStyle.Render("filters"), m.width)}
	cursor := m.filter.list.Cursor()
	for i, item := range m.filter.list.Items() {
		marker := "  "
		if i == cursor {
			marker = ui.IndicatorStyle.Render("█") + " "
		}
		box := "[ ]"
		if m.filterToggleOn(item.toggle) {
			box = "[x]"
		}
		line := fmt.Sprintf("    %s%s %s %s", marker, item.key, box, item.label)
		lines = append(lines, ui.TruncateString(line, m.width))
	}
	return lines
}

// viewWithModal renders the summary, the full table, and the active modal below
// it, replacing the footer. The modal bodies are ported onto List in a later
// slice; here they keep the current bespoke rendering (ADR-0079).
func (m QueueDashboard) viewWithModal() string {
	var body strings.Builder
	if m.err != nil {
		fmt.Fprintf(&body, "refresh error: %v\n", m.err)
	}
	if m.actionErr != nil {
		fmt.Fprintf(&body, "%s\n", dashboardActionErrorLine(m.actionErr))
	}
	fmt.Fprintf(&body, "Queue · %s\n", dashboardSummary(m.snap.Rows))
	fmt.Fprintln(&body)
	renderDashboardTable(&body, m.snap.Rows, m.list.Cursor(), m.width, m.height, m.liveCache())
	// avail is the number of body lines left for the modal below the table, so
	// its scroll window clamps long worktree/ref lists instead of overflowing.
	// A non-positive avail (no WindowSizeMsg yet) means "don't clamp".
	avail := 0
	if m.height > 0 {
		avail = m.height - strings.Count(body.String(), "\n")
	}
	switch {
	case m.bind != nil:
		renderDashboardBindModal(&body, m.bind, avail, m.width)
	case m.drainPick != nil:
		renderDashboardDrainModal(&body, m.drainPick, avail, m.width)
	case m.abandon != nil:
		renderDashboardAbandonModal(&body, m.abandon, m.width)
	}
	return body.String()
}

func dashboardSummary(rows []DashboardRow) string {
	total := len(rows)
	ready := 0
	running := 0
	autoDrain := 0
	maps := 0
	for _, row := range rows {
		if row.IsMap {
			maps++
			continue
		}
		if row.RawStatus == tasks.StatusReady {
			ready++
		}
		if row.LiveDrain {
			running++
		}
		if work.AutoDrainWaiting(row) {
			autoDrain++
		}
	}
	sets := total - maps
	parts := []string{countPhrase(sets, "task set", "task sets")}
	if maps > 0 {
		parts = append(parts, countPhrase(maps, "map", "maps"))
	}
	if ready > 0 {
		parts = append(parts, countPhrase(ready, "ready", "ready"))
	}
	if running > 0 {
		parts = append(parts, countPhrase(running, "running", "running"))
	}
	if autoDrain > 0 {
		parts = append(parts, countPhrase(autoDrain, "auto-drain", "auto-drain"))
	}
	return strings.Join(parts, " · ")
}

func countPhrase(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}

// viewDetail renders the full-screen task-set detail view. The task text peek
// (ADR-0079) and the task action-menu overlay keep their bespoke rendering; the
// loading, error, and content states compose through a Frame with the task list on
// ui.List.
func (m QueueDashboard) viewDetail() string {
	d := m.detail
	if d.peek != nil {
		var b strings.Builder
		renderTaskTextPeek(&b, d, m.height, m.width, m.taskMenu)
		return b.String()
	}
	if m.taskMenu != nil {
		var b strings.Builder
		renderDetailContent(&b, d, m.height, m.width, m.taskMenu)
		return b.String()
	}
	frame, body := m.detailFrame()
	return frame.Render(body)
}

// detailFrame builds the Frame and body for the non-menu detail states: loading,
// error, missing/malformed manifest, and the task-list content. The same Frame
// drives the body-height budget and the render (ADR-0079); the content body's List
// is sized to the budget the Frame leaves minus the table's own chrome, so the list
// clamps to the terminal instead of rendering every task.
func (m QueueDashboard) detailFrame() (ui.Frame, string) {
	d := m.detail
	if d.row.IsMap {
		return m.detailMapFrame()
	}
	const backHint = "h/esc back"
	if d.loading {
		return ui.Frame{Width: m.width, TermH: m.height, Hints: backHint}, fmt.Sprintf("Loading %s...", d.row.SetID)
	}
	if d.err != nil {
		return ui.Frame{Width: m.width, TermH: m.height, Hints: backHint}, fmt.Sprintf("error loading %s: %v", d.row.SetID, d.err)
	}

	manifest := d.manifest
	status := tasks.DeriveStatus(manifest)
	label := string(status)
	progress := ""
	if d.taskRow != nil {
		status = d.taskRow.Status
		label = tasks.StatusLabel(*d.taskRow)
		progress = d.taskRow.Progress
	}
	header := detailHeader(d.row.SetID, label, progress, d.taskRow)

	if status == tasks.StatusMissing {
		return ui.Frame{Width: m.width, TermH: m.height, Header: header, Hints: backHint}, "  registered task set missing"
	}
	if manifest == nil || !manifest.Valid {
		lines := []string{"  malformed manifest"}
		if manifest != nil {
			for _, e := range manifest.Errors {
				lines = append(lines, "  - "+e)
			}
		}
		return ui.Frame{Width: m.width, TermH: m.height, Header: header, Hints: backHint}, strings.Join(lines, "\n")
	}

	frame := ui.Frame{
		Width:  m.width,
		TermH:  m.height,
		Header: header,
		Status: d.statusMsg,
		Hints:  "j/k · gg/G top/bottom · l/enter peek · a actions · y copy name · h/esc back",
	}
	listH := frame.BodyHeight(m.height) - detailTableChromeLines
	if listH < 1 {
		listH = 1
	}
	d.list.Resize(listH)
	parts := []string{
		"",
		"  " + detailTableHeader(d.cols.idW),
		"  " + detailTableSeparator(d.cols.idW),
	}
	parts = append(parts, d.list.VisibleRows()...)
	return frame, strings.Join(parts, "\n")
}

func (m QueueDashboard) detailMapFrame() (ui.Frame, string) {
	d := m.detail
	const backHint = "h/esc back"
	if d.loading {
		return ui.Frame{Width: m.width, TermH: m.height, Hints: backHint}, fmt.Sprintf("Loading %s...", d.row.SetID)
	}
	if d.err != nil {
		return ui.Frame{Width: m.width, TermH: m.height, Hints: backHint}, fmt.Sprintf("error loading %s: %v", d.row.SetID, d.err)
	}
	if d.wfMap == nil {
		return ui.Frame{Width: m.width, TermH: m.height, Hints: backHint}, "  map not found"
	}
	if d.wfMap.Malformed {
		body := "  malformed map"
		if d.wfMap.MalformedReason != "" {
			body += ": " + d.wfMap.MalformedReason
		}
		return ui.Frame{
			Width:  m.width,
			TermH:  m.height,
			Header: detailMapHeader(*d.wfMap),
			Hints:  backHint,
		}, body
	}

	frame := ui.Frame{
		Width:  m.width,
		TermH:  m.height,
		Header: detailMapHeader(*d.wfMap),
		Status: d.statusMsg,
		Hints:  "j/k · gg/G top/bottom · l/enter peek · y copy name · h/esc back",
	}
	listH := frame.BodyHeight(m.height) - detailTableChromeLines
	if listH < 1 {
		listH = 1
	}
	d.ticketList.Resize(listH)
	parts := []string{
		"",
		"  " + detailMapTableHeader(d.ticketCols.nameW),
		"  " + detailMapTableSeparator(d.ticketCols.nameW),
	}
	parts = append(parts, d.ticketList.VisibleRows()...)
	return frame, strings.Join(parts, "\n")
}

// detailMapHeader builds the Map detail title line.
func detailMapHeader(m wayfinder.Map) string {
	counts := wayfinder.CountTickets(m.Tickets)
	frontier := len(wayfinder.Frontier(m.Tickets))
	return fmt.Sprintf("Map · %s  [WAYFINDING]  %d open / %d frontier", m.ID, counts.Open, frontier)
}

func detailTicketName(t wayfinder.Ticket) string {
	if t.Slug != "" {
		return t.ID + "-" + t.Slug
	}
	return t.ID
}

func detailTicketFilename(t wayfinder.Ticket) string {
	if t.Slug != "" {
		return t.ID + "-" + t.Slug + ".md"
	}
	return t.ID + ".md"
}

func detailTicketNameWidth(items []wayfinder.Ticket) int {
	w := len("TICKET")
	for _, t := range items {
		if n := len(detailTicketName(t)); n > w {
			w = n
		}
	}
	return w
}

var (
	mapFrontierTicketStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	mapDimTicketStyle      = lipgloss.NewStyle().Faint(true)
)

func detailMapTicketStatusLabel(t wayfinder.Ticket, frontier map[string]bool) string {
	switch t.Status {
	case wayfinder.TicketResolved:
		return "resolved"
	case wayfinder.TicketClaimed:
		return "claimed"
	case wayfinder.TicketOpen:
		if frontier[t.ID] {
			return "open"
		}
		return "open (blocked)"
	default:
		return string(t.Status)
	}
}

func detailMapTicketLine(t wayfinder.Ticket, nameW int, frontier map[string]bool) string {
	return fmt.Sprintf("%-*s  %-*s  %s",
		nameW, detailTicketName(t),
		detailTypeW, t.Type,
		detailMapTicketStatusLabel(t, frontier))
}

func styledDetailMapTicketLine(t wayfinder.Ticket, nameW int, frontier map[string]bool) string {
	line := detailMapTicketLine(t, nameW, frontier)
	if frontier[t.ID] {
		return mapFrontierTicketStyle.Render(line)
	}
	return mapDimTicketStyle.Render(line)
}

func detailMapTableHeader(nameW int) string {
	return fmt.Sprintf("%-*s  %-*s  %s",
		nameW, "TICKET", detailTypeW, "TYPE", "STATUS")
}

func detailMapTableSeparator(nameW int) string {
	return fmt.Sprintf("%-*s  %-*s  %s",
		nameW, strings.Repeat("-", nameW),
		detailTypeW, strings.Repeat("-", detailTypeW),
		strings.Repeat("-", 10))
}

// detailHeader builds the detail view's title line: "Task · <set>  [<status>]"
// plus progress and, when applicable, the Verified-at SHA badge inside the
// status brackets (ADR-0096/0156).
func detailHeader(setID, label, progress string, taskRow *tasks.Row) string {
	if taskRow != nil {
		if badgeText := dashboardVerifiedAtBadgeStyled(DashboardRow{
			SetRef:            SetRef{RawStatus: taskRow.Status},
			VerifiedAtSHA:     taskRow.VerifiedAtSHA,
			VerifiedAtDrifted: taskRow.VerifiedAtDrifted,
		}); badgeText != "" {
			label += " · " + badgeText
		}
	}
	header := fmt.Sprintf("Task · %s  [%s]", setID, label)
	if progress != "" {
		header += "  " + progress
	}
	return header
}

// detailIDWidth returns the ID-column width: the widest task ID, floored at the
// "ID" header label.
func detailIDWidth(items []tasks.Task) int {
	idW := len("ID")
	for _, t := range items {
		if len(t.ID) > idW {
			idW = len(t.ID)
		}
	}
	return idW
}

// detailTaskLine formats one task row's cells (status / type / id / title /
// blocked-by) over the fixed and idW-derived widths, without the cursor prefix —
// the List owns the leading indicator column.
func detailTaskLine(t tasks.Task, idW int) string {
	title := t.Title
	if len(title) > detailTitleW {
		title = title[:detailTitleW-3] + "..."
	}
	blockedBy := "-"
	if len(t.BlockedBy) > 0 {
		blockedBy = strings.Join(t.BlockedBy, ", ")
	}
	statusCell := string(t.Status)
	if t.Status == tasks.TaskFailed && t.FailedAfter != nil {
		statusCell = fmt.Sprintf("failed(%d)", *t.FailedAfter)
	}
	return fmt.Sprintf("%-*s  %-*s  %-*s  %-*s  %s",
		detailStatusW, statusCell, detailTypeW, t.Type, idW, t.ID, detailTitleW, title, blockedBy)
}

// detailTableHeader is the detail task-table column header, idW-aligned to match
// detailTaskLine.
func detailTableHeader(idW int) string {
	return fmt.Sprintf("%-*s  %-*s  %-*s  %-*s  %s",
		detailStatusW, "STATUS", detailTypeW, "TYPE", idW, "ID", detailTitleW, "TITLE", "BLOCKED-BY")
}

// detailTableSeparator is the dashed rule under detailTableHeader.
func detailTableSeparator(idW int) string {
	return fmt.Sprintf("%-*s  %-*s  %-*s  %-*s  %s",
		detailStatusW, strings.Repeat("-", detailStatusW),
		detailTypeW, strings.Repeat("-", detailTypeW),
		idW, strings.Repeat("-", idW),
		detailTitleW, strings.Repeat("-", detailTitleW),
		strings.Repeat("-", 12))
}

// renderDetailContent renders the detail task list with the action-menu overlay
// spliced next to the cursored task (ADR-0079 bespoke placement; its cursor is
// ported onto List in a later slice). It renders every task — no scroll window —
// and reads the cursor from the List. The non-menu states render via detailFrame.
func renderDetailContent(b *strings.Builder, d *detailView, height, width int, menu *taskMenu) {
	manifest := d.manifest
	taskRow := d.taskRow

	status := tasks.DeriveStatus(manifest)
	label := string(status)
	progress := ""
	if taskRow != nil {
		status = taskRow.Status
		label = tasks.StatusLabel(*taskRow)
		progress = taskRow.Progress
	}

	header := detailHeader(d.row.SetID, label, progress, taskRow)
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

	idW := d.cols.idW
	fmt.Fprintf(b, "  %s\n", detailTableHeader(idW))
	fmt.Fprintf(b, "  %s\n", detailTableSeparator(idW))

	cursorIdx := d.list.Cursor()
	var menuLines []string
	placeBelow := true
	if menu != nil && !menu.inPeek {
		menuLines = taskMenuLines(menu, width)
		placeBelow = dashboardMenuPlaceBelow(cursorIdx, len(menuLines), height)
	}
	writeMenu := func() {
		for _, ml := range menuLines {
			fmt.Fprintf(b, "%s\n", ml)
		}
	}
	for i, t := range manifest.Tasks {
		if menuLines != nil && i == cursorIdx && !placeBelow {
			writeMenu()
		}
		prefix := "  "
		if i == cursorIdx {
			prefix = ui.IndicatorStyle.Render("█") + " "
		}
		fmt.Fprintf(b, "%s%s\n", prefix, detailTaskLine(t, idW))
		if menuLines != nil && i == cursorIdx && placeBelow {
			writeMenu()
		}
	}

	fmt.Fprintln(b)
	if d.statusMsg != "" {
		fmt.Fprintf(b, "  %s\n", d.statusMsg)
	}
	hint := "  j/k · gg/G top/bottom · l/enter peek · a actions · y copy name · h/esc back"
	if menu != nil {
		hint = "  j/k move · enter/letter run · esc close"
	}
	writeDashboardFooter(b, height, ui.HintStyle.Render(hint))
}

// taskMenuLines renders the task-level action overlay as a block of lines,
// indented to nest under the cursored task, with the highlighted item carrying
// the shared cursor block. The first line is a dimmed "actions" caption. It
// mirrors dashboardMenuLines (the set-view overlay) for a consistent look.
func taskMenuLines(menu *taskMenu, width int) []string {
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

func renderTaskTextPeek(b *strings.Builder, d *detailView, height, width int, menu *taskMenu) {
	p := d.peek
	header := d.row.SetID
	if p.taskID != "" {
		header += " / " + p.taskID
	}
	fmt.Fprintln(b, header)
	if menu != nil && menu.inPeek {
		for _, ml := range taskMenuLines(menu, width) {
			fmt.Fprintln(b, ml)
		}
	}
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
			fmt.Fprintln(b, ui.TruncateString(line, width))
		}
	}
	fmt.Fprintln(b)
	if p.statusMsg != "" {
		fmt.Fprintf(b, "  %s\n", p.statusMsg)
	}
	position := ""
	if maxScroll > 0 {
		position = fmt.Sprintf(" · %d/%d", p.scroll+1, len(lines))
	}
	hint := "  j/k · C-d/C-u · gg/G · y copy name · a actions · h/esc back" + position
	if menu != nil && menu.inPeek {
		hint = "  j/k move · enter/letter run · esc close"
	}
	writeDashboardFooter(b, height, ui.HintStyle.Render(hint))
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

// writeModalListRows renders a modal list's scroll window: it sizes the list to
// listH rows (or, when listH is non-positive, to its full length so the caller's
// "don't clamp" mode renders every row) and writes the rows the List returns,
// including its cursor/pad prefix column. Each row is truncated to width so the
// modal never spills past the terminal edge.
func writeModalListRows[T any](w io.Writer, list *ui.List[T], listH, width int) {
	if list == nil {
		return
	}
	if listH < 1 {
		listH = list.Len()
	}
	list.Resize(listH)
	for _, line := range list.VisibleRows() {
		fmt.Fprintln(w, ui.TruncateString(line, width))
	}
}

func renderDashboardBindModal(w io.Writer, modal *dashboardBindModal, avail, width int) {
	if modal == nil {
		return
	}
	fmt.Fprintln(w, ui.TruncateString("Bind worktree", width))
	if modal.loading {
		fmt.Fprintln(w, ui.TruncateString("  loading...", width))
		return
	}
	switch modal.stage {
	case dashboardBindStageWorktree:
		// Chrome above/below the list: the "Bind worktree" title and the hint.
		writeModalListRows(w, modal.list, modalListHeight(avail, 2), width)
		fmt.Fprint(w, ui.HintStyle.Render(ui.TruncateString("enter select · esc cancel", width)))
	case dashboardBindStageBaseRef:
		fmt.Fprintln(w, ui.TruncateString("Base ref", width))
		// Chrome: the title, the "Base ref" caption, and the hint.
		writeModalListRows(w, modal.list, modalListHeight(avail, 3), width)
		fmt.Fprint(w, ui.HintStyle.Render(ui.TruncateString("enter select · esc cancel", width)))
	case dashboardBindStageName:
		fmt.Fprintln(w, ui.TruncateString(fmt.Sprintf("Base: %s", modal.baseRef), width))
		fmt.Fprintln(w, ui.TruncateString(fmt.Sprintf("Name: %s", modal.name), width))
		fmt.Fprint(w, ui.HintStyle.Render(ui.TruncateString("enter create · esc cancel", width)))
	}
}

func renderDashboardDrainModal(w io.Writer, modal *dashboardDrainModal, avail, width int) {
	if modal == nil {
		return
	}
	fmt.Fprintln(w, ui.TruncateString(fmt.Sprintf("Drain target for %s", modal.row.SetID), width))
	if modal.loading {
		fmt.Fprintln(w, ui.TruncateString("  draining...", width))
		return
	}
	// Chrome above/below the list: the title line and the hint.
	writeModalListRows(w, modal.list, modalListHeight(avail, 2), width)
	fmt.Fprint(w, ui.HintStyle.Render(ui.TruncateString("enter drain · esc cancel", width)))
}

// modalListHeight derives a modal list's scroll-window height from the body
// lines left for the modal (avail) minus its chrome lines. A non-positive avail
// (no WindowSizeMsg yet) returns 0, signalling writeModalListRows to render every
// row unclamped; otherwise it floors the window at one row.
func modalListHeight(avail, chrome int) int {
	if avail <= 0 {
		return 0
	}
	h := avail - chrome
	if h < 1 {
		h = 1
	}
	return h
}

func renderDashboardAbandonModal(w io.Writer, modal *dashboardAbandonModal, width int) {
	if modal == nil {
		return
	}
	fmt.Fprintln(w, ui.TruncateString(fmt.Sprintf("Unbind worktree for %s", modal.row.SetID), width))
	if modal.loading {
		fmt.Fprintln(w, ui.TruncateString("  unbinding...", width))
		return
	}
	fmt.Fprintln(w, ui.TruncateString("This releases the binding without integrating. Task statuses are unchanged.", width))
	fmt.Fprint(w, ui.HintStyle.Render(ui.TruncateString("y confirm · enter/n/esc cancel", width)))
}

func renderDashboardTable(w io.Writer, rows []DashboardRow, cursor, width, height int, live livePaneCache) {
	renderDashboardTableWithMenu(w, rows, cursor, width, height, nil, live)
}

// renderDashboardTableWithMenu renders the task-set table and, when menu is
// non-nil, splices the action overlay in next to the cursored row: below it by
// default, flipping above when the cursor sits too low for the menu to fit
// beneath it within height (dashboardMenuPlaceBelow). live colours handoff-verb
// keys in the overlay (ADR-0158).
func renderDashboardTableWithMenu(w io.Writer, rows []DashboardRow, cursor, width, height int, menu *dashboardMenu, live livePaneCache) {
	if dashboardTwoLineMode(rows, width, height) {
		renderDashboardTableTwoLineWithMenu(w, rows, cursor, width, height, menu, live)
		return
	}
	headers := dashboardTableHeaders()
	widths := dashboardTableWidthsForRows(rows, width)
	fmt.Fprintf(w, "%s\n", ui.TruncateString("  "+dashboardTableLine(headers, widths), width))
	fmt.Fprintf(w, "%s\n", ui.TruncateString("  "+dashboardTableSeparator(widths), width))

	var menuLines []string
	placeBelow := true
	if menu != nil {
		menuLines = dashboardMenuLines(menu, width, live)
		placeBelow = dashboardMenuPlaceBelow(cursor, len(menuLines), height)
	}
	writeMenu := func() {
		for _, ml := range menuLines {
			fmt.Fprintf(w, "%s\n", ml)
		}
	}
	for i, row := range rows {
		if menu != nil && i == cursor && !placeBelow {
			writeMenu()
		}
		var prefix string
		if i == cursor {
			prefix = ui.IndicatorStyle.Render("█") + " "
		} else {
			prefix = "  "
		}
		line := ui.TruncateString(prefix+dashboardTableLine(dashboardRowValues(row, live), widths), width)
		fmt.Fprintf(w, "%s\n", line)
		if menu != nil && i == cursor && placeBelow {
			writeMenu()
		}
	}
}

// renderDashboardTableTwoLineWithMenu renders the two-line task-set table and,
// when menu is non-nil, splices the action overlay next to the cursored row.
// Each row occupies two terminal lines: line 1 holds the activity cluster,
// PROJECT, TASK SET (the set id) and WORKTREE; line 2 holds STATUS indented under
// the TASK SET column.
func renderDashboardTableTwoLineWithMenu(w io.Writer, rows []DashboardRow, cursor, width, height int, menu *dashboardMenu, live livePaneCache) {
	line1Widths := dashboardTwoLineFitWidths(dashboardTwoLineNaturalWidths(rows), dashboardTableBodyBudget(width))
	fmt.Fprintf(w, "%s\n", ui.TruncateString("  "+dashboardTwoLineTableHeader(line1Widths), width))
	fmt.Fprintf(w, "%s\n", ui.TruncateString("  "+dashboardTwoLineStatusHeader(line1Widths), width))
	fmt.Fprintf(w, "%s\n", ui.TruncateString("  "+dashboardTwoLineTableSeparator(line1Widths), width))

	var menuLines []string
	placeBelow := true
	if menu != nil {
		menuLines = dashboardMenuLines(menu, width, live)
		placeBelow = dashboardMenuPlaceBelowTwoLine(cursor, len(menuLines), height)
	}
	writeMenu := func() {
		for _, ml := range menuLines {
			fmt.Fprintf(w, "%s\n", ml)
		}
	}
	for i, row := range rows {
		if menu != nil && i == cursor && !placeBelow {
			writeMenu()
		}
		var prefix string
		if i == cursor {
			prefix = ui.IndicatorStyle.Render("█") + " "
		} else {
			prefix = "  "
		}
		line1 := ui.TruncateString(prefix+dashboardTwoLineRowLine1(row, line1Widths, live), width)
		line2 := ui.TruncateString("  "+dashboardTwoLineRowLine2(row, line1Widths), width)
		fmt.Fprintf(w, "%s\n", line1)
		fmt.Fprintf(w, "%s\n", line2)
		if menu != nil && i == cursor && placeBelow {
			writeMenu()
		}
	}
}

// dashboardMenuPlaceBelowTwoLine is the two-line-mode variant of
// dashboardMenuPlaceBelow. Each row consumes two terminal lines, so the space
// below the cursored row is reduced accordingly.
func dashboardMenuPlaceBelowTwoLine(cursor, menuHeight, height int) bool {
	if height <= 0 {
		return true
	}
	linesBelowCursor := height - (dashboardTableTopOffset + 1) - 2*(cursor+1)
	return linesBelowCursor >= menuHeight
}

// dashboardTableTopOffset is the number of lines above the first table row in
// the dashboard view: the summary line, a blank, the header, and the separator.
const dashboardTableTopOffset = 4

// dashboardMenuPlaceBelow reports whether the action menu of menuHeight lines
// should render below the cursor row (true) or flip above it (false). It flips
// above only when the cursor sits low enough that the menu would not fit beneath
// it within the viewport. A non-positive height (no WindowSizeMsg yet) keeps the
// menu below.
func dashboardMenuPlaceBelow(cursor, menuHeight, height int) bool {
	if height <= 0 {
		return true
	}
	linesBelowCursor := height - 1 - dashboardTableTopOffset - cursor
	return linesBelowCursor >= menuHeight
}

// dashboardMenuLines renders the action overlay as a block of lines indented to
// nest under the cursored row, with the highlighted item carrying the shared
// cursor block. The first line is a dimmed "actions" caption. When the status
// submenu is open it renders that instead. Handoff-verb keys are coloured by
// the live-pane affordance (ADR-0158).
func dashboardMenuLines(menu *dashboardMenu, width int, live livePaneCache) []string {
	if menu == nil {
		return nil
	}
	if menu.status != nil {
		return dashboardStatusMenuLines(menu.status, width)
	}
	lines := []string{ui.TruncateString("    "+ui.HintStyle.Render("actions"), width)}
	cursor := menu.list.Cursor()
	for i, item := range menu.list.Items() {
		marker := "  "
		if i == cursor {
			marker = ui.IndicatorStyle.Render("█") + " "
		}
		key := styleHandoffKey(item.key, menuItemLiveState(item, menu.row, live))
		line := fmt.Sprintf("    %s%s  %s", marker, key, item.label)
		lines = append(lines, ui.TruncateString(line, width))
	}
	return lines
}

// dashboardStatusMenuLines renders the nested status submenu.
func dashboardStatusMenuLines(status *dashboardStatusMenu, width int) []string {
	if status == nil {
		return nil
	}
	lines := []string{ui.TruncateString("    "+ui.HintStyle.Render("status"), width)}
	cursor := status.list.Cursor()
	for i, item := range status.list.Items() {
		marker := "  "
		if i == cursor {
			marker = ui.IndicatorStyle.Render("█") + " "
		}
		line := fmt.Sprintf("    %s%s  %s", marker, item.key, item.label)
		lines = append(lines, ui.TruncateString(line, width))
	}
	return lines
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

// RunDashboard opens the read-only Work dashboard TUI. It returns the bound
// checkout path chosen with Ctrl-g on the main list (empty when the dashboard
// quit for any other reason), leaving the workbench-aware open to the command
// layer (task 02).
func RunDashboard(d *Deps, cfg *config.Config) (string, error) {
	snap, err := work.BuildSnapshot(d.WorkDeps(), cfg)
	if err != nil {
		return "", err
	}
	m := newQueueDashboard(d, cfg, snap)
	program := tea.NewProgram(m)
	final, err := program.Run()
	if err != nil {
		return "", err
	}
	if fm, ok := final.(QueueDashboard); ok {
		return fm.openCheckout, nil
	}
	return "", nil
}
