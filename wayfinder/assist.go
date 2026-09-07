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

func assistPaneTitleFor(cfg *config.Config) string {
	return assistPaneTitle + " · " + tasks.FormatAgentEntry(tasks.EffectiveAttendedEntry(cfg))
}

// AssistPane is one of the Map's own attended sessions: a pane in its `map`
// window, addressed by slot, holding a conversation about the Map itself rather
// than about any one ticket (ADR-0184). It claims nothing, so there is no
// ClaimResult beside it and nothing to release when the pane dies.
type AssistPane struct {
	MapID   string
	Session MapSession
	Window  string
	PaneID  string
	Title   string
	// Slot is the digit that addresses this pane among the Map's assist panes
	// (ADR-0263). It is what a caller reports and what a second ask has to name
	// to come back here rather than to a sibling.
	Slot tmux.PaneSlot
	// Reused reports that an assist session was already live in the pane, so it
	// became a jump target rather than being sent the command again (ADR-0158).
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

// SpawnAssist returns to the Map's assist session, which is what asking for
// assist without naming a slot means: it lands on the lowest slot the Map
// already holds a pane on — a live one for preference, an idle one respawned in
// place otherwise — and on the first slot when it holds none. `pop map assist`
// reaches the session through here, so typing the verb twice never opens a
// second conversation.
//
// No claim is taken and none is needed for the manifest: the writes an assist
// session makes go through the same per-Map lock every resolve takes. Prose the
// session writes into map.md outside the `pop:generated` markers takes no lock
// at all, and the single reused pane is no longer what covers it: a Map holds up
// to nine assist panes now (ADR-0263), so between two live sessions those prose
// edits are last-writer-wins. A claim row would not fix it either, and would
// need a TTL and a release path for a session holding no ticket.
func SpawnAssist(d *Deps, cfg *config.Config, m Map) (*AssistPane, error) {
	session, err := EnsureMapSession(d, m.ID)
	if err != nil {
		return nil, err
	}
	slot, err := tmux.LowestHeldPaneSlot(d.tmux(), tmux.TagAssist, session.Name, mapWindow, m.ID)
	if err != nil {
		return nil, err
	}
	return openAssistPane(d, cfg, m, *session, slot)
}

// SpawnAssistOnSlot opens (or returns to) the assist pane on one named slot —
// the Assist pane menu's digit, which is how an operator reaches a particular
// one of a Map's conversations. A pane still running there is a jump target; one
// that has fallen back to its shell is respawned in place (ADR-0158).
func SpawnAssistOnSlot(d *Deps, cfg *config.Config, m Map, slot tmux.PaneSlot) (*AssistPane, error) {
	session, err := EnsureMapSession(d, m.ID)
	if err != nil {
		return nil, err
	}
	return openAssistPane(d, cfg, m, *session, slot)
}

// SpawnNewAssist opens another assist session on the Map, on the lowest slot it
// holds no pane on. Past the ninth it refuses with tmux's own ErrNoFreePaneSlot
// rather than opening a session no digit could address (ADR-0263).
func SpawnNewAssist(d *Deps, cfg *config.Config, m Map) (*AssistPane, error) {
	session, err := EnsureMapSession(d, m.ID)
	if err != nil {
		return nil, err
	}
	slot, err := tmux.LowestFreePaneSlot(d.tmux(), tmux.TagAssist, session.Name, mapWindow, m.ID)
	if err != nil {
		return nil, err
	}
	return openAssistPane(d, cfg, m, *session, slot)
}

// openAssistPane is the one spawn body all three entry points share: resolve the
// command and land it in the pane on slot. `pop map assist` and the Work
// dashboard's map row both come through here, so a session started from either
// looks the same.
func openAssistPane(d *Deps, cfg *config.Config, m Map, session MapSession, slot tmux.PaneSlot) (*AssistPane, error) {
	if session.Dir == "" {
		return nil, ErrNoTrunk
	}
	command, err := AssistInvocation(d, cfg, m.ID, session.Dir)
	if err != nil {
		return nil, err
	}
	paneID, reused, err := openSlottedMapPane(d, session, tmux.TagAssist, slot, m.ID, assistPaneTitleFor(cfg), command)
	if err != nil {
		return nil, err
	}
	return &AssistPane{
		MapID:   m.ID,
		Session: session,
		Window:  mapWindow,
		PaneID:  paneID,
		Title:   assistPaneTitleFor(cfg),
		Slot:    slot,
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
