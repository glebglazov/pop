package queue

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/work"
)

// The dashboard's end of `Kind.Perform`: how a verb the dashboard owns no modal
// for is run and what its outcome does to the surface. Every verb the Task-set
// kind still dispatches queue-side has its own case in dispatchVerb, because a
// drain picker or a bind modal is the dashboard's; everything else — every Routine
// verb, and any future kind's — arrives here and needs no dashboard code at all.
//
// The kind runs the verb inside a tea.Cmd, never on the update loop: a verb writes
// a manifest, spawns a pane, or reads a store, and none of that belongs between
// two keypresses.

// dashboardKindVerbMsg carries one performed verb's outcome back to the model.
// The row, item and inPeek travel with it so the result lands on the surface the
// verb was invoked from — a row verb on the dashboard's status line, an item verb
// on the detail's or the peek's.
type dashboardKindVerbMsg struct {
	row     DashboardRow
	item    *work.Item
	inPeek  bool
	verb    work.Verb
	outcome work.Outcome
	err     error
}

// performKindVerb asks the row's own kind to run verb.
func (m QueueDashboard) performKindVerb(row DashboardRow, verb work.Verb) tea.Cmd {
	return m.performKind(row, nil, false, verb)
}

// performKindItemVerb asks the row's kind to run verb over one of its items.
func (m QueueDashboard) performKindItemVerb(row DashboardRow, item work.Item, inPeek bool, verb work.Verb) tea.Cmd {
	return m.performKind(row, &item, inPeek, verb)
}

func (m QueueDashboard) performKind(row DashboardRow, item *work.Item, inPeek bool, verb work.Verb) tea.Cmd {
	kinds := m.kinds
	return func() tea.Msg {
		msg := dashboardKindVerbMsg{row: row, item: item, inPeek: inPeek, verb: verb}
		k := kinds.kindFor(row)
		if k == nil {
			msg.err = fmt.Errorf("no Work kind wired for %s", row.ID)
			return msg
		}
		msg.outcome, msg.err = k.Perform(row, item, verb)
		return msg
	}
}

// applyKindVerb carries out a performed verb's outcome. It is the one place the
// four outcome kinds are interpreted, so a kind that returns a refresh, a
// clipboard write, a detail view or a pane handoff gets the same treatment
// whichever kind it is and whichever page the row was listed on.
func (m QueueDashboard) applyKindVerb(msg dashboardKindVerbMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.reportVerbError(msg, msg.err)
		return m, nil
	}
	switch msg.outcome.Kind {
	case work.OutcomeMessage:
		m.reportVerbStatus(msg, m.outcomeMessage(msg.outcome))
		return m, nil
	case work.OutcomeRefresh:
		m.reportVerbStatus(msg, m.outcomeMessage(msg.outcome))
		return m, m.reload()
	case work.OutcomeDetail:
		row, ok := m.rowByCursorKey(msg.row.CursorKey)
		if !ok {
			row = msg.row
		}
		m.detail = newDetailView(row)
		return m, nil
	case work.OutcomeHandoff:
		return m, m.handoffOutcome(msg.row, msg.outcome.Handoff)
	}
	// A caller-modal outcome names a verb whose modal the dashboard opened itself,
	// so there is nothing left to do here.
	return m, nil
}

// outcomeMessage writes the outcome's clipboard payload, if it carries one, and
// returns the line to show: the kind's own message on success, since the kind
// knows what it copied better than a generic "copied <payload>" would.
func (m QueueDashboard) outcomeMessage(outcome work.Outcome) string {
	if outcome.Clipboard == "" {
		return outcome.Message
	}
	if err := m.clipboardCopy()(outcome.Clipboard); err != nil {
		return fmt.Sprintf("copy failed: %v", err)
	}
	if outcome.Message != "" {
		return outcome.Message
	}
	return "copied " + outcome.Clipboard
}

// handoffOutcome performs ADR-0158's handoff sequence for a kind's pane: focus
// the pane the verb named and quit, or explain why the operator went nowhere. A
// handoff that names a directory instead of a pane is a shell — the one handoff
// the dashboard spawns itself, because the spawn is queue's own launcher.
func (m QueueDashboard) handoffOutcome(row DashboardRow, h work.Handoff) tea.Cmd {
	if target := strings.TrimSpace(h.Target); target != "" {
		d := m.d
		return func() tea.Msg {
			return handoffAfterLaunch(d, DashboardDrainResult{PaneID: target}, nil)
		}
	}
	if dir := strings.TrimSpace(h.Dir); dir != "" {
		return m.launchShell(row, dir)
	}
	return func() tea.Msg {
		return dashboardHandoffMsg{status: "nothing to hand off to"}
	}
}

// reportVerbStatus puts a verb's one-line result where the operator is looking.
func (m *QueueDashboard) reportVerbStatus(msg dashboardKindVerbMsg, status string) {
	if status == "" {
		return
	}
	switch {
	case msg.item != nil && msg.inPeek && m.detail != nil && m.detail.peek != nil:
		m.detail.peek.statusMsg = status
	case msg.item != nil && m.detail != nil:
		m.detail.statusMsg = status
	default:
		m.statusMsg = status
	}
}

// reportVerbError surfaces a refused verb. A row verb's failure is sticky on the
// dashboard's action-error line; an item verb's stays inside the detail it was run
// from, the way the detail's own status writes already report.
func (m *QueueDashboard) reportVerbError(msg dashboardKindVerbMsg, err error) {
	if msg.item != nil && m.detail != nil {
		m.reportVerbStatus(msg, fmt.Sprintf("error: %v", err))
		return
	}
	m.actionErr = err
}

// rowByCursorKey finds the current build of a row, so a verb that opens a view
// over it shows what the last poll read rather than the snapshot the menu was
// opened on.
func (m QueueDashboard) rowByCursorKey(key string) (DashboardRow, bool) {
	for _, row := range m.snap.Containers {
		if row.CursorKey == key {
			return row, true
		}
	}
	return DashboardRow{}, false
}
