package wayfinder

import (
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/tasks"
)

// assistPaneTitle titles the Map-scoped pane. A wall of tiled panes reads as the
// ticket stems being grilled; this one reads as the session that holds no ticket
// at all.
const assistPaneTitle = "assist"

func assistPaneTitleFor(d *Deps, cfg *config.Config) string {
	var overrides *tasks.AgentOverrides
	if d != nil {
		if td := d.taskDeps(); td != nil {
			overrides = td.AgentOverrides
		}
	}
	return assistPaneTitle + " · " + tasks.FormatAgentEntry(tasks.EffectiveAttendedEntry(cfg, overrides))
}

// AssistPane is the Map's own attended session: one pane per Map in its `map`
// window, holding a conversation about the Map itself rather than about any one
// ticket (ADR-0184). It claims nothing, so there is no ClaimResult beside it and
// nothing to release when the pane dies.
type AssistPane struct {
	MapID   string
	Session MapSession
	Window  string
	PaneID  string
	Title   string
	// Reused reports that an assist session was already live in the pane, so it
	// became a jump target rather than being sent the command again (ADR-0158).
	// This is also what dissolves the two-sessions-editing-one-map race: asking
	// for a second assist session lands you in the first.
	Reused bool
}

// AssistMap is `pop map assist`: the Map-scoped session, reachable whatever the
// frontier looks like. An empty or fully-claimed frontier is exactly when an idea
// about the Map's own shape has nowhere else to land, so nothing here consults the
// frontier at all — the Map only has to be registered and not BROKEN, the same
// gate the claiming verbs apply.
func AssistMap(d *Deps, cfg *config.Config, cwd, mapID string) (*AssistPane, error) {
	m, err := findClaimableMap(d, cwd, mapID)
	if err != nil {
		return nil, err
	}
	return SpawnAssist(d, cfg, m)
}

// SpawnAssist opens (or returns to) m's assist pane. It is the only assist spawn
// path: `pop map assist` and the Work dashboard's map row both reach the session
// through here, so a session started from either looks the same.
//
// No claim is taken and none is needed. The manifest writes an assist session
// makes go through the same per-Map lock every resolve takes, and the prose race
// two sessions could run on map.md is dissolved by the single reused pane rather
// than by a claim row that would need a TTL and a release path for a session
// holding no ticket.
func SpawnAssist(d *Deps, cfg *config.Config, m Map) (*AssistPane, error) {
	session, err := EnsureMapSession(d, m.ID)
	if err != nil {
		return nil, err
	}
	if session.Dir == "" {
		return nil, ErrNoTrunk
	}
	command, err := AssistInvocation(d, cfg, m.ID, session.Dir)
	if err != nil {
		return nil, err
	}
	paneID, reused, err := openMapPane(d, *session, tmux.TagAssist, m.ID, assistPaneTitleFor(d, cfg), command)
	if err != nil {
		return nil, err
	}
	return &AssistPane{
		MapID:   m.ID,
		Session: *session,
		Window:  mapWindow,
		PaneID:  paneID,
		Title:   assistPaneTitleFor(d, cfg),
		Reused:  reused,
	}, nil
}

// AssistInvocation is the attended agent command an assist pane runs: the
// configured interactive agent, opened on the wayfinding skill in assist mode for
// one Map and no ticket. Naming no ticket is what keeps the
// one-non-research-ticket-per-session rule intact — a session with no ticket in
// hand has none to resolve.
func AssistInvocation(d *Deps, cfg *config.Config, mapID, dir string) (string, error) {
	return agentPaneCommand(d, cfg, AssistModeInvocation(skillsPrefixOf(cfg), mapID), dir)
}
