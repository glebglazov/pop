---
status: accepted
relates: "applies [ADR-0158](0158-work-dashboard-verbs-split-by-handoff.md)'s case rule to the Map's frontier verbs"
---

# A Map's frontier is worked as tiled panes in one window, fanned out on one key

Every Decision ticket being grilled is a **pane** in the single `map` window of
its **Map session**, tiled, with no overview pane and no per-ticket window.
`pop map fan-out <map-id>` spawns one such pane for every frontier ticket in one
act — HITL tickets included, each running the configured interactive agent in
skip-permissions mode — and is defined as looped `pop map next`, sharing that
verb's one spawn path rather than adding a second. Neither verb moves the
operator unless asked: `--focus` on the CLI, and on the Work dashboard's map row
`I`/`A` (go) against `i`/`a` (stay).

## Context

Wayfinding is a sitting, not a single question. A Map's frontier routinely holds
four or five tickets that no longer block each other, and the point of the
frontier is that they can all be walked at once — claims already make that safe.

The session model did not match. `OpenGrillingWindow` gave each ticket its own
tmux window named after the ticket file's stem, so an N-ticket sitting was N
windows, and nothing showed what was in flight: you cycled windows to find out
what you had running. Window 1 ran the Map's status render for exactly that
purpose and went unused, because it was a static render one keypress away from
the work rather than the thing you were looking at.

The spawn path also parked its claims on the wrong pane. A claim's owner is the
calling pane (`DefaultClaimOwner`), so `pop map next` claimed for *your* pane and
then spawned the agent into a different one; the agent's own `pop map claim` was
refused as "claimed by another pane" for the four-hour TTL. Invisible at N=1
because a human rarely re-claims what they were just handed — and N claims wide
the moment fan-out exists.

## Decision

- **Pane per ticket, one window.** Ticket agents are panes in the `map` window
  under `tiled`, tagged `@pop_ticket=<id>` and titled with the ticket stem. The
  tag is the reuse key: a live tagged pane is a jump target and is never re-sent
  work (ADR-0158); a bare-shell one is respawned. This is `EnsureTaggedPane` with
  its window name lifted to a parameter — the same primitive the `pop-work` drain
  window already uses.
- **No overview pane.** `pop map status <map-id>` stays a verb the human types.
  The session is born on the first spawn, so its initial pane is a ticket agent;
  opening a Map with nothing running lands in a bare shell at the Trunk, and the
  first spawn adopts that shell rather than splitting beside it.
- **No cap, no spill.** Every frontier ticket gets a pane. Cramped panes are
  answered with `prefix-z`, not with a second place to look.
- **Fan-out is looped `next`.** One spawn implementation; everything fan-out
  needs is a parameter on it. Each iteration claims atomically, so a ticket lost
  to a parallel session mid-loop costs one pane and nothing else. An empty
  frontier is a message and exit 0, not an error; a re-run tops up idempotently.
- **The spawned pane owns its claim.** `next` spawns first, then claims with the
  new pane's id as owner.
- **Staying put is the default.** Both verbs default to no focus; `--focus`
  switches to the `map` window, selecting no particular pane. On the dashboard
  the focusing variants are uppercase and the staying ones lowercase, which is
  ADR-0158's case rule applied unchanged — a verb that spawns without quitting
  the dashboard is an in-place verb.

## Considered Options

**Keeping window-per-ticket and adding a fan-out that opens N windows.** Rejected:
it multiplies the exact complaint — no single view of what is in flight — and
window cycling scales worse than a tiled wall.

**Keeping the overview as a reserved pane** (`main-vertical`, overview as the main
pane). Rejected: it is a render of state the human can ask for on demand, and
reserving screen for it takes width from the agents that need it. The operator
reports never having used it.

**A pane cap with spill into `map-2`, `map-3`.** Rejected: it reintroduces the
window cycling this decision removes, at exactly the frontier sizes fan-out
exists for.

**Batch-claiming the frontier in one transaction** instead of looping `next`.
Rejected: it needs a second claim path beside `ClaimFirstWorkItem` and buys
nothing — a mid-loop loss is already a benign outcome.

**A confirmation prompt before spawning N skip-permissions agents.** Rejected:
fan-out is only ever typed deliberately, and a prompt on the one verb whose
purpose is bulk would be friction at the wrong end.

## Consequences

- Spawning N interactive agents in skip-permissions mode is now one keystroke.
  That posture is accepted knowingly: the agents are grilling sessions on a Map,
  which writes nothing into the repository, and claims stop two of them landing
  on the same ticket.
- The **Grilling window** concept is retired in favour of **Grilling pane**, and
  **Map session** no longer promises an overview in window 1.
- `pop map next` no longer moves the operator by default — a behaviour change for
  anyone who typed it expecting to be taken there. `--focus` and dashboard `I`
  restore it.
- The claim-owner fix means claims now age out against the pane actually doing
  the work, so a killed agent pane releases its ticket on the intended clock.
- `pop map assist` (a map-scoped session with no ticket) inherits a window that
  already tiles panes, so it has somewhere obvious to live.
