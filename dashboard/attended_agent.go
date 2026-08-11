package dashboard

import (
	"fmt"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/setkind"
	"github.com/glebglazov/pop/wayfinder"
	"github.com/glebglazov/pop/work"
)

// The dashboard's attended-agent visibility: the persistent subheader and the
// action-menu rows that name the entry an attended launch will use (ADR-0196
// decision 9, kept by ADR-0202 decision 5). Both read the merged config, so an
// override written in the Config dashboard is what they report.

// AfterConfigReload hands the page what its host re-read after an override was
// written from the Config dashboard (ADR-0202 decision 14). The page's renders
// and its next poll both build from this value, so the subheader and the
// attended action rows report the override the moment the modal closes.
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
	return tasks.FormatAttendedAgentStatus(tasks.EffectiveAttendedEntry(m.cfg))
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
	return label + " · " + tasks.FormatAgentEntry(tasks.EffectiveAttendedEntry(m.cfg))
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

// reservedActionKeys are chords no Work kind may claim as Action.Key: the
// movement keys, which every table shares. Nothing else is reserved — ADR-0202
// decision 5 returned alt+a to the pool when it deleted the picker it opened.
var reservedActionKeys = []string{"j", "k", "J", "K"}

func actionKeyReserved(key string) bool {
	for _, reserved := range reservedActionKeys {
		if key == reserved {
			return true
		}
	}
	return false
}
