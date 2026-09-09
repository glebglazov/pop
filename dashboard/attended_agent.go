package dashboard

import (
	"fmt"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/setkind"
	"github.com/glebglazov/pop/wayfinder"
	"github.com/glebglazov/pop/work"
)

// The dashboard's attended-agent surface: the persistent subheader and the
// action-menu rows that name the entry an attended launch will use (ADR-0196
// decision 9, kept by ADR-0202 decision 5).

// AfterConfigReload hands the page what its host re-read after a Config dashboard
// write or an outside edit (ADR-0202 decision 14).
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
	return label + " · " + tasks.FormatAgentEntry(m.effectiveAttendedEntry())
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
// movement keys every table shares. The retired attended chooser has no
// replacement shortcut and needs no global key-space reservation (ADR-0266).
var reservedActionKeys = []string{"j", "k", "J", "K"}

func actionKeyReserved(key string) bool {
	for _, reserved := range reservedActionKeys {
		if key == reserved {
			return true
		}
	}
	return false
}
