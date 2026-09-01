---
status: accepted
---

# An unanswered Work view preset field never removes a row

## Context

Page A of the **Work dashboard** holds two **Work kind**s — **Task set**s and
**Map**s — and one **Work view preset** selects the rows for both.
[ADR-0197](0197-work-view-presets-replace-the-inclusion-toggles.md) decision 1
fixed that preset's vocabulary as a closed field set, and decision 10 recorded
what the field set actually is: `status`, `unfolded` and `created_within` are
Task-set vocabulary. Routine presets were deferred on exactly those grounds.

Maps were wired into the same predicate anyway, and the seam has no way to say
"this kind has no answer for that field". `tasks.ViewFacts` is a flat struct of
answered values, so `wayfinder`'s projection supplies a **false** one:
`Unfolded: false` for every Map. The consequences were never decided, only
inherited:

- the shipped `unfolded` preset removes **every** Map, always — not because a Map
  has nothing to fold, but because a hardcoded `false` fails a positive test;
- a Map whose identifier carries no date is removed by `created_within` for the
  same reason, through a different route (`ok=false` reads as a failed match);
- `active`'s `hide {status = ["done"], unfolded = false}` can never fire on a
  Map, because `hide` is a conjunction and no Map status is `done` — so the
  Task-set-shaped clause silently no-ops on the kind it was never written for.

A read surface that removes rows by accident of a struct's zero value is a
defect, and the missing third state is what makes it one.

## Decision

**A **Work kind** states the fields it cannot answer, and an unanswered field
never removes a row.**

1. **Absence is expressed by leaving the field unset.** The optional fields land
   on `tasks.ViewFacts` — `unfolded` becomes optional, and an identifier with no
   date leaves `created_within` unanswered instead of failing its match. The kind
   already builds the projection, so an unset field *is* its declaration. A
   separate per-kind list of answerable field names was rejected: it is a second
   place to forget, and it can disagree with the facts beside it.

2. **A positive unanswered field is ignored by default.** A preset judges each
   row only on the fields that row's kind can answer. This is the **Unanswered
   filter field** rule, and `admit` is its default because the convenience of
   Task-set-shaped filtering should not silently cost a kind its visibility.

3. **`unanswered = "admit" | "drop"` is the opt-out, declared per preset entry,
   defaulting to `admit`.** `drop` restores the pre-ADR removal for a preset
   whose whole question is Task-set vocabulary. It follows `archived`'s precedent
   — a word with genuine states, not a bool — and it names the condition rather
   than a posture (`strict`), so `unanswered = "drop"` teaches the rule on the
   line where it appears.

4. **The shipped `unfolded` preset is the only entry that declares `drop`.** An
   **Unfolded Task set** is the entire subject of that view, and a Map can never
   be one. Every other shipped preset (`active`, `recent-7d`, `recent-30d`,
   `all`, `muted`) needs nothing: their fields are cross-kind, and `active`'s
   `hide` clause never fired on a Map under either rule. `active` was the obvious
   place to put the declaration and is the wrong one — it does not exercise it.

5. **A `hide` clause never fires on an unanswered field, and `unanswered` does
   not reach inside it.** The rule inverts across the positions because `hide` is
   an AND that *subtracts*: removing a term makes the clause match more rows, so
   ignoring an unanswered field inside `hide {unfolded = false}` would hide every
   Map. A subtraction must be proved on every field it names. Keeping the clause
   rule fixed under `drop` also stops `drop` removing a row twice — once by
   failing the positive test, once by firing a clause it could not evaluate. The
   key is therefore entry-level only; `hide` inherits the entry and carries no
   copy of its own, and `unanswered` inside a `hide` table is a finding
   ([ADR-0054](0054-config-validation-is-caller-scoped.md)).

6. **A kind-local answer in different words is an answer, not an absence.** A
   Map answers `status` with `active`/`arrived`. So a preset naming
   `status = ["blocked"]` still removes every Map, at every `unanswered` setting.
   This is the residual asymmetry of the decision and it is deliberate: mapping
   one kind's status vocabulary onto another's is the shared-status-facet
   modelling [ADR-0173](0173-work-is-one-kind-interface-with-data-shaped-returns-and-kind-side-adapters.md)
   refused, and this ADR ranks nothing across vocabularies.

## Consequences

- **Amends [ADR-0197](0197-work-view-presets-replace-the-inclusion-toggles.md).**
  Decision 1's closed vocabulary gains `unanswered`. Decision 5's derived
  `unfolded` predicate gains a third state for kinds that cannot hold a
  **Worktree binding**. Decision 10 deferred Routine presets partly because the
  fields are Task-set vocabulary; this decision removes that obstacle, but page B
  stays unwired — no Routine preset worth writing has been identified, and
  shipping one is a separate call.
- **`pop work status` keeps a coarser preference this ADR does not touch.** Page
  A prints under the caption `Task sets:` with `omit=mapRow`, so **every** Map is
  dropped from that table whatever the preset says. It is named here as a known
  asymmetry with ADR-0197 decision 7's "both surfaces, one vocabulary", and left
  alone.
- The **membership tiers** are unaffected: `work.Tier` reads only shared
  `Container` fields, so a live drain floats for any kind and no ordering
  question arises here
  ([ADR-0210](0210-work-rows-interleave-across-kinds-by-creation-date.md)).

## Considered options

- **Keep the false answer and special-case the `unfolded` preset.** Rejected: the
  defect is that a kind cannot state an absence, and one preset's exemption
  leaves the next field to rediscover it.
- **An unanswered field removes the row by default, with the new key admitting
  it.** Preserves today's behaviour exactly — and changes nothing in the shipped
  roster, since every other preset's fields are already cross-kind. It would ship
  as pure capacity for presets nobody has written, and would keep the accidental
  removal as the default meaning of an absence.
- **`kinds = ["task-set"]` on a preset.** Rejected: it answers a coarser question
  than the one that failed. The **Map** rows removed by `unfolded` were removed
  field-by-field, and a kind allow-list would also strip a Map from presets whose
  fields it answers perfectly well.
- **Map `unfolded` onto a Map-side notion of "finished but still held".** Rejected
  as the shared-status-facet modelling ADR-0173 refused; an **Arrived** Map holds
  no checkout to tear down, so the analogy has no referent.
