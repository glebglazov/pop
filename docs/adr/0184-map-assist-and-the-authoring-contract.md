---
status: accepted
relates: "splits its rules across the seam [ADR-0183](0183-authoring-rules-are-owned-by-the-binary.md) drew, and adds a pane to the window [ADR-0182](0182-frontier-fan-out-panes.md) defined"
---

# A Map assist session writes the map's shape and never resolves

## Context

Wayfinding has one way in: `pop map next` claims a frontier ticket and spawns a
pane to grill it. That serves the case where the work is a *decision*, but not the
case where an idea arrives about the map itself — new scope for an existing
ticket, a fresh ticket, a patch of fog, or the realisation that something sits
past the destination. Today that thought has nowhere to go except a ticket
grilling session, which mis-files it under whatever ticket happened to be open.

A map-scoped session is unclaimed and unscoped by construction, which is what
makes it worth an ADR. The one-non-research-ticket-per-session rule is what keeps
a Map's decisions traceable to the conversations that made them, and a session
holding the whole map — every ticket in reach, no claim to bound it — is the one
place that rule can be broken silently.

It cannot be enforced in code. `pop map resolve` has no way to tell a claimed
ticket pane from an assist pane, and gating on the claim would be wrong, since
resolving from a Task-set pane is legitimate (ADR-0158's in-place writes). So the
boundary has to be a stated rule the agent reads.

Where such rules live is already settled and is **not** re-decided here:
[ADR-0183](0183-authoring-rules-are-owned-by-the-binary.md) gives each Work kind
an authoritative `authoring-guide` verb carrying mechanics and judgment prose
alike, and leaves the Work-store doc carrying behavioural rules only — claiming,
resolution, handoff. This decision places assist's rules across that existing seam
and says what they are.

## Decision

**Add `pop map assist <map-id>`: an attended agent session scoped to the Map, not
to any ticket.** It claims nothing and resolves nothing.

### The write surface

Allowed from any wayfinding session, assist or charting:

- create tickets, including graduating fog into them
- amend a ticket's `## Question`
- wire and unwire `blocked_by`
- edit `map.md`'s `Destination`, `Notes` and `Not yet specified`
- `pop map out-of-scope` a mis-scoped ticket

Forbidden: `pop map resolve`, anything between the `pop:generated` markers, and
the repository under study.

`pop map out-of-scope` sits on the allowed side **despite flipping a ticket to
`resolved`**, because it is a scoping act rather than a step on the route: it
renders under `Out of scope`, never into the decision index, and "that is past the
destination" is exactly the realisation that arrives with no ticket in hand.
Redrawing `Destination` is the same family of act one level up, which is why
assist may do that too.

**The contract is flat, not per-mode.** Charting and assist write the same set;
there is no permission charting holds alone. What separates the modes is the
conversation being had, which belongs to the skill.

### The rules land on both sides of the existing seam

Split by ADR-0183's own line — the guide describes the artifact, not a workflow:

- **`pop map authoring-guide`** carries the artifact half: which files and regions
  a session may write, and that the `pop:generated` regions and the repository are
  off-limits.
- **The Work-store doc's `### Resolution`** carries the workflow half: an assist
  session never resolves and hands the human `pop map next <map-id> <NN>`, and it
  closes by re-running `pop map register <map-id>`. Both sit directly beside the
  one-non-research-ticket-per-session rule they exist to protect, which that
  section already owns.

Re-validating on close matters because assist's structural writes are
hand-written files: `register` is re-runnable and prints the whole fix list, and it
is the safety net ADR-0183 leaned on when it rejected a JSON authoring API. Assist
is otherwise the write path that could leave a Map broken.

### Mechanics

One pane per Map in the **Map session**'s single `map` window, tagged
`@pop_assist`, reusing ADR-0182's `EnsureTaggedPane`/`FindTaggedPane`: a live pane
is a jump target and is never re-sent work (ADR-0158), a bare shell is respawned.
Structural writes take the existing per-Map file lock the resolve path uses.
Skill: `wayfinder` in a third mode, `assist`, alongside chart and work, seeded by
the CLI verb. Dashboard key `S` (now `A`, and `MapKind.Actions` `I W A O` then `i w y` —
see ADR-0158's 2026-09-05 amendment) — matching `pop tasks assist`'s key,
uppercase per
ADR-0158 because it quits the dashboard — ungated by frontier size, since a Map
with an empty or fully-claimed frontier is when assist is most needed.
`MapKind.Actions` becomes `I A S O` then `i a y`.

## Considered Options

**A separate skill instead of a third `wayfinder` mode.** A cleaner boundary: an
assist skill physically cannot contain work mode's resolve flow. Rejected because
it would duplicate the whole map model, the tracker-doc resolution and the
refer-by-name discipline, and assist's write surface turns out to be charting's
write surface applied to a live map. The boundary it buys is one sentence of prose
either way.

**Per-mode permission tables in the guide.** Rejected: the only candidate
asymmetry was naming the `Destination`, and assist may redraw that too. A table
with identical rows is worse than no table.

**A singleton claim row so two assist sessions cannot race.** Rejected as the
wrong layer. The per-Map lock already protects `index.json`, and the prose race is
dissolved by the reused tagged pane — there is no second assist session, because
asking for one lands you in the first. A claim row would also need a TTL and a
release path for a session holding no ticket.

**Keeping the whole write surface in the guide**, as this ticket's grilling first
settled. Reversed once ADR-0183 landed mid-session: two of the rules are
workflow, and their sibling — one non-research ticket per session — is prose the
doc keeps. Putting them in the guide would make the guide carry workflow, which is
the line that ADR drew.

**A lowercase `s` twin, spawn-and-stay.** Rejected: `i`/`a` earn stay-put twins
because fanning out N tickets is followed by more triage, whereas assist is one
pane you intend to go talk to.

**`chat` or `edit` as the verb.** Rejected. `chat` names the medium, not the job;
`edit` undersells reading around. `pop tasks assist`'s own help — "open an Assist
session on a task set at its current status (no drain, no Verifier)" — is already
the container-scoped, runs-no-automated-verb sense this needs, so the map analogue
is exact rather than borrowed.

## Consequences

- The one-non-research-ticket-per-session rule gains a written guard at the one
  place it could be broken silently. It stays prose, not enforcement: an assist
  session that runs `resolve` will succeed. What changes is that the rule is
  stated where the session reading it will look.
- Assist's rules are split across the guide and the doc. This is the seam
  ADR-0183 accepted, not a new one — a skill reads both surfaces already, because
  claiming, resolution and handoff stay in the doc regardless.
- `MapKind.Actions` gains `S`, ungated. The Map kind now has a container action
  that works on a Map in any state, which no other Map key does.
- A third `wayfinder` mode means work mode's resolve flow must be fenced
  explicitly in the skill, since the two modes share a file.
