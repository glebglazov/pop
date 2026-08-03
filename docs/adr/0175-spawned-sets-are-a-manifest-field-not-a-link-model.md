---
status: accepted
---

# Spawned sets are a field on the Map manifest, not a link model

## Context

Ticket 04 of the *generalize-work* map asked how Work containers of different
kinds refer to each other, now that Maps, Task sets and Routines are all
registered Work behind one **Work kind** seam
([ADR-0173](0173-work-is-one-kind-interface-with-data-shaped-returns-and-kind-side-adapters.md)).
The obvious shape was a general one: typed directed edges between Work refs, a
`work_edges` table keyed `(from_ref, type, to_ref)`, with a Map's handoffs as one
edge type among the several a growing system would surely want.

The premise did not survive inspection. Exactly **one** relationship is ever
traversed by a human: a Map, and the Task sets it spawned — read from the Map, to
answer "did the work I found the way to actually land?". Nothing traverses it in
reverse, nothing traverses between two Task sets, and nothing traverses between a
Routine and anything. A one-way relationship with one owner is a field on the
owner, and a graph model for a single field buys generality by paying in
concepts: an edge type vocabulary, a direction to reason about at every call
site, ref pairs to validate, and orphan edges to reconcile when a container goes
away.

## Decision

**`spawned_sets` is a bare array of Task-set ids on the Map manifest.** No edges
table, no edge types, no ref pairs, no reverse direction. The array holds ids and
nothing else — no titles, no timestamps, no statuses — because each of those
would be a cached copy of another file's truth, and a copy drifts. A spawned
set's live status is read fresh at render time.

Storing the link in the Map's `index.json` rather than in pop.db makes it
**file-canonical and portable**: it travels with the Map's folder, so a Map moved
to another machine keeps its lineage, while a pop.db edges table would make
lineage machine-local and lose it on exactly the move a per-machine store makes
routine.

**`pop map` owns the write.** `pop map spawned <map-id> <task-set-id>` appends the
id — idempotently, so a re-run over an already-recorded set is a no-op — and
re-renders `## Spawned sets` from the manifest. `to-tasks` calls it after
`pop tasks register` succeeds. `pop tasks register --source-map <map-id>` was
rejected: it would make `tasks` read and write wayfinder's storage layout, which
is the import direction the kind seam exists to forbid.

**The set-side half is `source_map` on the set's own `index.json`**, written on
every Map-sourced set whether or not a spec exists, so the back-link is never
half-built for a spec-less set. `spec.md`'s `Source map:` line stays human-facing
prose: nothing parses it and nothing derives from it, so the two never disagree
about which one is authoritative.

**`## Spawned sets` becomes a pop-generated region** like `Decisions so far` and
`Out of scope` ([ADR-0172](0172-pop-owns-the-wayfinding-lifecycle-and-pop-wayfinder-becomes-pop-map.md)),
rendered from the manifest. Until now a skill hand-wrote both the section and,
notionally, the array — two writers for one fact, which is the drift the manifest
exists to kill.

## Consequences

- **No backfill.** The `## Spawned sets` convention has never actually been used,
  so the first generated rewrite of a Map discards anything hand-written under
  that heading. That is the accepted cost of the marker-less-section takeover
  rule, not a special case for this section.
- **A recorded id is never validated against the Task store, and never pruned.**
  A Map is a historical record of what an effort spawned; a set that is later
  archived, moved or deleted still reads back, rendering `(missing)` where it
  cannot be resolved.
- **A second relationship would need this decision revisited, not extended.** The
  right move then is a second field on its owner, not a retrofit of the graph
  rejected here — unless several relationships arrive at once, which is the only
  condition under which the edge table's generality would have paid for itself.
