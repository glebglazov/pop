package dashboard

import (
	"errors"
	"fmt"
	"github.com/glebglazov/pop/tasks/drain"
	"io"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/debug"
	"github.com/glebglazov/pop/history"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/setkind"
	"github.com/glebglazov/pop/ui"
	"github.com/glebglazov/pop/wayfinder"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

const dashboardPollInterval = 2 * time.Second

// dashboardHandoffPending is shown from the moment a Handoff verb dispatches
// until it either quits the dashboard or reports why it could not. A handoff
// ends the surface that could report progress, so without this a slow one is
// indistinguishable from a key that did nothing — which is exactly how the
// pre-ADR-0167 drain latency was experienced (ADR-0167).
const dashboardHandoffPending = "handing off…"

// dashboardSpawnPending is the same reassurance for a spawn that does not move the
// operator: the dashboard stays open, so the key has to say it did something while
// the agent starts.
const dashboardSpawnPending = "spawning…"

// A dashboard row is a Work container — there is no row model beside it — and
// the seam's data types live in the top-level work package (ADR-0143). These
// aliases preserve queue's local vocabulary and its exported surface
// (queue.DashboardRow / DashboardSnapshot) for consumers like dashboardshell and
// cmd; the names on the other side drop the Dashboard prefix.
type (
	DashboardRow      = work.Container
	DashboardSnapshot = work.Snapshot
)

// dashboardTickMsg and dashboardRowsMsg name the page they belong to. Both pages
// are this same model, so an untagged poll would let one page's tick drive the
// other's reload — and one page's rows land in the other's table. A page ignores
// what is not its own, which also lets the entry shell hand every message to the
// page in focus without cross-wiring the two.
type dashboardTickMsg struct {
	page Page
}

// dashboardLivePrimeMsg carries the open-time live-pane cache, ahead of the
// first poll's full snapshot rebuild.
type dashboardLivePrimeMsg struct {
	live livePaneCache
}
type dashboardRowsMsg struct {
	page Page
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

// dashboardItemTextMsg carries one Work item's text back to the detail peek.
type dashboardItemTextMsg struct {
	itemID string
	path   string
	text   string
	err    error
}
type dashboardBindListMsg struct {
	row     DashboardRow
	entries []drain.BindEntry
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
	entries []drain.DrainEntry
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

type dashboardBindModal struct {
	row   DashboardRow
	stage dashboardBindStage
	// list drives the worktree-pick and base-ref-pick stages (both wrapping).
	// Base refs are held as entries with only Label set. The name stage is a
	// plain text input and does not use the list.
	list    *ui.List[drain.BindEntry]
	baseRef string
	name    string
	loading bool
}

// bindEntryCell renders one bind-modal row: the worktree label (falling back to
// the checkout path) or, in the base-ref stage, the ref held in Label.
func bindEntryCell(e drain.BindEntry, _ ui.RowState) string {
	if e.Label != "" {
		return e.Label
	}
	return e.Path
}

// newBindEntryList builds the wrapping list backing a bind-modal list stage.
func newBindEntryList(entries []drain.BindEntry) *ui.List[drain.BindEntry] {
	return ui.NewList(entries, ui.Opts[drain.BindEntry]{
		Wrap: true,
		Cell: bindEntryCell,
	})
}

// bindRefEntries wraps base refs as bind entries so the base-ref stage reuses
// the same wrapping list as the worktree-pick stage.
func bindRefEntries(refs []string) []drain.BindEntry {
	entries := make([]drain.BindEntry, len(refs))
	for i, ref := range refs {
		entries[i] = drain.BindEntry{Label: ref}
	}
	return entries
}

// dashboardDrainModal is the Drain target picker shown when `i` is pressed on an
// unbound set: pick an existing worktree to adopt, a new managed worktree to
// provision off the trunk (the default cursor), or the trunk itself — then bind
// (or stay unbound for trunk) and drain in one action. A bound set skips the
// picker and resumes in its binding (ADR-0052).
type dashboardDrainModal struct {
	row     DashboardRow
	list    *ui.List[drain.DrainEntry]
	loading bool
}

// newDashboardDrainModal builds the Drain target picker with a wrapping list,
// positioning the cursor on "new managed worktree" — the frictionless default.
func newDashboardDrainModal(row DashboardRow, entries []drain.DrainEntry) *dashboardDrainModal {
	list := ui.NewList(entries, ui.Opts[drain.DrainEntry]{
		Wrap: true,
		Cell: func(e drain.DrainEntry, _ ui.RowState) string {
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

// dashboardMenuItem is one verb in the action menu overlay: the flat shortcut
// letter it keeps, the label shown beside it, and the verb id it dispatches. All
// three are the owning kind's (ADR-0173) — the dashboard authors no verb of its
// own, it only recognises the ids whose modal it still owns.
type dashboardMenuItem struct {
	key   string
	label string
	verb  work.Verb
}

// dashboardStatusMenu is the nested status overlay opened with `s` from the
// action menu. Its items are the row's own kind's StatusActions — a task set's
// five task/archive writes, a Map's four lifecycle writes (ADR-0186) — so this
// file holds no status vocabulary of any kind. Every one of them writes
// in-process (ADR-0158) and is performed through the kind's own Perform, like any
// other verb the dashboard dispatches.
type dashboardStatusMenu struct {
	row  DashboardRow
	list *ui.List[work.Action]
}

// newDashboardStatusMenu opens the status submenu over row with the verbs its
// kind offers right now. A kind that offers none never gets here: it does not
// offer the opener either.
func newDashboardStatusMenu(kinds workKinds, row DashboardRow) *dashboardStatusMenu {
	return &dashboardStatusMenu{
		row:  row,
		list: ui.NewList(kinds.statusActionsFor(row), ui.Opts[work.Action]{Wrap: true}),
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
// dashboardMenuItems returns the verbs applicable to row: whatever the row's own
// Work kind offers over it right now (ADR-0173). Conditional verbs are the
// kind's to filter — a task set shows verify only where a verdict can move it,
// unbind only when bound, unpark only when parked; a Map shows its frontier verb
// only when it has a frontier — and the dashboard neither knows nor reproduces
// those rules. `copy-name` and `shell` come back on the same key from every kind.
func dashboardMenuItems(kinds workKinds, row DashboardRow) []dashboardMenuItem {
	actions := kinds.actionsFor(row)
	items := make([]dashboardMenuItem, 0, len(actions))
	for _, a := range actions {
		items = append(items, dashboardMenuItem{key: a.Key, label: a.Label, verb: a.Verb})
	}
	return items
}

// dashboardVerifyEligible reports whether the verify verb applies to row: a set
// a verdict can still move that no live drain holds (ADR-0123). It keys on the
// verification mark rather than the status, so a human-completed set — DONE with
// an unverified mark — still offers the verb. It is the single guard shared by
// the menu (inclusion) and dispatch (self-containment).
func dashboardVerifyEligible(row DashboardRow) bool {
	if row.LiveDrain {
		return false
	}
	return row.VerifyMark == tasks.VerifyMarkUnverified || row.VerifyMark == tasks.VerifyMarkFailed
}

// dashboardFoldEligible reports whether the fold verb applies to row: a bound DONE
// or AWAITING-APPROVAL set (ADR-0148, ADR-0156).
func dashboardFoldEligible(row DashboardRow) bool {
	return row.Bound && tasks.FoldEligibleStatus(row.RawStatus)
}

// dashboardMenuItemHandoff reports whether a menu item hands off. The key's case
// is the fact, not a second list to keep in step with the kinds: ADR-0158 makes
// an uppercase key mean "spawns or focuses a pane and leaves the surface", so a
// kind that follows the rule needs no registration here.
func dashboardMenuItemHandoff(item dashboardMenuItem) bool {
	return item.key != "" && item.key != strings.ToLower(item.key)
}

// items are the verbs the open menu is showing, empty for a menu with no list yet.
func (menu *dashboardMenu) items() []dashboardMenuItem {
	if menu == nil || menu.list == nil {
		return nil
	}
	return menu.list.Items()
}

// newDashboardMenu opens the action overlay on row, wrapping the kind's verbs in
// a ui.List with j/k wrap-around navigation. When pinned is true the menu
// survives in-place verbs and J/K move the row cursor beneath it.
func newDashboardMenu(kinds workKinds, row DashboardRow, pinned bool) *dashboardMenu {
	return &dashboardMenu{
		row:    row,
		pinned: pinned,
		list:   ui.NewList(dashboardMenuItems(kinds, row), ui.Opts[dashboardMenuItem]{Wrap: true}),
	}
}

// syncPinnedMenuRow re-filters the pinned menu to the dashboard's cursored row.
func (m QueueDashboard) syncPinnedMenuRow() (tea.Model, tea.Cmd) {
	if m.menu == nil || !m.menu.pinned {
		return m, nil
	}
	row, ok := m.list.Selected()
	items := dashboardMenuItems(m.kinds, row)
	if !ok || len(items) == 0 {
		m.menu = nil
		return m, nil
	}
	m.menu.row = row
	m.menu.status = nil
	m.menu.list = ui.NewList(items, ui.Opts[dashboardMenuItem]{Wrap: true})
	return m, nil
}

// dashboardFilterToggle identifies one row-inclusion view filter the filter
// menu flips. Both of today's toggles are cross-kind view flags on the model's
// deps — Show done (ADR-0121) and Show archived (ADR-0186) — and the enum and the
// item list are the extension point for future inclusion filters (by status, by
// project).
type dashboardFilterToggle int

const (
	filterToggleShowDone dashboardFilterToggle = iota
	filterToggleShowArchived
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
		// Archived rows are how unarchive is reachable at all: the verb is in the
		// row's own status submenu, and an archived row is off screen by default
		// (ADR-0186). Like show done it is session-only and starts off.
		{key: "a", label: "show archived", toggle: filterToggleShowArchived},
	}
}

// newDashboardFilterMenu opens the filter modal with j/k wrap-around navigation.
func newDashboardFilterMenu() *dashboardFilterMenu {
	return &dashboardFilterMenu{
		list: ui.NewList(dashboardFilterItems(), ui.Opts[dashboardFilterItem]{Wrap: true}),
	}
}

// itemMenu is the action overlay opened with `a` over a single Work item — in
// the detail view (over the cursored item) or the item text peek (over the
// previewed one). Its verbs are the owning kind's ItemActions, asked for when the
// menu opens rather than carried on the item, so eligibility is as fresh as the
// keypress (ADR-0173). inPeek marks which view it was opened from so the renderer
// can place it correctly.
type itemMenu struct {
	item   work.Item
	list   *ui.List[work.Action]
	inPeek bool
}

// newItemMenu wraps the kind's verbs for one item in a ui.List with j/k
// wrap-around navigation. inPeek records which detail view opened it.
func newItemMenu(item work.Item, actions []work.Action, inPeek bool) *itemMenu {
	return &itemMenu{
		item:   item,
		list:   ui.NewList(actions, ui.Opts[work.Action]{Wrap: true}),
		inPeek: inPeek,
	}
}

// detailView is the full-screen container detail that replaces the table. It is
// generic over kinds: the kind's prose sections render above one item list, and
// every item verb comes from that kind (ADR-0173) — a new kind gets a detail view
// by filling Items and DetailSections, never by writing a frame of its own.
//
// Its data is the container itself, refreshed by the same periodic rebuild that
// feeds the table: there is no second loader to drift from the rows behind it.
// ReplaceItems re-anchors the cursor by item id on refresh (ADR-0079).
type detailView struct {
	row  work.Container
	list *ui.List[work.Item]
	cols *detailColumns
	peek *itemTextPeek
	// statusMsg is a transient one-line message shown above the hint bar.
	// Set to a hint on invalid transition; set to confirmation on success.
	statusMsg string
}

// detailColumns holds the detail item list's ID-column width, precomputed over the
// container's items (the status/type/title columns are fixed). The List's Cell closure
// closes over a pointer to it so a refresh updates the width in place,
// matching the house pattern (dashboardColumns / pickerCell).
type detailColumns struct {
	idW int
}

// Detail item-table column widths. Status, type, and title are fixed; the ID
// column grows to the widest item ID (floored at the "ID" header).
const (
	detailStatusW = 10
	detailTypeW   = 4
	detailTitleW  = 40
)

// detailTableChromeLines is the number of body lines above the detail List rows:
// the blank line under the header (or under the last prose section), the column
// header, and the separator.
const detailTableChromeLines = 3

// newDetailView builds the detail view for row over the items the container
// already carries.
func newDetailView(row work.Container) *detailView {
	cols := &detailColumns{idW: len("ID")}
	d := &detailView{row: row, cols: cols}
	d.list = ui.NewList([]work.Item{}, ui.Opts[work.Item]{
		Key:    func(i work.Item) string { return i.ID },
		Anchor: ui.AnchorTop,
		Cell: func(i work.Item, _ ui.RowState) string {
			return detailItemLine(i, cols.idW)
		},
	})
	d.sync(row)
	return d
}

// sync adopts a rebuilt container: the same container the table now shows, with
// its items fed to the List (which re-anchors the cursor by item id) and the ID
// column re-measured.
func (d *detailView) sync(row work.Container) {
	d.row = row
	d.cols.idW = detailIDWidth(row.Items)
	d.list.ReplaceItems(row.Items)
}

// itemByID returns the container item with the given id, or false if absent.
func (d *detailView) itemByID(id string) (work.Item, bool) {
	for _, item := range d.row.Items {
		if item.ID == id {
			return item, true
		}
	}
	return work.Item{}, false
}

type itemTextPeek struct {
	itemID  string
	path    string
	text    string
	loading bool
	err     error
	scroll  int
	// statusMsg is a transient one-line message shown above the hint bar.
	statusMsg string
}

// dashboardColumns holds the task-set table's natural column widths (derived from
// content) and the fitted widths clamped to the terminal budget. The List's Cell
// closure closes over a pointer to it so a reload or filter can update widths in
// place without rebuilding the list — matching the house pattern of pickerCell
// closing over its picker.
type QueueDashboard struct {
	d   *drain.Deps
	cfg *config.Config
	// page is which of the dashboard's two pages this model shows: which kinds it
	// lists, which of them heads its columns, and the words its chrome uses. The
	// model is otherwise identical on both pages — that is what makes them pages of
	// one dashboard rather than two TUIs.
	page dashboardPage
	// kinds is the wired Work-kind list indexed by id: every cell a row renders
	// and every verb its menu offers is asked of the kind that owns the row, so
	// the dashboard branches on no kind of its own (ADR-0173).
	kinds   workKinds
	snap    DashboardSnapshot
	allRows []DashboardRow // source of truth; snap.Containers is the filtered view
	list    *ui.List[DashboardRow]
	cols    *dashboardColumns
	err     error
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
	itemMenu  *itemMenu
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
	return row.ID
}

// copyClipboard writes payload via Clipboard copy and returns a transient status
// message naming what was copied, or the error.
func (m QueueDashboard) copyClipboard(payload string) string {
	if err := m.clipboardCopy()(payload); err != nil {
		return fmt.Sprintf("copy failed: %v", err)
	}
	return fmt.Sprintf("copied %s", payload)
}

// copyRowName copies the cursored row's identifier via Clipboard copy and
// returns a transient status message naming what was copied, or the error.
func (m QueueDashboard) copyRowName(row DashboardRow) string {
	return m.copyClipboard(rowCopyNamePayload(row))
}

// TestDashboardRow completes a partially filled dashboard row for tests outside
// the queue package: it fills in the identity cells a row is never without. The
// cursor key mirrors production derivation from project and set ID.
func TestDashboardRow(project, setID string, row DashboardRow) DashboardRow {
	if row.ID == "" {
		row.ID = setID
	}
	row.Project = project
	row.CursorKey = project + "\x00" + row.ID
	return row
}

// NewDashboard constructs a Work dashboard model from a snapshot, on page A.
func NewDashboard(d *drain.Deps, cfg *config.Config, snap DashboardSnapshot) QueueDashboard {
	return newQueueDashboard(d, cfg, snap)
}

// NewDashboardOn constructs the model on one named page. It is how the entry
// layer opens the dashboard on page B — `pop routine dashboard` is this call and
// nothing else.
func NewDashboardOn(d *drain.Deps, cfg *config.Config, snap DashboardSnapshot, page Page) QueueDashboard {
	return newQueueDashboardOn(d, cfg, snap, pageSpec(page))
}

// OpenPage builds one page's snapshot and returns the model showing it. A failed
// build is the page's own error chrome, not a returned error: the entry shell
// opens a page the first time the operator asks for it, and a Routines page that
// will not build must not take the Task sets down with it (ADR-0189).
func OpenPage(d *drain.Deps, cfg *config.Config, page Page) QueueDashboard {
	snap, err := BuildPageSnapshot(d, cfg, page)
	m := NewDashboardOn(d, cfg, snap, page)
	m.err = err
	return m
}

// BuildPageSnapshot builds one page's snapshot: only the kinds that page lists,
// so kind precedence orders that page alone and every Kind.Summary counts only
// the containers on it.
func BuildPageSnapshot(d *drain.Deps, cfg *config.Config, page Page) (DashboardSnapshot, error) {
	if d == nil {
		d = drain.DefaultDeps()
	}
	return work.BuildSnapshot(pageSpec(page).kinds(d, cfg))
}

func newQueueDashboard(d *drain.Deps, cfg *config.Config, snap DashboardSnapshot) QueueDashboard {
	return newQueueDashboardOn(d, cfg, snap, workPage())
}

func newQueueDashboardOn(d *drain.Deps, cfg *config.Config, snap DashboardSnapshot, page dashboardPage) QueueDashboard {
	if d == nil {
		d = drain.DefaultDeps()
	}
	kinds := newWorkKinds(page.kinds(d, cfg))
	cols := &dashboardColumns{page: page}
	cols.syncNatural(kinds, snap.Containers)
	live := &livePaneCache{}
	var list *ui.List[DashboardRow]
	list = ui.NewList(snap.Containers, ui.Opts[DashboardRow]{
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
					return ui.TruncateString(dashboardTwoLineRowLine2(kinds, r, line1Widths), budget)
				}
				return ui.TruncateString(dashboardTwoLineRowLine1(r, line1Widths, cache), budget)
			}
			return ui.TruncateString(dashboardTableLine(page.styledCells(kinds, r, cache), cols.widths), budget)
		},
	})
	return QueueDashboard{d: d, cfg: cfg, page: page, kinds: kinds, snap: snap, allRows: snap.Containers, list: list, cols: cols, live: live}
}

// dashboardChromeLines returns the chrome height above the List rows for the
// current render mode.
func (m QueueDashboard) dashboardChromeLines() int {
	if m.page.twoLine(m.snap.Containers, m.width, m.height) {
		return dashboardTwoLineChromeLines
	}
	return dashboardTableChromeLines
}

// syncListRows feeds the current filtered rows to the List (re-anchoring the
// cursor by CursorKey) and recomputes the column widths over them.
func (m QueueDashboard) syncListRows() {
	m.list.ReplaceItems(m.snap.Containers)
	m.cols.syncNatural(m.kinds, m.snap.Containers)
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
	if m.page.twoLine(m.snap.Containers, m.width, m.height) {
		m.list.SetLinesPerItem(2)
	} else {
		m.list.SetLinesPerItem(1)
	}
	m.list.Resize(listH)
}

// ViewToggleAllowed reports whether v may switch to the other page. The rule is
// unchanged from when the toggle swapped two TUIs: a modal, menu or detail owns
// the keyboard, so v means whatever that overlay says it means.
func (m QueueDashboard) ViewToggleAllowed() bool {
	return m.bind == nil && m.drainPick == nil && m.abandon == nil &&
		m.detail == nil && m.menu == nil && m.itemMenu == nil && m.filter == nil
}

// ActivePage reports which page this model shows.
func (m QueueDashboard) ActivePage() Page {
	return m.page.id
}

// OtherPage is the page `v` switches to from here.
func (m QueueDashboard) OtherPage() Page {
	return m.page.other()
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
	return tea.Batch(dashboardTick(m.page.id), m.primeLiveCache())
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
			m.list.SetCursor(len(m.snap.Containers) - 1)
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
			if len(dashboardMenuItems(m.kinds, row)) == 0 {
				return m, nil
			}
			m.menu = newDashboardMenu(m.kinds, row, msg.String() == "A")
			m.err = nil
			m.statusMsg = ""
			return m, nil
		case "f":
			// Open the row-inclusion filter menu (ADR-0121). Unlike `/` (a transient
			// fuzzy query over the already-included rows) this modal flips which rows
			// are included at all; the two are independent concepts.
			if !m.page.rowFilters {
				return m, nil
			}
			m.filter = newDashboardFilterMenu()
			m.err = nil
			m.statusMsg = ""
			return m, nil
		case "I":
			// The wayfinding shortcut is anchored to the Map kind rather than to the
			// verb being offered: a Map with an empty frontier must still answer with
			// the "no frontier tickets" report rather than a dead key.
			row, ok := m.list.Selected()
			if !ok || !mapRow(row) {
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
			return m, nil
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
		if msg.page != m.page.id {
			// The other page's poll, arriving while this one is in focus: dropping it
			// retires that page's tick chain, which its own Init restarts when the
			// operator switches back.
			return m, nil
		}
		return m, tea.Batch(dashboardTick(m.page.id), m.reload())
	case ui.SpinnerTickMsg:
		return m, nil
	case dashboardLivePrimeMsg:
		if m.live == nil {
			m.live = &livePaneCache{}
		}
		*m.live = msg.live
		return m, nil
	case dashboardRowsMsg:
		if msg.page != m.page.id {
			return m, nil
		}
		m.err = msg.err
		if msg.err == nil {
			m.allRows = msg.snap.Containers
			m.snap = msg.snap
			if m.live == nil {
				m.live = &livePaneCache{}
			}
			*m.live = msg.live
			if m.filterMode {
				m.snap.Containers = filterDashboardRows(m.allRows, m.filterInput.Value())
			}
			m.syncListRows()
			if m.detail != nil {
				// The detail view reads the container the table just rebuilt: one
				// data path, so an item status that moved on disk shows up in the
				// detail and the row it was opened from at the same tick.
				for _, row := range m.snap.Containers {
					if row.CursorKey == m.detail.row.CursorKey {
						m.detail.sync(row)
						break
					}
				}
			}
			if m.menu != nil {
				if m.menu.pinned {
					if row, ok := m.list.Selected(); ok {
						m.menu.row = row
						m.menu.list = ui.NewList(dashboardMenuItems(m.kinds, row), ui.Opts[dashboardMenuItem]{Wrap: true})
					}
				} else {
					for _, row := range m.snap.Containers {
						if row.CursorKey == m.menu.row.CursorKey {
							m.menu.row = row
							break
						}
					}
				}
			}
		}
	case dashboardToggleMsg:
		if msg.err != nil {
			m.actionErr = msg.err
			return m, m.reload()
		}
		for i := range m.snap.Containers {
			if m.snap.Containers[i].CursorKey == msg.key {
				m.snap.Containers[i].AutoDrain = msg.autoDrain
				break
			}
		}
		m.cols.syncNatural(m.kinds, m.snap.Containers)
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
	case dashboardDrainListMsg:
		if msg.err != nil {
			m.actionErr = msg.err
			return m, nil
		}
		if len(msg.entries) == 0 {
			m.actionErr = fmt.Errorf("no drain target available for %s", msg.row.ID)
			return m, nil
		}
		m.drainPick = newDashboardDrainModal(msg.row, msg.entries)
		return m, nil
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
		return m, m.reload()
	case dashboardKindVerbMsg:
		return m.applyKindVerb(msg)
	case dashboardItemTextMsg:
		if m.detail == nil || m.detail.peek == nil {
			return m, nil
		}
		m.detail.peek.loading = false
		m.detail.peek.itemID = msg.itemID
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
// matching verb letter runs that verb directly. Navigation is resolved before
// verb letters so a hotkey can never shadow movement.
func (m QueueDashboard) updateStatusMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.menu == nil || m.menu.status == nil {
		return m, nil
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
	for i, item := range m.menu.status.list.Items() {
		if msg.String() == item.Key {
			return m.invokeStatusMenuItem(i)
		}
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
	if item.verb == work.VerbStatus {
		m.menu.status = newDashboardStatusMenu(m.kinds, row)
		return m, nil
	}
	pinned := m.menu.pinned && !dashboardMenuItemHandoff(item)
	if !pinned {
		m.menu = nil
	}
	return m.dispatchVerb(item.verb, row)
}

// invokeStatusMenuItem closes the submenu and dispatches the status verb at idx
// down the one path every row verb takes — no status-specific dispatch of its own
// (ADR-0186). A status write is in-place, so a pinned menu survives it.
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
	return m.dispatchVerb(item.Verb, row)
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
// drain.Deps (m.d.IncludeDone) and returns a reload: BuildDashboard re-derives the
// rows honoring the new flag and re-sorts them (ADR-0121), and the reload's
// dashboardRowsMsg re-applies any active `/` fuzzy query, so the two filters
// stay independent. The flag is session-only — a fresh drain.Deps on relaunch resets
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
	case filterToggleShowArchived:
		m.d.IncludeArchived = !m.d.IncludeArchived
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
	case filterToggleShowArchived:
		return m.d != nil && m.d.IncludeArchived
	}
	return false
}

// dispatchVerb runs the verb the menu (or a flat shortcut) selected, keyed by
// verb id and never by kind. The Task-set verbs that drive a modal the dashboard
// owns — the drain picker, the bind picker, the unbind confirm — plus the ones
// that spawn through queue's own launchers stay here by decision (ADR-0173):
// moving them behind Kind.Perform needs a modal-capable Outcome and is deferred.
// The conditional guards mirror the kind's own eligibility filtering — an item
// present in the menu always passes its guard, but the guards keep dispatch
// self-contained.
func (m QueueDashboard) dispatchVerb(verb work.Verb, row DashboardRow) (tea.Model, tea.Cmd) {
	m.err = nil
	switch verb {
	case setkind.VerbDrain:
		m.statusMsg = dashboardHandoffPending
		return m, m.launchDrain(row)
	case setkind.VerbVerify:
		if !dashboardVerifyEligible(row) {
			return m, nil
		}
		m.statusMsg = dashboardHandoffPending
		return m, m.launchVerify(row)
	case setkind.VerbBind:
		m.bind = &dashboardBindModal{row: row, loading: true}
		return m, m.loadBindWorktrees(row)
	case setkind.VerbUnbind:
		if !row.Bound {
			return m, nil
		}
		m.abandon = &dashboardAbandonModal{row: row}
		return m, nil
	case setkind.VerbAutoDrain:
		if row.Orphaned {
			return m, nil
		}
		for i := range m.snap.Containers {
			if m.snap.Containers[i].CursorKey == row.CursorKey {
				m.snap.Containers[i].AutoDrain = !m.snap.Containers[i].AutoDrain
				break
			}
		}
		m.cols.syncNatural(m.kinds, m.snap.Containers)
		return m, m.ToggleSetAutoDrain(row)
	case setkind.VerbAssist:
		m.statusMsg = dashboardHandoffPending
		return m, m.launchAssist(row)
	case setkind.VerbFold:
		if !dashboardFoldEligible(row) {
			return m, nil
		}
		if err := drain.PreflightFold(m.d, m.cfg, row); err != nil {
			m.actionErr = err
			return m, nil
		}
		m.statusMsg = dashboardHandoffPending
		return m, m.launchFold(row)
	case setkind.VerbUnpark:
		if !row.Parked {
			m.statusMsg = "task set is not parked"
			return m, nil
		}
		m.statusMsg = ""
		return m, m.unparkSet(row)
	case wayfinder.VerbWork:
		m.statusMsg = dashboardHandoffPending
		return m, m.launchWayfinderSession(row, "")
	case wayfinder.VerbWorkHere:
		m.statusMsg = dashboardSpawnPending
		return m, m.spawnWayfinderSession(row, "")
	case wayfinder.VerbFanOut:
		m.statusMsg = dashboardHandoffPending
		return m, m.launchWayfinderFanOut(row)
	case wayfinder.VerbFanOutHere:
		m.statusMsg = dashboardSpawnPending
		return m, m.spawnWayfinderFanOut(row)
	case wayfinder.VerbAssist:
		m.statusMsg = dashboardHandoffPending
		return m, m.launchWayfinderAssist(row)
	case work.VerbShell:
		// The directory is the kind's answer, not the dashboard's: a task set opens
		// in its bound checkout, a Map in its repository, and a kind that resolves
		// none says so by leaving it blank. A row from a builder that predates the
		// seam carries only the Task-set binding, which is that kind's answer too.
		dir := strings.TrimSpace(row.Checkout)
		if dir == "" {
			dir = strings.TrimSpace(row.RuntimePath)
		}
		if dir == "" {
			m.statusMsg = "no checkout bound to this task set"
			return m, nil
		}
		m.statusMsg = dashboardHandoffPending
		return m, m.launchShell(row, dir)
	case work.VerbCopyName:
		m.statusMsg = m.copyRowName(row)
		return m, nil
	}
	// Every other verb is the kind's own to run: it performs it and the dashboard
	// carries out the outcome, so a kind whose verbs need no dashboard-owned modal
	// needs no case here (ADR-0173).
	m.statusMsg = ""
	return m, m.performKindVerb(row, verb)
}

func (m QueueDashboard) updateDetailView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.itemMenu != nil {
		m.pendingG = false
		return m.updateItemMenu(msg)
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
			m.moveItemTextPeek(1)
		case "k", "up":
			m.moveItemTextPeek(-1)
		case "ctrl+d":
			m.moveItemTextPeek(halfPageDelta(m.itemTextPeekPageSize()))
		case "ctrl+u":
			m.moveItemTextPeek(-halfPageDelta(m.itemTextPeekPageSize()))
		case "G":
			m.detail.peek.scroll = m.maxItemTextPeekScroll()
		case "a":
			item, ok := m.detail.itemByID(m.detail.peek.itemID)
			if !ok {
				return m, nil
			}
			actions := m.kinds.itemActionsFor(m.detail.row, item)
			if len(actions) == 0 {
				return m, nil
			}
			m.itemMenu = newItemMenu(item, actions, true)
		case "y":
			item, ok := m.detail.itemByID(m.detail.peek.itemID)
			if !ok {
				return m, nil
			}
			m.detail.peek.statusMsg = m.copyItemName(m.detail.row, item)
			return m, nil
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
	case "ctrl+g":
		// Open the detail container's checkout in pop, mirroring the main-list
		// Ctrl-g: surface the checkout on quit so the command layer runs the
		// workbench-aware open after the TUI exits; a container whose kind resolves
		// none shows an inline status and keeps the dashboard running.
		if m.detail == nil {
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
			m.detail.list.MoveDown()
		}
	case "k", "up":
		if m.detail != nil {
			m.detail.list.MoveUp()
		}
	case "G":
		if m.detail != nil {
			m.detail.list.SetCursor(len(m.detail.row.Items) - 1)
		}
	case "l", "enter":
		if m.detail == nil {
			return m, nil
		}
		item, ok := m.detail.list.Selected()
		if !ok {
			return m, nil
		}
		m.detail.peek = &itemTextPeek{itemID: item.ID, loading: true}
		return m, m.loadItemText(item)
	case "a":
		if m.detail == nil {
			return m, nil
		}
		item, ok := m.detail.list.Selected()
		if !ok {
			return m, nil
		}
		actions := m.kinds.itemActionsFor(m.detail.row, item)
		if len(actions) == 0 {
			return m, nil
		}
		m.detail.statusMsg = ""
		m.itemMenu = newItemMenu(item, actions, false)
		return m, nil
	case "y":
		if m.detail == nil {
			return m, nil
		}
		item, ok := m.detail.list.Selected()
		if !ok {
			return m, nil
		}
		m.detail.statusMsg = m.copyItemName(m.detail.row, item)
		return m, nil
	}
	return m, nil
}

// updateItemMenu drives the item-level action overlay: esc/ctrl+c close it, j/k
// move the highlight, Enter runs the highlighted verb, and any matching verb
// letter runs that verb directly. Non-matching keys are inert while open.
// Navigation is resolved before verb letters so a hotkey can never shadow
// movement.
func (m QueueDashboard) updateItemMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.itemMenu == nil {
		return m, nil
	}
	switch msg.String() {
	case "esc", "ctrl+c":
		m.itemMenu = nil
		return m, nil
	case "j", "down":
		m.itemMenu.list.MoveDown()
		return m, nil
	case "k", "up":
		m.itemMenu.list.MoveUp()
		return m, nil
	case "enter":
		return m.invokeItemMenuItem(m.itemMenu.list.Cursor())
	}
	for i, action := range m.itemMenu.list.Items() {
		if msg.String() == action.Key {
			return m.invokeItemMenuItem(i)
		}
	}
	return m, nil
}

// invokeItemMenuItem closes the menu and dispatches the verb at idx against the
// item the menu was opened on. The kind pre-filtered the verbs to the ones that
// apply, so the verb runs without a separate confirmation.
func (m QueueDashboard) invokeItemMenuItem(idx int) (tea.Model, tea.Cmd) {
	if m.itemMenu == nil {
		return m, nil
	}
	actions := m.itemMenu.list.Items()
	if idx < 0 || idx >= len(actions) {
		return m, nil
	}
	if m.detail == nil {
		m.itemMenu = nil
		return m, nil
	}
	verb := actions[idx].Verb
	item := m.itemMenu.item
	inPeek := m.itemMenu.inPeek
	m.itemMenu = nil
	return m.dispatchItemVerb(verb, m.detail.row, item, inPeek)
}

// dispatchItemVerb runs one item verb, keyed by verb id and never by kind — the
// same split the container-level dispatch keeps (ADR-0173). Copy-name is the
// kind's own answer, taken from its Perform; the Task-set status writes and the
// Map's grilling handoff run through queue's existing launchers because they are
// the ones that own the process handoff and the detail's refresh. Anything else is
// performed by the kind and its outcome applied, exactly as for a row verb.
func (m QueueDashboard) dispatchItemVerb(verb work.Verb, row work.Container, item work.Item, inPeek bool) (tea.Model, tea.Cmd) {
	switch verb {
	case work.VerbCopyName:
		msg := m.copyItemName(row, item)
		if inPeek && m.detail.peek != nil {
			m.detail.peek.statusMsg = msg
		} else {
			m.detail.statusMsg = msg
		}
		return m, nil
	case wayfinder.VerbWork:
		m.detail.statusMsg = ""
		return m, m.launchWayfinderSession(row, item.ID)
	case wayfinder.VerbWorkHere:
		m.detail.statusMsg = dashboardSpawnPending
		return m, m.spawnWayfinderSession(row, item.ID)
	case setkind.VerbComplete, setkind.VerbOpen, setkind.VerbSkip:
		m.detail.statusMsg = ""
		return m, m.applyDetailOverride(row, item, verb)
	}
	m.detail.statusMsg = ""
	return m, m.performKindItemVerb(row, item, inPeek, verb)
}

// copyItemName copies the item reference the owning kind names — a task set's
// paste-ready `<set>/<file>.md`, a Map's bare ticket id — and returns the
// transient confirmation.
func (m QueueDashboard) copyItemName(row work.Container, item work.Item) string {
	payload, err := m.kinds.itemCopyPayload(row, item)
	if err != nil {
		return fmt.Sprintf("copy failed: %v", err)
	}
	return m.copyClipboard(payload)
}

// applyDetailOverride dispatches the complete/open/skip verb to the appropriate
// tasks.*With function via the drain.Deps seam.
func (m QueueDashboard) applyDetailOverride(row work.Container, item work.Item, verb work.Verb) tea.Cmd {
	d := m.d
	if d == nil {
		d = drain.DefaultDeps()
	}
	taskPath := setkind.TaskRef(row.ID, item)
	return func() tea.Msg {
		var err error
		switch verb {
		case setkind.VerbComplete:
			err = d.CompleteTaskFile(row.DefPath, taskPath)
		case setkind.VerbOpen:
			err = d.ResetTaskFile(row.DefPath, taskPath)
		case setkind.VerbSkip:
			err = d.SkipTaskFile(row.DefPath, taskPath)
		}
		return dashboardDetailOverrideMsg{taskID: item.ID, verb: string(verb), err: err}
	}
}

func (m QueueDashboard) updateFilterMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.filterMode = false
		m.filterInput = ui.TextField{}
		m.snap.Containers = m.allRows
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
		m.snap.Containers = filterDashboardRows(m.allRows, m.filterInput.Value())
		m.syncListRows()
		return m, nil
	}
}

func (m QueueDashboard) moveItemTextPeek(delta int) {
	if m.detail == nil || m.detail.peek == nil || delta == 0 {
		return
	}
	m.detail.peek.scroll += delta
	if m.detail.peek.scroll < 0 {
		m.detail.peek.scroll = 0
	}
	if maxScroll := m.maxItemTextPeekScroll(); m.detail.peek.scroll > maxScroll {
		m.detail.peek.scroll = maxScroll
	}
}

func (m QueueDashboard) maxItemTextPeekScroll() int {
	if m.detail == nil || m.detail.peek == nil {
		return 0
	}
	lines := itemTextPeekLines(m.detail.peek.text)
	maxScroll := len(lines) - m.itemTextPeekPageSize()
	if maxScroll < 0 {
		return 0
	}
	return maxScroll
}

func (m QueueDashboard) itemTextPeekPageSize() int {
	if m.detail == nil || m.detail.peek == nil {
		return 1
	}
	return itemTextPeekPageSize(m.height, m.detail.peek.path)
}

// filterDashboardRows returns rows whose Project or id contain query
// (case-insensitive). Returns allRows unchanged when query is empty.
func filterDashboardRows(rows []DashboardRow, query string) []DashboardRow {
	if query == "" {
		return rows
	}
	q := strings.ToLower(query)
	var filtered []DashboardRow
	for _, row := range rows {
		if strings.Contains(strings.ToLower(row.Project), q) ||
			strings.Contains(strings.ToLower(row.ID), q) {
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

// reload rebuilds this page's snapshot: only its own kinds, so the poll never
// pays for the other page's scan and the header it recomputes counts only what is
// on screen.
func (m QueueDashboard) reload() tea.Cmd {
	return func() tea.Msg {
		snap, err := work.BuildSnapshot(m.page.kinds(m.d, m.cfg))
		live := loadLivePaneCache(m.d)
		return dashboardRowsMsg{page: m.page.id, snap: snap, live: live, err: err}
	}
}

// unparkSet handles the `P` key: it writes a durable park-clear event for the
// selected parked set so the daemon may auto-spawn it again (ADR-0055).
func (m QueueDashboard) unparkSet(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		err := drain.UnparkSet(m.d, row)
		return dashboardUnparkMsg{setID: row.ID, err: err}
	}
}

func (m QueueDashboard) ToggleSetAutoDrain(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		result, err := m.d.ToggleSetAutoDrain(row.DefPath, row.StatePath, row.ID)
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
		bound, err := dashboardSetBound(m.d, m.cfg, row)
		if err != nil {
			return dashboardDrainListMsg{row: row, err: err}
		}
		if bound {
			result, err := drain.LaunchDrain(m.d, m.cfg, row)
			return handoffAfterLaunch(m.d, result, err)
		}
		entries, err := drain.DrainTargetEntries(m.d, m.cfg, row)
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
func (m QueueDashboard) launchDrainTarget(row DashboardRow, target drain.DrainEntry) tea.Cmd {
	return func() tea.Msg {
		result, err := drain.LaunchDrainTarget(m.d, m.cfg, row, target)
		return handoffAfterLaunch(m.d, result, err)
	}
}

// defaultDrainCursor positions the picker on "new managed worktree" — the
// frictionless default that provisions an isolated checkout. It falls back to the
// first entry when no trunk is resolvable (the option is absent).
func defaultDrainCursor(entries []drain.DrainEntry) int {
	for i, e := range entries {
		if e.Kind == drain.DrainTargetNewManaged {
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
		result, err := drain.LaunchVerify(m.d, m.cfg, row)
		return handoffAfterLaunch(m.d, result, err)
	}
}

func (m QueueDashboard) launchWayfinderSession(row DashboardRow, ticketID string) tea.Cmd {
	return func() tea.Msg {
		result, err := LaunchWayfinderSession(m.d, m.cfg, row, ticketID)
		return handoffAfterLaunch(m.d, result, err)
	}
}

// spawnWayfinderSession is launchWayfinderSession without the move: the same
// spawn, reported as a status line so the operator stays on the dashboard
// (ADR-0182's lowercase half).
func (m QueueDashboard) spawnWayfinderSession(row DashboardRow, ticketID string) tea.Cmd {
	return func() tea.Msg {
		result, err := LaunchWayfinderSession(m.d, m.cfg, row, ticketID)
		if err != nil {
			return dashboardHandoffMsg{err: err}
		}
		return dashboardHandoffMsg{status: fmt.Sprintf("grilling pane spawned in %s", result.Session)}
	}
}

func (m QueueDashboard) launchWayfinderFanOut(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		result, _, err := LaunchWayfinderFanOut(m.d, m.cfg, row)
		return handoffAfterLaunch(m.d, result, err)
	}
}

func (m QueueDashboard) spawnWayfinderFanOut(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		result, count, err := LaunchWayfinderFanOut(m.d, m.cfg, row)
		if err != nil {
			return dashboardHandoffMsg{err: err}
		}
		return dashboardHandoffMsg{status: fmt.Sprintf("fanned out %d frontier tickets into %s", count, result.Session)}
	}
}

// launchWayfinderAssist opens the Map-scoped assist pane and hands off to it. It
// has no staying twin: an assist session is one pane you asked for in order to go
// talk to it (ADR-0184).
func (m QueueDashboard) launchWayfinderAssist(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		result, err := LaunchWayfinderAssist(m.d, m.cfg, row)
		return handoffAfterLaunch(m.d, result, err)
	}
}

func dashboardWayfinderEmptyFrontierMessage() string {
	return "no frontier tickets — open tickets are blocked or claimed"
}

func (m QueueDashboard) launchAssist(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		result, err := drain.LaunchAssist(m.d, m.cfg, row)
		return handoffAfterLaunch(m.d, result, err)
	}
}

func (m QueueDashboard) launchFold(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		result, err := drain.LaunchFold(m.d, m.cfg, row)
		return handoffAfterLaunch(m.d, result, err)
	}
}

func (m QueueDashboard) launchShell(row DashboardRow, dir string) tea.Cmd {
	return func() tea.Msg {
		result, err := drain.LaunchShellIn(m.d, m.cfg, row, dir)
		return handoffAfterLaunch(m.d, result, err)
	}
}

// handoffAfterLaunch is the single post-spawn path for drain, verify, fold,
// assist, shell, and wayfinder (ADR-0158): focus the pane when inside tmux and
// signal quit, or stay open with a status line explaining why focus was
// unavailable / nothing moved.
func handoffAfterLaunch(d *drain.Deps, result drain.DashboardDrainResult, err error) dashboardHandoffMsg {
	if err != nil {
		return dashboardHandoffMsg{err: err}
	}
	if strings.TrimSpace(result.PaneID) == "" {
		return dashboardHandoffMsg{status: "nothing to hand off to"}
	}
	if d == nil {
		d = drain.DefaultDeps()
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
	recordHandoffLanding(d, result)
	return dashboardHandoffMsg{quit: true}
}

// recordHandoffLanding writes the checkout tmux has just moved the operator into
// into History (ADR-0188). It sits behind the focus, so it records only the
// handoffs that actually went somewhere, and it covers every verb at once because
// they all end here — a manually launched drain, verify or fold included: the line
// History draws is manual versus daemon, not human work versus machine work.
//
// The launcher's own answer for where the pane lives comes first — a task set's
// binding-resolved runtime path, a Map's Trunk worktree — and a launcher that
// named none is one whose pane is all it has to offer (a Routine's refinement or
// prompt-edit window), so the pane's own working directory is the landing.
//
// Best-effort throughout, and silent when no store is wired: a Deps built as a
// bare literal must not reach for the machine-global store to record a test's
// handoff.
func recordHandoffLanding(d *drain.Deps, result drain.DashboardDrainResult) {
	if d == nil || d.Tasks == nil {
		return
	}
	path := strings.TrimSpace(result.RuntimePath)
	if path == "" {
		paneDir, err := d.Tmux.PaneCurrentPath(result.PaneID)
		if err != nil {
			debug.Error("dashboard: pane directory for history: %v", err)
			return
		}
		path = strings.TrimSpace(paneDir)
	}
	if path == "" {
		return
	}
	hd := &history.Deps{FS: d.Tasks.FS, Tmux: d.Tmux, Tasks: d.Tasks}
	if err := history.RecordWith(hd, path); err != nil {
		debug.Error("dashboard: record history: %v", err)
	}
}

// dashboardRowStorageDir derives a container's Task-storage directory from the
// definition path its repository group carried.
func dashboardRowStorageDir(row DashboardRow) string {
	if row.DefPath != "" {
		return filepath.Dir(row.DefPath)
	}
	return ""
}

// loadItemText reads one Work item's text for the peek. The path is the item's
// own — every kind resolves it when it builds the item, so the peek needs no
// directory of the kind's to join against.
func (m QueueDashboard) loadItemText(item work.Item) tea.Cmd {
	return func() tea.Msg {
		if strings.TrimSpace(item.File) == "" {
			return dashboardItemTextMsg{itemID: item.ID, err: fmt.Errorf("%s has no file to preview", item.ID)}
		}
		d := m.d
		if d == nil {
			d = drain.DefaultDeps()
		}
		if d.Tasks == nil {
			d.Tasks = tasks.DefaultDeps()
		}
		data, err := d.Tasks.FS.ReadFile(item.File)
		if err != nil {
			return dashboardItemTextMsg{itemID: item.ID, path: item.File, err: err}
		}
		return dashboardItemTextMsg{itemID: item.ID, path: item.File, text: string(data)}
	}
}

func (m QueueDashboard) loadBindWorktrees(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		entries, err := drain.BindWorktreeEntries(m.d, m.cfg, row)
		return dashboardBindListMsg{row: row, entries: entries, err: err}
	}
}

func (m QueueDashboard) loadBindRefs(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		refs, err := drain.BindBaseRefs(m.d, m.cfg, row)
		return dashboardBindRefsMsg{refs: refs, err: err}
	}
}

func (m QueueDashboard) adoptBindWorktree(row DashboardRow, checkoutPath string) tea.Cmd {
	return func() tea.Msg {
		_, err := drain.AdoptWorktree(m.d, m.cfg, row, checkoutPath)
		return dashboardBindMsg{err: err}
	}
}

func (m QueueDashboard) bindManagedWorktree(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		_, err := drain.BindManagedWorktree(m.d, m.cfg, row)
		return dashboardBindMsg{err: err}
	}
}

func (m QueueDashboard) createBindWorktree(row DashboardRow, baseRef, name string) tea.Cmd {
	return func() tea.Msg {
		_, err := drain.CreateWorktree(m.d, m.cfg, row, baseRef, name)
		return dashboardBindMsg{err: err}
	}
}

func (m QueueDashboard) abandonWorktree(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		_, err := drain.UnbindWorktree(m.d, m.cfg, row)
		return dashboardAbandonMsg{err: err}
	}
}

// dashboardSetBound reports whether the row's set already holds a Worktree
// binding. The Drain target picker only opens for unbound sets; a bound set
// resumes in its binding (ADR-0052).
func dashboardSetBound(d *drain.Deps, cfg *config.Config, row DashboardRow) (bool, error) {
	d = drain.EnsureDeps(d)
	repoKey := row.RepoKey
	if repoKey == "" {
		rk, err := drain.RepoKeyForRow(d, cfg, row)
		if err != nil {
			return false, err
		}
		repoKey = rk
	}
	b, ok := drain.BindingForSet(d.Tasks, repoKey, row.ID)
	return ok && strings.TrimSpace(b.RuntimePath) != "", nil
}

func dashboardTick(page Page) tea.Cmd {
	return tea.Tick(dashboardPollInterval, func(time.Time) tea.Msg { return dashboardTickMsg{page: page} })
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
	case m.itemMenu != nil:
		// Item-level action menu (in detail or peek). Its verbs are the owning
		// kind's, so the help lists the menu that is actually open rather than one
		// kind's vocabulary written out here.
		entries := make([]ui.HelpEntry, 0, len(m.itemMenu.list.Items())+3)
		for _, action := range m.itemMenu.list.Items() {
			entries = append(entries, ui.HelpEntry{Key: action.Key, Desc: action.Label})
		}
		return append(entries,
			ui.HelpEntry{Key: "j/k", Desc: "navigate"},
			ui.HelpEntry{Key: "enter", Desc: "run action"},
			ui.HelpEntry{Key: "esc", Desc: "close menu"},
		)
	case m.menu != nil && m.menu.status != nil:
		// The status submenu's verbs are the focused row's own kind's, so the help
		// lists the submenu that is actually open rather than one kind's vocabulary
		// written out here — a Map row's keys would otherwise be a lie.
		items := m.menu.status.list.Items()
		entries := make([]ui.HelpEntry, 0, len(items)+3)
		for _, action := range items {
			entries = append(entries, ui.HelpEntry{Key: action.Key, Desc: action.Label})
		}
		return append(entries,
			ui.HelpEntry{Key: "j/k", Desc: "navigate"},
			ui.HelpEntry{Key: "enter", Desc: "run action"},
			ui.HelpEntry{Key: "esc", Desc: "back to action menu"},
		)
	case m.menu != nil:
		// Dashboard action menu. Its verbs are the focused row's own kind's, so the
		// help lists the menu that is actually open rather than one kind's vocabulary
		// written out here — a Routine row's keys would otherwise be a lie.
		items := m.menu.items()
		entries := make([]ui.HelpEntry, 0, len(items)+4)
		for _, item := range items {
			entries = append(entries, ui.HelpEntry{Key: item.key, Desc: item.label})
		}
		entries = append(entries,
			ui.HelpEntry{Key: "j/k", Desc: "navigate"},
			ui.HelpEntry{Key: "enter", Desc: "run action"},
			ui.HelpEntry{Key: "esc", Desc: "close menu"},
		)
		if m.menu.pinned {
			entries = append(entries, ui.HelpEntry{Key: "J/K", Desc: "move row cursor"})
		}
		return entries
	case m.filter != nil:
		// Row-inclusion filter menu
		return []ui.HelpEntry{
			{Key: "d", Desc: "toggle show done"},
			{Key: "a", Desc: "toggle show archived"},
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
		entries = append(entries, ui.HelpEntry{Key: "a", Desc: "item actions"})
		return entries
	case m.detail != nil:
		// Detail view (one container's items, whatever kind it is)
		return []ui.HelpEntry{
			{Key: "j/k", Desc: "navigate items"},
			{Key: "gg", Desc: "first item"},
			{Key: "G", Desc: "last item"},
			{Key: "l/enter", Desc: "peek item text"},
			{Key: "a", Desc: "item actions"},
			{Key: "y", Desc: "copy name"},
			{Key: "ctrl+g", Desc: "open worktree"},
			{Key: "h/esc", Desc: "back to list"},
		}
	case m.filterMode:
		// Filter mode
		return []ui.HelpEntry{
			{Key: "typing", Desc: "filter rows"},
			{Key: "j/k", Desc: "navigate filtered"},
			{Key: "v", Desc: m.page.toggleWord + " view"},
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
		}
		if m.page.rowFilters {
			entries = append(entries, ui.HelpEntry{Key: "f", Desc: "filter menu"})
		}
		entries = append(entries,
			ui.HelpEntry{Key: "v", Desc: m.page.toggleWord + " view"},
			ui.HelpEntry{Key: "h/esc", Desc: "quit"},
		)
		if row, ok := m.list.Selected(); ok && mapRow(row) {
			entries = append(entries, ui.HelpEntry{Key: "I", Desc: "work next frontier ticket"})
		}
		return entries
	}
	return nil
}

func (m QueueDashboard) View() tea.View {
	if m.showHelp {
		// The help overlay names the page it was opened over, so an operator who
		// pressed v cannot mistake one page's key list for the other's.
		page := m.page.title
		title := "Help · " + page
		if m.filterMode {
			title = "Help · " + page + " · filter"
		} else if m.detail != nil && m.detail.peek != nil {
			title = "Help · " + page + " · peek"
		} else if m.detail != nil {
			title = "Help · " + page + " · detail"
		} else if m.menu != nil && m.menu.status != nil {
			title = "Help · " + page + " · status submenu"
		} else if m.menu != nil && m.menu.pinned {
			title = "Help · " + page + " · pinned action menu"
		} else if m.menu != nil {
			title = "Help · " + page + " · action menu"
		} else if m.filter != nil {
			title = "Help · " + page + " · filter menu"
		} else if m.itemMenu != nil {
			title = "Help · " + page + " · item menu"
		} else if m.bind != nil {
			title = "Help · " + page + " · bind"
		} else if m.drainPick != nil {
			title = page + " · drain"
		} else if m.abandon != nil {
			title = page + " · unbind"
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
	if len(m.snap.Containers) > 0 {
		header = m.pageHeader()
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

// pageHeader is the summary line over the rows on screen: this page's noun, then
// each of its kinds' own phrases. The counts are the page's alone — a page never
// reports the other page's containers, so the Routine page's "M here" tally
// cannot be diluted by task sets.
func (m QueueDashboard) pageHeader() string {
	return m.page.title + " · " + dashboardSummary(m.kinds, m.snap.Containers)
}

// mainHint returns the footer hint for the main (non-modal, non-menu) view.
func (m QueueDashboard) mainHint() string {
	toggle := "v " + m.page.toggleWord
	if len(m.snap.Containers) == 0 {
		if m.filterMode {
			return "esc clear filter · " + toggle + " · C-h help"
		}
		return toggle + " · C-h help · h/esc quit"
	}
	if m.filterMode {
		return "esc clear filter · j/k navigate · " + toggle + " · C-h help"
	}
	filters := ""
	if m.page.rowFilters {
		filters = "f filters · "
	}
	return "j/k move · gg/G top/bottom · l/enter status · y copy name · a actions · / filter · " + filters + toggle + " · C-h help · h/esc quit"
}

// mainBody renders the table body (a blank line, the column header, the
// separator, then the List's scroll window) or the empty-state message. It is
// the body the Frame composes its chrome around.
func (m QueueDashboard) mainBody() string {
	if len(m.snap.Containers) == 0 {
		if m.filterMode {
			return m.page.emptyFiltered
		}
		return m.page.empty
	}
	var parts []string
	if m.page.twoLine(m.snap.Containers, m.width, m.height) {
		line1Widths := dashboardTwoLineFitWidths(dashboardTwoLineNaturalWidths(m.snap.Containers), dashboardTableBodyBudget(m.width))
		parts = []string{
			"",
			ui.TruncateString("  "+dashboardTwoLineTableHeader(line1Widths), m.width),
			ui.TruncateString("  "+dashboardTwoLineStatusHeader(line1Widths), m.width),
			ui.TruncateString("  "+dashboardTwoLineTableSeparator(line1Widths), m.width),
		}
	} else {
		headers := m.page.headers(m.kinds)
		parts = []string{
			"",
			ui.TruncateString("  "+dashboardTableLine(headers, m.cols.widths), m.width),
			ui.TruncateString("  "+dashboardTableSeparator(headers, m.cols.widths), m.width),
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
	fmt.Fprintf(&body, "%s\n", m.pageHeader())
	fmt.Fprintln(&body)
	renderDashboardTableWithMenu(&body, m.page, m.kinds, m.snap.Containers, m.list.Cursor(), m.width, m.height, m.menu, m.liveCache())
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
	fmt.Fprintf(&body, "%s\n", m.pageHeader())
	fmt.Fprintln(&body)
	renderDashboardTable(&body, m.page, m.kinds, m.snap.Containers, m.list.Cursor(), m.width, m.height, m.liveCache())
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
	fmt.Fprintf(&body, "%s\n", m.pageHeader())
	fmt.Fprintln(&body)
	renderDashboardTable(&body, m.page, m.kinds, m.snap.Containers, m.list.Cursor(), m.width, m.height, m.liveCache())
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

// dashboardSummary is the header line over the rows currently on screen: each
// kind's own phrases over its own rows, in kind precedence order (ADR-0173). It
// counts the displayed rows rather than reusing the snapshot's phrases so a
// fuzzy filter narrows the header with the table.
func dashboardSummary(kinds workKinds, rows []DashboardRow) string {
	return strings.Join(kinds.summary(rows), " · ")
}

// viewDetail renders the full-screen container detail view. The item text peek
// (ADR-0079) and the item action-menu overlay keep their bespoke rendering; the
// plain state composes through a Frame with the item list on ui.List.
func (m QueueDashboard) viewDetail() string {
	d := m.detail
	if d.peek != nil {
		var b strings.Builder
		renderItemTextPeek(&b, d, m.height, m.width, m.itemMenu)
		return b.String()
	}
	if m.itemMenu != nil {
		var b strings.Builder
		m.renderDetailContent(&b, d, m.height, m.width, m.itemMenu)
		return b.String()
	}
	frame, body := m.detailFrame()
	return frame.Render(body)
}

// detailFrame builds the Frame and body for the non-menu detail states: a
// container that could not be read, and the sections-plus-item-list content. The
// same Frame drives the body-height budget and the render (ADR-0079); the
// content body's List is sized to the budget the Frame leaves minus the prose
// sections and the table's own chrome, so the list clamps to the terminal
// instead of rendering every item.
func (m QueueDashboard) detailFrame() (ui.Frame, string) {
	d := m.detail
	const backHint = "h/esc back"
	header := m.detailHeader(d.row)
	if d.row.Broken {
		body := "  BROKEN"
		if d.row.BrokenReason != "" {
			body += ": " + d.row.BrokenReason
		}
		return ui.Frame{Width: m.width, TermH: m.height, Header: header, Hints: backHint}, body
	}

	frame := ui.Frame{
		Width:  m.width,
		TermH:  m.height,
		Header: header,
		Status: d.statusMsg,
		Hints:  "j/k · gg/G top/bottom · l/enter peek · a actions · y copy name · h/esc back",
	}
	budget := frame.BodyHeight(m.height)
	// The item list is the point of the view, so prose yields to it: sections are
	// cut (with an elision marker) before the list is squeezed below one row.
	sections := clampDetailSections(detailSectionLines(d.row.DetailSections, m.width), budget-detailTableChromeLines-1)
	listH := budget - detailTableChromeLines - len(sections)
	if listH < 1 {
		listH = 1
	}
	d.list.Resize(listH)
	parts := append([]string{}, sections...)
	parts = append(parts,
		"",
		"  "+detailTableHeader(d.cols.idW),
		"  "+detailTableSeparator(d.cols.idW),
	)
	parts = append(parts, d.list.VisibleRows()...)
	return frame, strings.Join(parts, "\n")
}

// detailSectionLines renders the kind-authored prose blocks that sit above the
// item list: each is a blank line, its title, and its body indented under it.
// A kind with nothing to say renders nothing at all, so its detail is the item
// list alone.
func detailSectionLines(sections []work.Section, width int) []string {
	var lines []string
	for _, section := range sections {
		body := strings.TrimRight(section.Body, "\n")
		if strings.TrimSpace(section.Title) == "" && strings.TrimSpace(body) == "" {
			continue
		}
		lines = append(lines, "", "  "+ui.TruncateString(section.Title, width))
		for _, line := range strings.Split(body, "\n") {
			lines = append(lines, ui.TruncateString("    "+line, width))
		}
	}
	return lines
}

// clampDetailSections cuts the rendered section lines to max, marking the cut so
// a reader knows the prose was elided rather than empty.
func clampDetailSections(lines []string, max int) []string {
	if max < 1 {
		return nil
	}
	if len(lines) <= max {
		return lines
	}
	cut := append([]string{}, lines[:max-1]...)
	return append(cut, "  …")
}

// detailHeader is the detail view's title line: the kind's noun, the container
// id, and the STATUS cell the kind composed, plus its headline phrase when it
// wrote one. The status label is left unpainted inside the brackets — the
// brackets already mark it — while an attention badge keeps its colour
// (ADR-0156).
func (m QueueDashboard) detailHeader(row work.Container) string {
	header := fmt.Sprintf("%s · %s  [%s]", detailKindNoun(row.Kind), row.ID, detailStatusStyled(m.kinds, row))
	if row.Headline != "" {
		header += "  " + row.Headline
	}
	return header
}

// detailKindNoun is what a container of each kind is called in a title line. It
// is a display noun and nothing else — no behaviour keys on it — so a kind whose
// noun is missing shows its enum member rather than being unrenderable.
func detailKindNoun(kind work.KindID) string {
	switch kind {
	case ref.KindMap:
		return "Map"
	case ref.KindRoutine:
		return "Routine"
	case ref.KindTaskSet, "":
		return "Task"
	}
	return string(kind)
}

// detailIDWidth returns the ID-column width: the widest item ID, floored at the
// "ID" header label.
func detailIDWidth(items []work.Item) int {
	idW := len("ID")
	for _, item := range items {
		if len(item.ID) > idW {
			idW = len(item.ID)
		}
	}
	return idW
}

// detailItemLine formats one item's cells (status / type / id / title /
// blocked-by) over the fixed and idW-derived widths, without the cursor prefix —
// the List owns the leading indicator column.
func detailItemLine(item work.Item, idW int) string {
	title := item.Title
	if len(title) > detailTitleW {
		title = title[:detailTitleW-3] + "..."
	}
	blockedBy := "-"
	if len(item.BlockedBy) > 0 {
		blockedBy = strings.Join(item.BlockedBy, ", ")
	}
	return fmt.Sprintf("%-*s  %-*s  %-*s  %-*s  %s",
		detailStatusW, item.DisplayStatus(), detailTypeW, item.Type, idW, item.ID, detailTitleW, title, blockedBy)
}

// detailTableHeader is the detail item-table column header, idW-aligned to match
// detailItemLine.
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

// renderDetailContent renders the detail item list with the action-menu overlay
// spliced next to the cursored item (ADR-0079 bespoke placement; its cursor is
// ported onto List in a later slice). It renders every item — no scroll window —
// and reads the cursor from the List. The non-menu state renders via detailFrame.
func (m QueueDashboard) renderDetailContent(b *strings.Builder, d *detailView, height, width int, menu *itemMenu) {
	fmt.Fprintln(b, m.detailHeader(d.row))

	if d.row.Broken {
		body := "  BROKEN"
		if d.row.BrokenReason != "" {
			body += ": " + d.row.BrokenReason
		}
		fmt.Fprintln(b, body)
		writeDashboardFooter(b, height, ui.HintStyle.Render("  h/esc back"))
		return
	}

	for _, line := range detailSectionLines(d.row.DetailSections, width) {
		fmt.Fprintln(b, line)
	}
	fmt.Fprintln(b)

	idW := d.cols.idW
	fmt.Fprintf(b, "  %s\n", detailTableHeader(idW))
	fmt.Fprintf(b, "  %s\n", detailTableSeparator(idW))

	cursorIdx := d.list.Cursor()
	var menuLines []string
	placeBelow := true
	if menu != nil && !menu.inPeek {
		menuLines = itemMenuLines(menu, width)
		placeBelow = dashboardMenuPlaceBelow(cursorIdx, len(menuLines), height)
	}
	writeMenu := func() {
		for _, ml := range menuLines {
			fmt.Fprintf(b, "%s\n", ml)
		}
	}
	for i, item := range d.row.Items {
		if menuLines != nil && i == cursorIdx && !placeBelow {
			writeMenu()
		}
		prefix := "  "
		if i == cursorIdx {
			prefix = ui.IndicatorStyle.Render("█") + " "
		}
		fmt.Fprintf(b, "%s%s\n", prefix, detailItemLine(item, idW))
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

// itemMenuLines renders the item-level action overlay as a block of lines,
// indented to nest under the cursored item, with the highlighted verb carrying
// the shared cursor block. The first line is a dimmed "actions" caption. It
// mirrors dashboardMenuLines (the container-view overlay) for a consistent look.
func itemMenuLines(menu *itemMenu, width int) []string {
	if menu == nil {
		return nil
	}
	lines := []string{ui.TruncateString("    "+ui.HintStyle.Render("actions"), width)}
	cursor := menu.list.Cursor()
	for i, action := range menu.list.Items() {
		marker := "  "
		if i == cursor {
			marker = ui.IndicatorStyle.Render("█") + " "
		}
		line := fmt.Sprintf("    %s%s  %s", marker, action.Key, action.Label)
		lines = append(lines, ui.TruncateString(line, width))
	}
	return lines
}

func renderItemTextPeek(b *strings.Builder, d *detailView, height, width int, menu *itemMenu) {
	p := d.peek
	header := d.row.ID
	if p.itemID != "" {
		header += " / " + p.itemID
	}
	fmt.Fprintln(b, header)
	if menu != nil && menu.inPeek {
		for _, ml := range itemMenuLines(menu, width) {
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
	lines := itemTextPeekLines(p.text)
	pageSize := itemTextPeekPageSize(height, p.path)
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

func itemTextPeekLines(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func itemTextPeekPageSize(height int, path string) int {
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
	fmt.Fprintln(w, ui.TruncateString(fmt.Sprintf("Drain target for %s", modal.row.ID), width))
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
	fmt.Fprintln(w, ui.TruncateString(fmt.Sprintf("Unbind worktree for %s", modal.row.ID), width))
	if modal.loading {
		fmt.Fprintln(w, ui.TruncateString("  unbinding...", width))
		return
	}
	fmt.Fprintln(w, ui.TruncateString("This releases the binding without integrating. Task statuses are unchanged.", width))
	fmt.Fprint(w, ui.HintStyle.Render(ui.TruncateString("y confirm · enter/n/esc cancel", width)))
}

func renderDashboardTable(w io.Writer, page dashboardPage, kinds workKinds, rows []DashboardRow, cursor, width, height int, live livePaneCache) {
	renderDashboardTableWithMenu(w, page, kinds, rows, cursor, width, height, nil, live)
}

// renderDashboardTableWithMenu renders the task-set table and, when menu is
// non-nil, splices the action overlay in next to the cursored row: below it by
// default, flipping above when the cursor sits too low for the menu to fit
// beneath it within height (dashboardMenuPlaceBelow). live colours handoff-verb
// keys in the overlay (ADR-0158).
func renderDashboardTableWithMenu(w io.Writer, page dashboardPage, kinds workKinds, rows []DashboardRow, cursor, width, height int, menu *dashboardMenu, live livePaneCache) {
	if page.twoLine(rows, width, height) {
		renderDashboardTableTwoLineWithMenu(w, kinds, rows, cursor, width, height, menu, live)
		return
	}
	headers := page.headers(kinds)
	widths := page.tableWidthsForRows(kinds, rows, width)
	fmt.Fprintf(w, "%s\n", ui.TruncateString("  "+dashboardTableLine(headers, widths), width))
	fmt.Fprintf(w, "%s\n", ui.TruncateString("  "+dashboardTableSeparator(headers, widths), width))

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
		line := ui.TruncateString(prefix+dashboardTableLine(page.styledCells(kinds, row, live), widths), width)
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
func renderDashboardTableTwoLineWithMenu(w io.Writer, kinds workKinds, rows []DashboardRow, cursor, width, height int, menu *dashboardMenu, live livePaneCache) {
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
		line2 := ui.TruncateString("  "+dashboardTwoLineRowLine2(kinds, row, line1Widths), width)
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
		line := fmt.Sprintf("    %s%s  %s", marker, item.Key, item.Label)
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
func RunDashboard(d *drain.Deps, cfg *config.Config) (string, error) {
	snap, err := work.BuildSnapshot(d.WorkKinds(cfg))
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
