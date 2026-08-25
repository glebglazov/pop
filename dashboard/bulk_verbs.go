package dashboard

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/tasks/setkind"
	"github.com/glebglazov/pop/ui"
	"github.com/glebglazov/pop/wayfinder"
	"github.com/glebglazov/pop/work"
)

// The Work dashboard's plural half (ADR-0215 decisions 5, 6 and 7): what a verb
// means when it is invoked over a Selection rather than over the cursored row.
//
// Three rules hold the whole of it. A verb reaches the plural menus only if its
// own kind declared it fit (work.Action.Modes) and *every* marked row offers it,
// because eligibility is already expressed by omission from a kind's per-row
// action list and a union menu would mean pressing `archive` on five rows and
// silently affecting two. Execution is a loop over the per-container Perform the
// singular path already uses — cross-container atomicity is unachievable across
// separate manifests and repositories, so a batch entry point would promise a
// transaction it could not honour while adding a second method every future kind
// must implement. And every bulk write is confirmed on the hint line first,
// because a mistaken `c` on one set costs one `o` and a mistaken archive over
// twelve costs twelve corrections.

// dashboardBulkPrompt is the inline y/N a bulk verb opens before it writes. It
// holds the rows the question named, so the answer applies to the set the human
// agreed to even though the poll keeps rebuilding the table underneath it. apply
// is the verb itself, already closed over any payload the human chose (the mute
// window is the one such payload today).
type dashboardBulkPrompt struct {
	label string
	rows  []DashboardRow
	apply func(QueueDashboard, []DashboardRow) tea.Cmd
}

// dashboardBulkResult is one row's turn through a bulk verb.
type dashboardBulkResult struct {
	row     DashboardRow
	outcome work.Outcome
	err     error
}

// dashboardBulkVerbMsg carries a whole bulk run's results back to the model in
// one message. One message rather than one per row is what makes the flash a
// single sentence about the run and the Selection's collapse a single edit: a
// per-row message would have the model reporting five times and re-deriving what
// is left after each.
type dashboardBulkVerbMsg struct {
	verb    work.Verb
	results []dashboardBulkResult
}

// pluralActions intersects one action list across the marked rows: the verbs
// every one of them offers *and* every one of them declares plural, in the first
// row's order. list is the per-row action list to intersect — a kind's Actions
// for the action menu, its StatusActions for the submenu — so both menus narrow
// by the same rule and neither grows a vocabulary of its own.
func pluralActions(rows []DashboardRow, list func(DashboardRow) []work.Action) []work.Action {
	if len(rows) == 0 {
		return nil
	}
	var out []work.Action
	for _, action := range list(rows[0]) {
		if !action.Modes.AllowsPlural() {
			continue
		}
		if offersPlural(rows[1:], list, action.Verb) {
			out = append(out, action)
		}
	}
	return out
}

// offersPlural reports whether every row offers verb as a plural-capable action.
func offersPlural(rows []DashboardRow, list func(DashboardRow) []work.Action, verb work.Verb) bool {
	for _, row := range rows {
		found := false
		for _, action := range list(row) {
			if action.Verb == verb && action.Modes.AllowsPlural() {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// bulkCount is how the plural surface names its target set, in the one phrasing
// the prompt and the flash use.
func bulkCount(n int) string { return work.CountPhrase(n, "row", "rows") }

// bulkLabel names what a verb is about to do to how many rows — the prompt's
// question.
func bulkLabel(verb work.Verb, n int) string {
	return fmt.Sprintf("%s %s", verb, bulkCount(n))
}

// singularRefusal is what a verb that acts on one row says when rows are marked.
// A key that goes silently inert is indistinguishable from a bug, so the refusal
// names the verb, the mode and the way out of it (ADR-0215 decision 4).
func singularRefusal(verb string, marked int) string {
	return fmt.Sprintf("%s acts on one row — shift+tab clears the %d selected", verb, marked)
}

// singularModalVerbs is the dashboard's own half of decision 5's capability
// audit. A kind declares its verbs plural in work.Action.Modes, but these verbs
// never reach a kind — the surface intercepts them in dispatchVerb for a modal of
// its own — so what they are capable of is written down here instead, in one
// reviewable list rather than a guard scattered through a dozen handlers. The
// value is the word the refusal uses, which is the kind's own label for the verb.
//
// Every entry resolves a checkout, a worktree or a session *per row*: one modal
// cannot answer for a set, and the handoffs among them address a pane, which has
// no plural meaning at all. Mute, unmute and abandon are absent because their
// input is shared — one duration, or a confirmation, or nothing — which is the
// line ADR-0215 decision 5 draws through the intercepted verbs.
var singularModalVerbs = map[work.Verb]string{
	setkind.VerbDrain:        "drain",
	setkind.VerbVerify:       "verify",
	setkind.VerbBind:         "bind worktree",
	setkind.VerbUnbind:       "unbind worktree",
	setkind.VerbAutoDrain:    "auto-drain",
	setkind.VerbAssist:       "assist",
	setkind.VerbFold:         "fold",
	setkind.VerbUnpark:       "unpark",
	wayfinder.VerbWork:       "work frontier ticket and go",
	wayfinder.VerbWorkHere:   "work frontier ticket",
	wayfinder.VerbFanOut:     "fan out frontier and go",
	wayfinder.VerbFanOutHere: "fan out frontier",
	wayfinder.VerbAssist:     "assist the map and go",
}

// refuseInterceptedVerb answers one of those verbs arriving while rows are
// marked. It is called from dispatchVerb — the one gate every route to an
// intercepted verb passes, whether the human came through a menu hotkey, a flat
// key — so no caller can reach a per-row modal with a Selection
// open, and none of them has to remember to check.
func (m *QueueDashboard) refuseInterceptedVerb(verb work.Verb) bool {
	name, ok := singularModalVerbs[verb]
	if !ok {
		return false
	}
	return m.refuseSingular(name)
}

// selectionMenuItems is the action menu over a Selection: the intersected verbs,
// each labelled with the verb's own label. Reserved keys are dropped exactly as
// they are for one row (ADR-0196), so no kind can claim a movement key by going
// plural.
func (m QueueDashboard) selectionMenuItems(rows []DashboardRow) []dashboardMenuItem {
	actions := pluralActions(rows, m.kinds.actionsFor)
	items := make([]dashboardMenuItem, 0, len(actions))
	for _, action := range actions {
		if actionKeyReserved(action.Key) {
			continue
		}
		items = append(items, dashboardMenuItem{
			key:   action.Key,
			label: action.Label,
			verb:  action.Verb,
		})
	}
	return items
}

// openSelectionMenu answers `a` in selection mode. An empty intersection is not
// an empty menu: the rows share no verb, and saying so is the only answer that
// tells the human what to change.
func (m QueueDashboard) openSelectionMenu() (tea.Model, tea.Cmd) {
	rows := m.selectionRows()
	items := m.selectionMenuItems(rows)
	if len(items) == 0 {
		m.flash.Set(fmt.Sprintf("no verb applies to all %s", bulkCount(len(rows))))
		return m, nil
	}
	m.err = nil
	m.menu = &dashboardMenu{
		row:     rows[0],
		plural:  true,
		targets: rows,
		list:    ui.NewList(items, ui.Opts[dashboardMenuItem]{Wrap: true}),
	}
	return m, nil
}

// openSelectionStatusMenu answers `s` under a Selection: the status verbs every
// marked row offers and declares plural, on a menu that says how many rows it is
// about to write (ADR-0236 decision 10).
func (m QueueDashboard) openSelectionStatusMenu() (tea.Model, tea.Cmd) {
	rows := m.selectionRows()
	actions := pluralActions(rows, m.kinds.statusActionsFor)
	if len(actions) == 0 {
		// Two kinds' status vocabularies share no verb, which is exactly the case the
		// intersection exists for: a Map and a task set have nothing to write in
		// common, and an empty menu would say that by looking broken.
		m.flash.Set(fmt.Sprintf("no status verb applies to all %s", bulkCount(len(rows))))
		return m, nil
	}
	m.err = nil
	m.menu = &dashboardMenu{
		row:     rows[0],
		plural:  true,
		targets: rows,
		status: &dashboardStatusMenu{
			row:  rows[0],
			list: ui.NewList(actions, ui.Opts[work.Action]{Wrap: true}),
		},
	}
	return m, nil
}

// openSelectionMuteMenu answers `m` under a Selection: the surface's own windows
// over every marked row, and the clear entry when every one of them is muted
// (ADR-0236 decision 10). A Selection holding a row that cannot be muted opens
// nothing — the plural menu expressed that by intersection, and at top level it
// has to be said.
func (m QueueDashboard) openSelectionMuteMenu() (tea.Model, tea.Cmd) {
	rows := m.selectionRows()
	if len(rows) == 0 {
		return m, nil
	}
	for _, row := range rows {
		if m.kinds.muterFor(row) == nil {
			m.flash.Set(fmt.Sprintf("not every one of the %s can be muted", bulkCount(len(rows))))
			return m, nil
		}
	}
	m.err = nil
	m.menu = &dashboardMenu{
		row:     rows[0],
		plural:  true,
		targets: rows,
		mute:    newDashboardMuteMenu(m.taskDeps(), rows[0], muteMenuClearable(rows)),
	}
	return m, nil
}

// invokeSelectionMenuItem runs the plural menu's item at idx down the bulk
// dispatch. Nothing in the menu opens another menu any more: both openers it
// used to carry are top-level keys (ADR-0236 decision 1).
func (m QueueDashboard) invokeSelectionMenuItem(idx int) (tea.Model, tea.Cmd) {
	items := m.menu.list.Items()
	if idx < 0 || idx >= len(items) {
		return m, nil
	}
	item := items[idx]
	rows := m.menu.targets
	m.menu = nil
	return m.dispatchBulkVerb(item.verb, rows)
}

// refuseMenuVerb answers a hotkey pressed in the plural menu that names a verb
// the marked rows do offer but only one at a time. The key would otherwise be
// silently inert, which is the one thing a mode must never be.
func (m *QueueDashboard) refuseMenuVerb(key string) {
	for _, row := range m.menu.targets {
		for _, action := range m.kinds.actionsFor(row) {
			if action.Key != key || action.Modes.AllowsPlural() {
				continue
			}
			m.flash.Set(singularRefusal(action.Label, len(m.menu.targets)))
			return
		}
	}
}

// dispatchBulkVerb is the plural counterpart of dispatchVerb: it decides what
// confirmation a verb needs and what runs it. Every verb here is one the kind
// declared plural, so the dozen verbs the dashboard intercepts for a modal of
// its own never arrive — a drain picker or a bind modal resolves a checkout per
// row and cannot answer for a set, which is why none of them is granted.
func (m QueueDashboard) dispatchBulkVerb(verb work.Verb, rows []DashboardRow) (tea.Model, tea.Cmd) {
	m.err = nil
	if len(rows) == 0 {
		return m, nil
	}
	switch verb {
	case work.VerbCopyName:
		// Copying is not a write: it changes no container and a mistaken one costs
		// a second keypress, so it keeps the immediacy the single-row `y` has.
		return m, m.performBulkKindVerb(verb, rows)
	}
	return m.openBulkPrompt(bulkLabel(verb, len(rows)), rows, func(m QueueDashboard, rows []DashboardRow) tea.Cmd {
		return m.performBulkKindVerb(verb, rows)
	})
}

// openBulkPrompt puts the question on the hint line. The flash is cleared first:
// a prompt a stale message covered would be answered blind.
func (m QueueDashboard) openBulkPrompt(label string, rows []DashboardRow, apply func(QueueDashboard, []DashboardRow) tea.Cmd) (tea.Model, tea.Cmd) {
	m.flash.Set("")
	m.bulkPrompt = &dashboardBulkPrompt{label: label, rows: rows, apply: apply}
	return m, nil
}

// updateBulkPrompt runs the confirmation's grammar — y writes, enter/n/esc/C-c
// back out, everything else is ignored — which is the monitor's grammar
// unchanged. A cancel keeps the whole Selection: nothing was written, so nothing
// is consumed.
func (m QueueDashboard) updateBulkPrompt(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y":
		prompt := m.bulkPrompt
		m.bulkPrompt = nil
		return m, prompt.apply(m, prompt.rows)
	case "n", "enter", "esc", "ctrl+c":
		m.bulkPrompt = nil
	}
	return m, nil
}

// bulkCmd is the loop itself: every row takes its turn, a failure is recorded
// rather than raised, and the run finishes whatever happened to any one row
// (ADR-0215 decision 6). It runs inside a tea.Cmd for the same reason one
// Perform does — a manifest write does not belong between two keypresses.
func (m QueueDashboard) bulkCmd(verb work.Verb, rows []DashboardRow, run func(DashboardRow) (work.Outcome, error)) tea.Cmd {
	rows = slices.Clone(rows)
	return func() tea.Msg {
		msg := dashboardBulkVerbMsg{verb: verb, results: make([]dashboardBulkResult, 0, len(rows))}
		for _, row := range rows {
			outcome, err := run(row)
			msg.results = append(msg.results, dashboardBulkResult{row: row, outcome: outcome, err: err})
		}
		return msg
	}
}

// performBulkKindVerb runs verb over every row through the row's own kind — the
// same Kind.Perform the single-row path calls, with no batch method anywhere on
// the seam.
func (m QueueDashboard) performBulkKindVerb(verb work.Verb, rows []DashboardRow) tea.Cmd {
	kinds := m.kinds
	return m.bulkCmd(verb, rows, func(row DashboardRow) (work.Outcome, error) {
		k := kinds.kindFor(row)
		if k == nil {
			return work.Outcome{}, fmt.Errorf("no Work kind wired for %s", row.ID)
		}
		return k.Perform(row, nil, verb)
	})
}

// bulkMute records one window on every marked row. The window is the shared
// input that makes mute plural at all: one date answers identically for every
// row, which is the line decision 5 draws through the dashboard's own modals.
func (m QueueDashboard) bulkMute(rows []DashboardRow, window MuteWindow) tea.Cmd {
	kinds := m.kinds
	return m.bulkCmd(work.VerbMute, rows, func(row DashboardRow) (work.Outcome, error) {
		muter := kinds.muterFor(row)
		if muter == nil {
			return work.Outcome{}, fmt.Errorf("%s cannot be muted", row.ID)
		}
		return muter.Mute(row, window.Until, window.Secret)
	})
}

// bulkUnmute clears the mute on every marked row through the same seam.
func (m QueueDashboard) bulkUnmute(rows []DashboardRow) tea.Cmd {
	kinds := m.kinds
	return m.bulkCmd(work.VerbUnmute, rows, func(row DashboardRow) (work.Outcome, error) {
		muter := kinds.muterFor(row)
		if muter == nil {
			return work.Outcome{}, fmt.Errorf("%s cannot be muted", row.ID)
		}
		return muter.Unmute(row)
	})
}

// applyBulkVerb carries out a finished run: the clipboard payloads it produced,
// the one-line report, and what is left of the Selection. On success the marks
// are consumed; on partial failure the Selection collapses to exactly the rows
// that failed, so they stay in the region, a retry needs no re-marking, and each
// reason surfaces in turn as the set shrinks (ADR-0215 decision 6).
func (m QueueDashboard) applyBulkVerb(msg dashboardBulkVerbMsg) (tea.Model, tea.Cmd) {
	done := 0
	var reasons, clipboard, failed []string
	for _, result := range msg.results {
		if result.err != nil {
			failed = append(failed, result.row.CursorKey)
			reasons = append(reasons, fmt.Sprintf("%s: %v", result.row.ID, result.err))
			continue
		}
		done++
		if result.outcome.Clipboard != "" {
			clipboard = append(clipboard, result.outcome.Clipboard)
		}
	}

	m.selection.Clear()
	for _, key := range failed {
		m.selection.Toggle(key)
	}

	status := bulkOutcome(msg.verb, done, reasons)
	if len(clipboard) > 0 {
		// One write of every payload, newline-joined in the region's order: a
		// clipboard holds one thing, and five separate writes would leave the last
		// row's name and call it a bulk copy.
		if err := m.clipboardCopy()(strings.Join(clipboard, "\n")); err != nil {
			status = fmt.Sprintf("copy failed: %v", err)
		}
	}
	m.flash.Set(status)
	m.applySearch(m.activeQuery())
	return m, m.reload()
}

// bulkOutcome words a run on the one line it has: what it wrote, then the single
// reason when exactly one row failed or a bare count when several did. One
// reason is readable and useful; five stacked reasons are neither, and the
// collapse leaves the failures marked so the next run names them one at a time.
func bulkOutcome(verb work.Verb, done int, reasons []string) string {
	var parts []string
	if done > 0 {
		head := bulkLabel(verb, done)
		if verb == work.VerbCopyName {
			// The surface performed the clipboard write, so it reports it: "copy-name
			// 5 rows" would name the verb id where the human is looking for the act.
			head = "copied " + work.CountPhrase(done, "name", "names")
		}
		parts = append(parts, head)
	}
	switch len(reasons) {
	case 0:
	case 1:
		parts = append(parts, "1 failed: "+reasons[0])
	default:
		parts = append(parts, fmt.Sprintf("%d failed", len(reasons)))
	}
	return strings.Join(parts, " · ")
}
