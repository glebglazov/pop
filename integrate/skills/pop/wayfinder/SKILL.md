---
name: wayfinder
description: Plan a huge chunk of work — more than one agent session can hold — as a shared map of decision tickets, and resolve them one at a time until the way to the destination is clear.
disable-model-invocation: true
---

<!--
base: mattpocock/skills engineering/wayfinder@8b78b53

This file is a marked overlay. Everything from here down to the "POP OVERLAY"
marker is a byte-verbatim copy of upstream engineering/wayfinder/SKILL.md body at
the pinned ref above. Pop inlines rather than delegating to Matt's skills, per
ADR-0009 (skills are embedded in the binary and ship to machines without them
installed). Pop-authored frontmatter is kept (name `wayfinder`, pop description,
`disable-model-invocation`): **human-opened**: wayfinding is a manual-only
session the user opens with `/wayfinder`, never something the model starts on
its own — a huge chunk of work gets charted only when a human decides it's
time. Pop's
Work-store seam and its irreducible invocation deltas live below the marker
(ADR-0136, ADR-0169). Upstream already routes the map, its tickets, blocking,
claiming, resolution, and the frontier through the tracker doc's "Wayfinding
operations" section (see "The Map"); the pop doc now carries those, so the
overlay only names which doc and states the deltas with no home there. To
review upstream drift, diff the region between this header and the marker
against engineering/wayfinder@<newref>.
-->

A loose idea has arrived — too big for one agent session, and wrapped in fog: the way from here to the **destination** isn't visible yet. Wayfinding is about finding that way, not charging at the destination. This skill charts the way as a **shared map** on the repo's issue tracker, then works its **decision tickets** — questions whose resolution is a decision, not slices of a build to execute — one at a time until the route is clear.

The destination varies per effort, and naming it is the first act of charting — it shapes every ticket. It might be a spec to hand off and iterate on, a decision to lock before planning starts, or a change made in place like a data-structure migration. The map is domain-agnostic — engineering work, course content, whatever fits the shape.

## Plan, don't do

Wayfinder is **planning** by default: each ticket resolves a decision, and the map is done when the way is clear — nothing left to decide before someone goes and does the thing. The pull to just do the work is usually the signal you've reached the edge of the map and it's time to hand off. An effort can override this in its **Notes** — carrying execution into the map itself — but absent that, produce decisions, not deliverables.

## Refer by name

Every map and ticket is an issue, so it has a **name** — its title. In everything the human reads — narration, the map's Decisions-so-far — refer to it by that name, never by a bare id, number, or slug. A wall of `#42, #43, #44` is illegible; names read at a glance. The id and URL don't vanish — a name wraps its link — but they ride _inside_ the name, never stand in for it.

## The Map

The map is a single issue on this repo's issue tracker, labelled `wayfinder:map` — the canonical artifact. Its tickets are child issues of the map.

The map is an **index**, not a store. It lists the decisions made and points at the tickets that hold their detail; a decision lives in exactly one place — its ticket — so the map never restates it, only gists it and links.

**Where the map, its child tickets, blocking, and frontier queries physically live is tracker-specific.** The issue tracker should have been provided to you — run `/setup-matt-pocock-skills` if not. Consult the tracker doc's "Wayfinding operations" section for how _this_ repo expresses them. If no tracker has been provided, default to the local-markdown tracker.

### The map body

The whole map at low resolution, loaded once per session. Open tickets are **not** listed — they are open child issues, found by query.

```markdown
## Destination

<what reaching the end of this map looks like — the spec, decision, or change this effort is finding its way to. One or two lines; every session orients to it before choosing a ticket.>

## Notes

<domain; skills every session should consult; standing preferences for this effort>

## Decisions so far

<!-- the index — one line per closed ticket: enough to judge relevance, then zoom the link for the detail the ticket holds -->

- [<closed ticket title>](link) — <one-line gist of the answer>

## Not yet specified

<!-- see "Fog of war": in-scope fog you can't ticket yet; graduates as the frontier advances -->

## Out of scope

<!-- see "Out of scope": work ruled beyond the destination; closed, never graduates -->
```

### Tickets

Each ticket is a **child issue** of the map; the tracker's issue id is its identity. Its body is the question, sized to one 100K token agent session:

```markdown
## Question

<the decision or investigation this ticket resolves>
```

Each ticket carries a `wayfinder:<type>` label — one of `research`, `prototype`, `grilling`, `task` (see [Ticket Types](#ticket-types)).

A session **claims** a ticket by assigning it to the dev driving the map, **first**, before any work, so concurrent sessions skip it. That assignee _is_ the claim: an open, unassigned ticket is unclaimed.

Blocking uses the tracker's **native** dependency relationship — essential because it renders the frontier _visually_ in the tracker's own UI, so the human sees what's takeable without opening the map. Only a tracker that lacks native blocking falls back to a body convention. A ticket is **unblocked** when every ticket blocking it is closed; the **frontier** is the open, unblocked, unclaimed children — the edge of the known.

The answer isn't part of the body — it's recorded on resolution (see [Work through the map](#work-through-the-map)). Assets created while resolving a ticket are linked from the issue, not pasted in.

## Ticket Types

Every ticket is either **HITL** — human in the loop, worked _with_ a human who speaks for themselves — or **AFK**, driven by the agent alone. A HITL ticket only resolves through that live exchange; the agent never stands in for the human's side of it (a grilling agent that answers its own questions has broken this).

- **Research** (AFK): Reading documentation, third-party APIs, or local resources like knowledge bases to surface a fact a decision waits on. Resolved by a `/research` **subagent**. Use when knowledge outside the current working directory is required.
- **Prototype** (HITL): Raise the fidelity of the discussion by making a cheap, rough, concrete artifact to react to — an outline, a rough take, a stub, or UI/logic code via the /prototype skill. Links the prototype as an asset. Use when "how should it look" or "how should it behave" is the key question.
- **Grilling** (HITL): Conversation. The default case. Always invoke the /grilling and /domain-modeling skills.
- **Task** (HITL or AFK): Manual work that must happen before a _decision_ can be made — nothing to decide, prototype, or research, but the discussion is blocked until it's done. Signing up for a service so its API can be judged, provisioning access, moving data so its shape can be seen. This is the one type that _does_ rather than decides — and it earns its place by unblocking a decision, not by delivering the destination. The agent drives it alone where it can (AFK); otherwise it hands the human a precise checklist (HITL). Resolved when the work is done; the answer records what was done and any resulting facts (credentials location, new URLs, row counts) later tickets depend on.

## Fog of war

The map is _deliberately_ incomplete: don't chart what you can't yet see. Beyond the live tickets lies the **fog of war** — the dim view of decisions and investigations you can tell are coming but can't yet pin down, because they hang on questions still open. Resolving a ticket clears the fog ahead of it, graduating whatever's now specifiable into fresh tickets — one at a time, until the way to the destination is clear and no tickets remain.

The map's **Not yet specified** section is where that dim view is written down: the suspected question, the area to revisit later. It's the undiscovered frontier _toward_ the destination — everything here is in scope, just not sharp enough to ticket. Write as loosely or as fully as the view allows; it doubles as a signpost for collaborators reading where the effort is headed.

**Fog or ticket?** The test is whether you can state the question precisely now — _not_ whether you can answer it now.

- **Ticket when** the question is already sharp — even if it's blocked and you can't act on it yet.
- **Not yet specified when** you can't yet phrase it that sharply. Don't pre-slice the fog into ticket-sized pieces: it's coarser than a ticket, and one patch may graduate into several tickets, or none, once the frontier reaches it.

**Not yet specified** excludes what's already decided (Decisions so far), what's already a live ticket, and what's out of scope (the next section).

## Out of scope

Fog only ever gathers _toward_ the destination. The destination fixes the scope, so work beyond it is **out of scope** — it isn't fog, and it doesn't belong in **Not yet specified**. It gets its own **Out of scope** section on the map: work you've consciously ruled out of _this_ effort. Scope, not sharpness, lands it here.

Out-of-scope work never graduates — the frontier stops at the destination — so it returns only if the destination is redrawn, and then as a fresh effort, not a resumption.

Ruling something out of scope is a scoping act, not a step on the route. When a ticket that already exists turns out to sit past the destination — mis-scoped in while charting, or exposed by a resolution — **close it** (a closed ticket is unambiguously off the frontier) and leave one line in the **Out of scope** section: the gist plus why it's out of scope, linking the closed ticket. It stays out of **Decisions so far**, which records the route actually walked — a scope boundary isn't a step on it.

## Invocation

Two modes. Either way, **never resolve more than one ticket per session** — with the exception of research tickets.

### Chart the map

User invokes with a loose idea.

1. **Name the destination.** Run a `/grilling` and `/domain-modeling` session to pin down what this map is finding its way to — the spec, decision, or change. The destination fixes the scope, so it's settled first.
2. **Map the frontier.** Grill again, **breadth-first** this time: fan out across the whole space rather than deep on any one thread, surfacing the open decisions and the first steps takeable now. **If this surfaces no fog** — the way to the destination is already clear, the whole journey small enough for one session — you don't need a map. Stop and ask the user how they'd like to proceed.
3. **Create the map** (label `wayfinder:map`): Destination and Notes filled in, Decisions-so-far empty, the fog sketched into **Not yet specified**.
4. **Create the tickets you can specify now** as child issues of the map — then wire blocking edges in a **second pass** (issues need ids before they can reference each other). Wiring sorts them into the frontier and the blocked; everything you can't yet specify stays in the fog — the **Not yet specified** section.
5. **Fire the research subagents.** For each `research` ticket you just created, spin up a `/research` subagent to resolve it in parallel, capturing its findings on a throwaway `research/<name>` branch with a context pointer from the ticket.
6. Stop — charting is one session's work; it hand-resolves nothing.

### Work through the map

User invokes with a map (URL or number). A ticket is **optional** — without one, you pick the next decision, not the user.

1. Load the **map** — the low-res view, not every ticket body.
2. Choose the ticket. If the user named one, use it. Otherwise take the first frontier ticket in order. **Claim it**: assign it to yourself before any work.
3. Resolve it — **zoom as needed**: fetch the full body of any related or closed ticket on demand; invoke the skills the `## Notes` block names. If in doubt, use `/grilling` and `/domain-modeling`.
4. Record the resolution: post the answer as a **resolution comment**, **close** the issue, and **append a context pointer** to the map's Decisions-so-far.
5. Add newly-surfaced tickets (create-then-wire); graduate any fog the answer has made specifiable, clearing each graduated patch from **Not yet specified** so it lives only as its new ticket. If the answer reveals a ticket — this one or another — sits beyond the destination, **rule it out of scope** rather than resolving it on the route. If the decision invalidates other parts of the map, update or delete those tickets.

The user may run unblocked tickets in parallel, so expect other sessions to be editing the tracker concurrently.
<!-- ═══════════════════════════════ POP OVERLAY ═══════════════════════════════
Everything below is pop-specific and replaces the tracker-doc seam in the
verbatim region above. Upstream already routes the map, its tickets, blocking,
claiming, resolution, and the frontier through the tracker doc's "Wayfinding
operations" section (see "The Map"); the pop doc now carries those, so this
overlay only resolves which doc and states the deltas that live in upstream's
`## Invocation` flow rather than behind that seam (ADR-0136, ADR-0169). Where
a line below contradicts the verbatim upstream region, the line below wins;
the upstream text is kept byte-intact only so drift stays diffable.
-->

## Issue tracker doc resolution

Ignore upstream's "run `/setup-matt-pocock-skills`" line and its default
local-markdown tracker. Resolve the issue-tracker doc two-layer instead:

1. If the repo has `docs/agents/issue-tracker.md`, that doc wins.
2. Otherwise read `~/.agents/docs/issue-tracker.md`.
3. If neither exists, stop and tell the user no issue-tracker doc is
   configured — there is no further fallback.

Resolve this repository's Task-storage root once per session with
`pop work show-path`; the doc lays maps out under a `maps/` sibling of that
root's `tasks/`.

Everywhere the verbatim region above defers to "the tracker doc's 'Wayfinding
operations' section", and wherever it names a tracker-native operation —
labelling `wayfinder:map`, assigning a ticket to claim it, native blocking
edges, posting a resolution comment and closing the issue, querying the frontier
— read it through the resolved doc's **"Wayfinding operations"** section. That
section owns every pop mechanic: the storage layout, the `map.md` and
ticket-file shapes, claiming, resolution and its ticket-type overrides, the
out-of-scope rule, the one-non-research-ticket-per-session rule, and handoff to
`to-spec` / `to-tasks`. None of it is restated here; consult the doc.

## Assist the map — a third mode

*(Upstream has two modes, chart and work. pop adds a third, seeded by
`pop map assist [<map-id>]`, for the idea that arrives about the **map itself**
with no ticket in hand: new scope for an existing ticket, a fresh ticket, a patch
of fog, or the realisation that something sits past the destination.)*

User invokes as `assist <map-id>`. The session is scoped to the whole map and
holds **no ticket**.

1. Load the map — the low-res view — and read the frontier. Zoom into any ticket
   body on demand.
2. Have the conversation, then write what it settles: create tickets (graduating
   fog out of **Not yet specified** as you go), amend an existing ticket's
   `## Question`, wire and unwire blocking edges, edit the map's prose sections,
   and `pop map out-of-scope` anything the conversation puts past the
   destination — including redrawing the **Destination** itself. This is the same
   write surface *Chart the map* has; run `pop map authoring-guide` for the file
   shapes, and see the tracker doc's **Resolution** section for the workflow
   rules.
3. Close by running `pop map register <map-id>` and working its fix list until
   clean. Your writes are hand-written files; this is what stops a structural
   edit leaving the map broken.

**Assist resolves nothing.** Step 4 of *Work through the map* — post the answer,
close the ticket, append to Decisions-so-far — is **fenced off from this mode**,
and so is `pop map resolve`. A session with no ticket in hand has no ticket to
resolve, and that is what keeps one non-research ticket per session traceable to
the conversation that decided it. When the conversation turns out to want a
decision made, hand the human `pop map next <map-id> <NN>` and stop.
`pop map out-of-scope` is not an exception to this: it is a scoping act, and it
renders under **Out of scope**, never into the decision index.

## Skill and invocation deltas

*(Irreducible pop bits: these live in upstream's `## Invocation` flow, not behind
the tracker-doc seam, so the resolved doc has no place to carry them.)*

- **Charting skills.** Where upstream's *Chart the map* runs `/grilling` and
  `/domain-modeling` (steps 1–2) to name the destination and map the frontier,
  run pop's `grill-with-map` instead — the wayfinding grilling skill, which
  writes only into the Map's own directory. Never `grill-with-docs`: its
  contract mandates repository fragment writes and a commit at session close,
  which is the one thing wayfinding must not do. The `research` subagents fired in step 5
  write their findings into the ticket's `## Answer` (per the doc's research
  override), **not** onto a throwaway `research/<name>` branch.
- **Invocation form.** `/wayfinder` charts a new map from a bare loose idea
  (no map id), works an existing one as `work <map-id> [<ticket-id>]` — the
  ticket id optional, defaulting to the first frontier ticket — and assists one as
  `assist <map-id>`, which names no ticket at all.
- **Mode fencing.** The three modes share this file, so read only the one you were
  invoked in: work mode's resolve flow belongs to work mode, and an assist session
  running it breaks the one-non-research-ticket-per-session rule silently.
