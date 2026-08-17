---
status: accepted
relates: "amends decision 7 of [ADR-0209](0209-an-attributed-pane-pins-its-rows-to-the-top-and-says-nothing-else.md) for the human's own mark, extends the kind-owned verb seam of [ADR-0173](0173-work-is-one-kind-interface-with-data-shaped-returns-and-kind-side-adapters.md) with a declared capability, and inherits the kill rules of [ADR-0205](0205-the-monitored-set-is-the-set-of-panes-the-dashboard-may-kill.md) unchanged"
---

# A Selection is a human mark that outranks a preset, and it turns verbs plural

`tab` marks a row on the **Work dashboard** and the **Dashboard**. Marked rows lift
into a **Selection area** at the top of the list, and while any are marked the surface
is in **Selection mode**: every action is plural and targets the **Selection** rather
than the cursored row.

## Context

Two operations wanted it. Killing a dozen agent panes meant twelve `ctrl+x` presses
and twelve `y`s. Moving a batch of Task sets to a status meant leaving the dashboard
for `pop tasks complete`, which has done multi-select from the CLI for a long time —
so the capability existed and only the surface was missing.

The interesting question was never how to mark rows. It was what a mark means when the
view is filtered, and pop had already answered the neighbouring question the other way.

## Decision

**1. A Selection is run-scoped and keyed, never persisted.** It lives in one command
invocation. Both surfaces already rebuild their row slice wholesale on every poll and
already carry a stable per-row key — a pane id, a container's cursor key — so the
Selection is a set of those keys and survives a rebuild for free. Members keep their
own domain nouns; there is no cross-surface "unit".

**2. Marked rows are exempt from hiding, which amends ADR-0209 decision 7.** That
decision reads *presets and filters are absolute*: a row the active **Work view preset**
or the live query hides is not pinned, and pinning never widens either. A Selection
does widen both. The asymmetry is the whole point and is not an inconsistency: decision
7 constrains **pop's inference** — attribution guessing which rows are yours — and a
guess must not overrule a preset the human deliberately chose. A Selection is not a
guess. It is the human pressing `tab` on a named row, at least as deliberate as the
preset and strictly later. So pop's inference still yields to the filter and the human's
mark does not.

This is what buys the design its simplicity. Because a marked row can never be hidden,
there is no *selected but invisible* state, no accounting of how many members a verb
will really touch, and no `3 of 5 selected (2 hidden)` phrasing anywhere. An earlier
draft of this feature had all three.

**3. The Selection area is a region, not a marker.** Selected rows are moved — never
copied, one row one key — into a reserved region above the **Pane pin** block, separated
by a dim `N selected` line. They sit in the list's own sort and kind precedence rather
than in marking order, so the area reads as the same list filtered to the marks; marking
order is invisible state nobody can reconstruct later. The cursor never lands there by
default or after a rebuild, but `j`/`k` walk in, which is how a row gets unmarked. The
area is capped at a third of the viewport and overflows into a `… +N more selected`
line — a rendering limit only, since the count is stated and every member is still a
target.

There is no per-row glyph. `ui.List.renderPrefix` spends exactly two cells on every row,
and both are taken: the cursor block, and `▸` for the pane pin on the Work dashboard or
the quick-access digit on the monitor. Position is the mark, which is the same move
ADR-0209 made when it replaced an attribution sentence with an ordering.

**4. Selection mode is derived, and it is a true mode.** It *is* "the Selection is
non-empty" — no second flag, so nothing can desync and fire a verb at the wrong target.
Within it every action is plural; an action that cannot be plural refuses out loud
rather than going inert, because a silent key is indistinguishable from a bug.
**Navigation** is never gated: cursor movement, search, presets, the page toggle and
help stay live in every mode. `gg` and `G` become region-aware, reaching the edge of the
region the cursor is in before the edge of the whole list.

**5. An action declares its own capability; the default is singular.** `work.Action`
gains the modes it works in, filled by the kind that owns the verb, and the monitor's
keymap carries a mirrored table. Silence means singular, so bulk is granted one verb at
a time by someone writing it down rather than by a blanket rule — and the initial grant
list is a reviewable audit rather than an invisible default.

The line for the dozen verbs the dashboard intercepts before they reach `Perform` is
**whether the modal's input is per-row or shared**. Mute, unmute and abandon go plural:
one duration or one confirmation answers identically for every marked row. Drain,
verify, bind, unbind and the Map fan-out verbs stay singular, because each resolves a
checkout, worktree or session per row, so one modal cannot answer for the set — and
several hand off to a pane, which has no plural meaning at all.

**6. Bulk is a loop over `Perform`, not a new seam.** Cross-container atomicity is not
achievable — separate manifests, separate repositories — so a batch entry point would
promise a transaction it could not honour while adding a second method every future kind
must implement. Failures do not abort the loop. The **Flash message** carries the reason
when exactly one row failed and a bare count when several did; on success the Selection
is consumed, and on partial failure it collapses to exactly the rows that failed, so a
retry needs no re-marking and the shrinking set surfaces each reason in turn.

**7. Every bulk write is confirmed, single-row behaviour is untouched.** Two or more
rows brings up the monitor's existing inline `y/N` hint-line grammar. A mistaken `c` on
one set costs one `o`; a mistaken archive over twelve costs twelve corrections. The
monitor's kills keep honouring `kill_pane_prompt_enabled` in both directions — someone
who turned it off made a standing decision about this exact risk under ADR-0205, and
re-prompting them would be pop overruling a setting they wrote. ADR-0205's narrower
rules survive intact: your own pane is skipped rather than failing the batch, the
Selection does not exist in `--pick` picker mode, and the prompt answers against the row
set captured when it opened.

**8. `tab` guarantees an outcome, not a mechanism.** After `tab`, the cursor sits on the
next row — by the marked row leaving the list in the dashboards, and by an explicit
advance in `ui.MultiSelect`, which has nowhere for a row to go. `shift+tab` clears the
whole Selection and thereby leaves the mode. `space` is retired as a selection toggle;
`space` as *activate the highlighted item* is a different act and stays.

## Considered options

**Retarget only the mutating verbs, leaving inspection on the cursor.** The first
proposal: the Selection is what you change, the cursor is what you look at, so `l` still
opens one detail view mid-selection. Rejected in favour of a true mode because the rule
"some keys follow the marks and some follow the cursor" has no visible boundary — the
user has to remember the classification. A mode you can see in the hint line is one
fact; a split target is a table.

**Deselect on preset or search change.** Simpler, and the honest reading of ADR-0209
decision 7. Rejected: it makes the Selection a thing you lose by navigating, which is
what marking rows is *for*.

**Keep the selection and gate the verb on visibility.** Marks persist, hidden members
are silently skipped or loudly counted. This is what decision 2 replaces, and it is
where most of the complexity of this feature lived — the counting, the phrasing, the
partial-application semantics. The area dissolved it.

**A per-row glyph in the prefix column.** No cell free on either surface, and on the
Work dashboard it would compete with the pane pin on a row that is both marked and
attributed.

**Derive capability from the verb's outcome kind.** Impossible: the outcome is known
only after `Perform` runs, and the menu is drawn before.

**Lift the monitor onto the Work kind seam** so both surfaces declare actions the same
way. Rejected: the monitor's verbs are callbacks over a pane id with no kind, container
or `Perform`, so unifying means inventing a kind seam for a surface with one kind, and
the payoff is that two small tables look alike.

## Consequences

- A row that is both marked and attributed lives in the Selection area and keeps its
  `▸`, so selecting it does not cost the attribution fact.
- Page A and page B hold independent Selections — they are separate models — and both
  survive the `v` toggle.
- `tab` is inert in the detail view. Item-level bulk is deliberately out of scope: a
  whole-set verb already means every unlocked task, and `pop tasks complete` already
  does per-task multi-select, so a third path to the same write buys nothing.
- `ui.MultiSelect` inherits the key and the cursor invariant and nothing else — no area,
  no mode word. A reserved area needs something to protect rows from, and a modal with
  no filter, search or sort has nothing.
- A bulk kill that empties the monitor quits it, exactly as a single kill does.
