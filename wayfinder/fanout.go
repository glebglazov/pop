package wayfinder

import (
	"github.com/glebglazov/pop/config"
)

// SpawnedTicket is one Decision ticket handed to a Grilling pane.
type SpawnedTicket struct {
	Ticket Ticket
	Pane   *GrillingPane
	// Claim is nil when the ticket went to a parallel session between the scan
	// that offered it and the claim the spawned pane took. The pane is already up
	// by then, so the loss costs one idle pane and nothing else.
	Claim *ClaimResult
}

// FrontierSpawn is one pass of the spawn path over a Map's frontier.
type FrontierSpawn struct {
	MapID string
	// Session is where the panes landed. It is zero when nothing was spawned: an
	// empty frontier creates no session, so fan-out is safe to run speculatively.
	Session MapSession
	Spawned []SpawnedTicket
	// Frontier is how many tickets the entry scan found — the loop's upper bound.
	Frontier int
	// Lost counts tickets a parallel session claimed mid-pass.
	Lost int
}

// SpawnTicket is the single-ticket spawn path, and the only one: `pop map next`,
// `pop map fan-out` and the Work dashboard's map row all reach a Grilling pane
// through here, so a session started from any of them looks the same.
//
// The pane comes first and the claim second, with that pane as owner. The other
// order is what pop did until ADR-0182, and it parked every claim on the *calling*
// pane: the spawned agent's own `pop map claim` was then refused as "claimed by
// another pane" for the claim's whole TTL. The cost of the inversion is that a
// ticket lost to a parallel session leaves an idle pane behind rather than a
// stranded claim.
func SpawnTicket(d *Deps, cfg *config.Config, m Map, ticket Ticket) (*SpawnedTicket, error) {
	session, err := EnsureMapSession(d, m.ID)
	if err != nil {
		return nil, err
	}
	if session.Dir == "" {
		return nil, ErrNoTrunk
	}
	command, err := GrillingInvocation(d, cfg, m.ID, ticket.ID, session.Dir)
	if err != nil {
		return nil, err
	}
	pane, err := openGrillingPane(d, *session, ticket, command, attendedEntryLabel(cfg))
	if err != nil {
		return nil, err
	}
	claim, err := ClaimTicketForPane(d, m, ticket, pane.PaneID)
	if err != nil {
		return nil, err
	}
	return &SpawnedTicket{Ticket: ticket, Pane: pane, Claim: claim}, nil
}

// SpawnFrontier runs SpawnTicket over a Map's frontier — a loop over the
// single-ticket spawn, never a second path. It is bounded above by the frontier
// the entry scan found, so tickets released while it runs wait for the next pass;
// limit caps it further (1 is `pop map next`, 0 the whole frontier).
//
// Re-running it tops up: a claimed ticket is off the frontier, and a ticket whose
// pane is still alive is reused as a jump target rather than sent work twice.
func SpawnFrontier(d *Deps, cfg *config.Config, m Map, limit int) (*FrontierSpawn, error) {
	frontier := Frontier(m.Tickets)
	out := &FrontierSpawn{MapID: m.ID, Frontier: len(frontier)}
	for _, ticket := range frontier {
		if limit > 0 && len(out.Spawned) >= limit {
			break
		}
		spawned, err := SpawnTicket(d, cfg, m, ticket)
		if err != nil {
			return nil, err
		}
		out.Session = spawned.Pane.Session
		if spawned.Claim == nil {
			out.Lost++
			continue
		}
		out.Spawned = append(out.Spawned, *spawned)
	}
	return out, nil
}

// NextFrontierTicket is `pop map next`: the spawn path bounded to one ticket. An
// empty frontier refuses here, because the verb's whole output is the ticket it
// handed out — fan-out, whose purpose is bulk, reports nothing to do instead.
func NextFrontierTicket(d *Deps, cfg *config.Config, cwd, mapID string) (*FrontierSpawn, error) {
	m, err := findClaimableMap(d, cwd, mapID)
	if err != nil {
		return nil, err
	}
	out, err := SpawnFrontier(d, cfg, m, 1)
	if err != nil {
		return nil, err
	}
	if len(out.Spawned) == 0 {
		return nil, emptyFrontier(m)
	}
	return out, nil
}

// FanOutFrontier is `pop map fan-out`: one Grilling pane for every ticket on the
// frontier, HITL ones included, in one act. There is no cap and no spill — every
// frontier ticket gets a pane, and tmux's own pane zoom answers a cramped window
// rather than a second place to look.
func FanOutFrontier(d *Deps, cfg *config.Config, cwd, mapID string) (*FrontierSpawn, error) {
	m, err := findClaimableMap(d, cwd, mapID)
	if err != nil {
		return nil, err
	}
	return SpawnFrontier(d, cfg, m, 0)
}
