# A Refine mark says whether the standard was applied, and gates nothing

The **Task set** audited in
[ADR-0259](0259-a-builder-proves-prior-art-before-it-writes-mechanism.md)
reached a human's sign-off with its one **Refine** run recorded as
`interrupted`. Nothing on any surface said so. The set looked exactly like a set
that had been refined and found clean.

That is a gap in what pop records, not in what it runs. A **Refine episode** is
written only for a refined outcome — by design, so a transient build failure
cannot cost a set its pass permanently — which means an interrupted,
gate-blocked or abandoned pass leaves the episode armed and no trace a reader
can see. The **Refine pointer** does not fill it either: it points at a report,
and these passes publish none.

## The mark

1. **Refinement joins completion and verification as an independent fact.** The
   **Verification mark** exists because one status slot cannot hold both "done"
   and "checked" — "done and nobody checked" is a different situation from "done
   and checked". Whether the changeset was held to the standard is a third such
   fact, and it gets a mark rather than a status value for the same reason.

2. **Two tiers, with the reason beside the mark rather than inside it.** The mark
   answers one question: was this changeset refined, yes or no. The four reasons
   a pass did not refine — interrupted, gate-blocked, abandoned, never ran — are
   *why*, not *what*, and to a human at a sign-off gate all four mean the same
   thing. Carrying five values into a status slot would push a reader into
   distinguishing `gate-blocked` from `abandoned` at a glance for no decision
   they are making.

3. **Gate-blocked is the reason worth surfacing in words.** It is the one that is
   not a statement about the pass at all: it says the scoped gate was already red
   when refinement began, which is a fact about the *work*, and one a human
   signing off very much wants.

4. **No new column.** The mark is derived from what is already recorded — a
   Captured run of phase `refine` exists or it does not, and its outcome says
   which. `refine_episodes` stays exactly as it is; reading it alone is what
   cannot distinguish "armed because it failed" from "armed because new work
   landed".

5. **Resolved once, read everywhere.** It resolves beside **Verified status
   resolution**, not per surface, so the HITL sign-off gate's preamble, the
   detail view, the paging entry and the Assist prompt cannot disagree about
   whether a set was refined. This is the Verification mark's own route, and the
   reason is the same: a fact four surfaces render is one derivation, or it is
   four that drift.

6. **It gates nothing.** `refine_phase.go` hands back no directive but the
   human's interrupt, and that stays true. Gating a **Drain** on a pass
   completing would park sets on an agent hiccup — trading a failure that costs
   one review for a failure that costs the drain. A mark is the honest middle: it
   says the check did not happen, refuses nothing, and puts the fact in front of
   the only person who can act on it.

## Consequences

- **The log, the cache and the mark all count differently, and that is
  expected.** Reports outlive the invalidation that deletes verdict rows, a
  cached verdict publishes nothing, and now a mark can read "not refined" over a
  set whose episode is armed. Each answers a different question.
- **A human completion is unaffected.** The drain already skips refining a set a
  human declared done. Such a set carries the mark like any other, saying plainly
  that nothing held it to the standard — which is the truth.
