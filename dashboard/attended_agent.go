package dashboard

import (
	"fmt"
	"io"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/setkind"
	"github.com/glebglazov/pop/ui"
	"github.com/glebglazov/pop/wayfinder"
	"github.com/glebglazov/pop/work"
)

// The dashboard's attended-agent surface: the persistent subheader and the
// action-menu rows that name the entry an attended launch will use (ADR-0196
// decision 9, kept by ADR-0202 decision 5), and the chooser that changes it
// for this dashboard session (ADR-0269). A session choice wins over the merged
// config for every render and launch from this dashboard only.

// AfterConfigReload hands the page what its host re-read after a Config dashboard
// write or an outside edit (ADR-0202 decision 14). A session choice stays in
// force ahead of the new config until this dashboard closes.
//
// A re-read that failed arrives as err instead: the host may not print — it
// hosts a component whose whole contract is that nothing writes to stdout — so
// the page shows it in the action-error line it already has, and keeps rendering
// the config it was built with.
func (m QueueDashboard) AfterConfigReload(cfg *config.Config, err error) QueueDashboard {
	if err != nil {
		m.actionErr = fmt.Errorf("could not re-read config after the override write: %w", err)
		return m
	}
	m.cfg = cfg
	return m
}

// attendedAgentStatusLine is the persistent subheader naming the attended entry
// in force and where it is changed.
func (m QueueDashboard) attendedAgentStatusLine() string {
	return tasks.FormatAttendedAgentStatus(m.effectiveAttendedEntry())
}

// attendedLaunchSpec is the Agent entry an attended launch from this page must
// run: the one the row it launches from names. Passing it makes the render
// binding (ADR-0264 decision 8) — the pane resolves the merged config for
// itself, so without it a pick made elsewhere between the draw and the
// keystroke would launch something the row never said.
func (m QueueDashboard) attendedLaunchSpec() string {
	return m.effectiveAttendedEntry().Cmd
}

func (m QueueDashboard) effectiveAttendedEntry() tasks.AgentGroupEntry {
	if m.attendedChoice != nil {
		return *m.attendedChoice
	}
	return tasks.EffectiveAttendedEntry(m.cfg)
}

// attendedActionVerb reports whether verb's action-menu row must name the
// attended entry that will run (ADR-0196 decision 9).
func attendedActionVerb(verb work.Verb) bool {
	switch verb {
	case setkind.VerbAssist,
		wayfinder.VerbAssist,
		wayfinder.VerbWork, wayfinder.VerbWorkHere,
		wayfinder.VerbFanOut, wayfinder.VerbFanOutHere:
		return true
	default:
		return false
	}
}

// enrichAttendedActionLabel appends the shared entry render to an attended
// verb's menu label.
func (m QueueDashboard) enrichAttendedActionLabel(verb work.Verb, label string) string {
	if !attendedActionVerb(verb) {
		return label
	}
	label += " · " + tasks.FormatAgentEntry(m.effectiveAttendedEntry())
	if tasks.AttendedPickOffered(m.cfg) {
		// The affordance is the row's own, and only where a choice exists: a list
		// of one has nothing to open (ADR-0269 decision 5).
		label += " · " + ui.AttendedPickKeyLabel + " to change"
	}
	return label
}

func (m QueueDashboard) enrichItemActions(actions []work.Action) []work.Action {
	if len(actions) == 0 {
		return actions
	}
	out := make([]work.Action, 0, len(actions))
	for _, a := range actions {
		if actionKeyReserved(a.Key) {
			continue
		}
		a.Label = m.enrichAttendedActionLabel(a.Verb, a.Label)
		out = append(out, a)
	}
	return out
}

// reservedActionKeys are keys no Work kind may claim as Action.Key: the
// movement keys every table shares. The attended chooser now opens with tab
// inside the Run menu, so it needs no global key-space reservation (ADR-0269).
var reservedActionKeys = []string{"j", "k", "J", "K"}

func actionKeyReserved(key string) bool {
	for _, reserved := range reservedActionKeys {
		if key == reserved {
			return true
		}
	}
	return false
}

// The attended chooser as a modal over the Run menu it opens from (ADR-0269
// decision 2). The component is the one a gate opens; the difference is only in
// the hosting — a gate runs it as its own inline program, and here it is a model
// this page drives, because a dashboard is already a program and a second one
// would take the terminal out from under it.

// openAttendedPick opens the chooser over the attended list the merged config
// resolves to. Nothing about the page changes underneath it: the rows, the
// cursor, the open Run menu and the poll are where they were when it closes.
func (m QueueDashboard) openAttendedPick() (tea.Model, tea.Cmd) {
	m.attendedPick = ui.NewAttendedAgentPicker(tasks.AttendedPickChoices(m.cfg))
	return m, nil
}

// updateAttendedPick drives the open chooser. Its tea.Quit is how it says it is
// closed, and it is dropped here: a component that ended the host program would
// make the dashboard's own quit key a trapdoor.
//
// A pick becomes a session choice on this page and is sent to the shell, which
// is the only place that holds both pages. No config or store value is written.
func (m QueueDashboard) updateAttendedPick(msg tea.Msg) (tea.Model, tea.Cmd) {
	updated, cmd := m.attendedPick.Update(msg)
	if p, ok := updated.(*ui.AttendedAgentPicker); ok {
		m.attendedPick = p
	}
	if !m.attendedPick.Done() {
		return m, cmd
	}
	picked := m.attendedPick.Choice()
	m.attendedPick = nil
	if picked == nil {
		return m, nil
	}
	entry := tasks.LaunchedAttendedEntry(m.cfg, picked.Cmd)
	m = m.WithAttendedSessionChoice(entry)
	return m, func() tea.Msg { return attendedSessionChoiceMsg{entry: entry} }
}

type attendedSessionChoiceMsg struct{ entry tasks.AgentGroupEntry }

// AttendedSessionChoice returns the choice a page asks its shell to share.
func AttendedSessionChoice(msg tea.Msg) (tasks.AgentGroupEntry, bool) {
	picked, ok := msg.(attendedSessionChoiceMsg)
	return picked.entry, ok
}

// WithAttendedSessionChoice applies the shell-owned choice to this page. An open
// Run menu is refreshed on it, so its attended rows name the picked entry at
// once rather than at the next open.
func (m QueueDashboard) WithAttendedSessionChoice(entry tasks.AgentGroupEntry) QueueDashboard {
	m.attendedChoice = &entry
	return m.withRefreshedMenuItems()
}

// AttendedPickOpen reports whether the chooser owns the keyboard, so the host
// suspends its own keys for as long as it does (ADR-0202 decision 11).
func (m QueueDashboard) AttendedPickOpen() bool { return m.attendedPick != nil }

func renderAttendedPickModal(w io.Writer, picker *ui.AttendedAgentPicker, avail, width int) {
	if picker == nil {
		return
	}
	lines := strings.Split(strings.TrimRight(picker.ViewContent(), "\n"), "\n")
	if avail > 0 && len(lines) > avail {
		lines = lines[:avail]
	}
	for _, line := range lines {
		fmt.Fprintln(w, ui.TruncateString(line, width))
	}
}
