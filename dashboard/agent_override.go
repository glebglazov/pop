package dashboard

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/setkind"
	"github.com/glebglazov/pop/ui"
	"github.com/glebglazov/pop/wayfinder"
	"github.com/glebglazov/pop/work"
)

// ensureAgentOverrides makes sure the shared process-lived override bag exists
// on the dashboard's tasks.Deps so both pages and every in-process attended
// launch see the same promotions (ADR-0196).
func (m QueueDashboard) ensureAgentOverrides() *tasks.AgentOverrides {
	if m.d == nil {
		return nil
	}
	if m.d.Tasks == nil {
		m.d.Tasks = tasks.DefaultDeps()
	}
	if m.d.Tasks.AgentOverrides == nil {
		m.d.Tasks.AgentOverrides = tasks.NewAgentOverrides()
	}
	return m.d.Tasks.AgentOverrides
}

func (m QueueDashboard) agentOverrides() *tasks.AgentOverrides {
	if m.d == nil || m.d.Tasks == nil {
		return nil
	}
	return m.d.Tasks.AgentOverrides
}

// openAgentOverridePicker opens the two-level override picker over the current
// page. Callers must only invoke this when no other picker/menu is live.
func (m QueueDashboard) openAgentOverridePicker() QueueDashboard {
	overrides := m.ensureAgentOverrides()
	groups := agentOverridePickerGroups(m.cfg, overrides)
	m.agentPick = ui.NewAgentOverridePicker(groups)
	m.err = nil
	m.statusMsg = ""
	return m
}

func agentOverridePickerGroups(cfg *config.Config, _ *tasks.AgentOverrides) []ui.AgentOverrideGroup {
	return tasks.AgentOverridePickerGroups(cfg)
}

// updateAgentOverridePicker drives the overlay: esc/ctrl+c leave unchanged;
// digits and arrows are the picker's own.
func (m QueueDashboard) updateAgentOverridePicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.agentPick == nil {
		return m, nil
	}
	kpm, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	updated, _ := m.agentPick.Update(kpm)
	p, _ := updated.(*ui.AgentOverridePicker)
	m.agentPick = p
	if !p.Done() {
		return m, nil
	}
	if choice := p.Choice(); choice != nil {
		m.ensureAgentOverrides().Promote(choice.Group, choice.Cmd)
	}
	m.agentPick = nil
	return m, nil
}

func (m QueueDashboard) agentOverrideStatusLine() string {
	entry := tasks.EffectiveAttendedEntry(m.cfg, m.agentOverrides())
	return tasks.FormatAgentOverrideStatus(entry)
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
	entry := tasks.EffectiveAttendedEntry(m.cfg, m.agentOverrides())
	return label + " · " + tasks.FormatAgentEntry(entry)
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

// reservedActionKeys are chords no Work kind may claim as Action.Key. Movement
// keys stay reserved as today; the agent-override chord is the ADR-0196
// cross-kind key.
var reservedActionKeys = []string{"j", "k", "J", "K", tasks.AgentOverrideKey}

func actionKeyReserved(key string) bool {
	for _, reserved := range reservedActionKeys {
		if key == reserved {
			return true
		}
	}
	return false
}

func (m QueueDashboard) dashboardAgentOverrideMenuLines() []string {
	if m.agentPick == nil {
		return nil
	}
	raw := m.agentPick.ViewContent()
	lines := strings.Split(strings.TrimRight(raw, "\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, ui.TruncateString("    "+line, m.width))
	}
	return out
}

func (m QueueDashboard) viewWithAgentOverridePicker() string {
	var body strings.Builder
	if m.err != nil {
		fmt.Fprintf(&body, "refresh error: %v\n", m.err)
	}
	if m.actionErr != nil {
		fmt.Fprintf(&body, "%s\n", dashboardActionErrorLine(m.actionErr))
	}
	fmt.Fprintf(&body, "%s\n", m.pageHeader())
	if line := m.agentOverrideStatusLine(); line != "" {
		fmt.Fprintf(&body, "%s\n", ui.HintStyle.Render(line))
	}
	fmt.Fprintln(&body)
	renderDashboardTable(&body, m.page, m.kinds, m.snap.Containers, m.list.Cursor(), m.width, m.height, m.liveCache())
	for _, ml := range m.dashboardAgentOverrideMenuLines() {
		fmt.Fprintf(&body, "%s\n", ml)
	}
	writeDashboardFooter(&body, m.height, ui.HintStyle.Render("1-9 jump · 0 back · enter/esc leave unchanged"))
	return body.String()
}
