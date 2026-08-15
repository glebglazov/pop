---
status: accepted
---

# An attributed pane pins its rows to the top of the Work dashboard, and says nothing else

> **Relates:** amends [ADR-0201](0201-a-pane-is-attributed-to-work-kind-side-and-seeds-the-dashboard-cursor.md).
> Its decisions 1, 3 and 7 (the attribution ladder, answered kind-side; no opt-out) stand
> unchanged. Its decisions 2, 4, 5 and 6 are replaced here, and the cursor mechanism they
> served — **Pane-seeded cursor** — is retired in favour of the **Pane pin**.

**Pane work attribution** now expresses itself as position, not as prose. The rows a pane is
attributed to are lifted to the top of the Work dashboard and marked; nothing is ever printed
about it.

## Context

ADR-0201 gave attribution one consumer: a cursor placed once at first render. Because a
cursor moved without explanation is indistinguishable from a broken feature, it also gave
attribution two status-line messages — one naming which of several bound sets was chosen and
why, one naming a container whose row the active preset had hidden.

In use, those lines are the feature's weakest part. They ride the three-second flash
(ADR-0204), so they are read out of the corner of the eye; they fire from the ladder's weakest
rung, which is also the rung that fires most often; and their content is about pop's internal
tiebreak rather than about the human's work. A sentence that says *it is the topmost bound row
under the current sort* explains a choice the human never asked pop to make.

Position carries the same information without a sentence. Putting the attributed rows first
answers "which of these is me?" by where the row sits, which is where the eye already goes.

## Decision

**1. Attribution pins rows; it does not move the cursor.** The pin is applied *after* the
sort resolves: attributed rows are lifted out of the ordered list and rendered first, the rest
of the list unchanged beneath them. This is deliberately not a sort term. Rows are never
ordered across kinds — `work/snapshot.go` orders kinds by precedence and sorts within each —
so an attributed Map row must jump the whole task-set block, which no comparator can express.
Pinning after the sort also keeps "topmost bound row under the current sort" a well-defined
phrase rather than a cycle feeding on its own output.

**2. Every bound candidate pins.** Where ADR-0201 chose one set among several bound to a
checkout and confessed the choice, all of them pin, in the order the old sub-ladder gives:
checkout claim first, then most recently drained, then topmost under the current sort. The
sub-ladder is demoted from deciding *which one wins* to deciding *which one leads*, which is
what it was always fit for — being wrong now costs second place instead of a wrong answer.
Nothing is chosen, so nothing needs confessing.

**3. Pinned rows are moved, not duplicated.** One row, one key. Two renderings of one
container would break the list widget's key-based re-anchoring and make `j`/`k` counts lie.

**4. The mark lives in the prefix column that already exists.** `ui.List.renderPrefix`
renders two cells on every row: `█` for the cursor, blank otherwise. The Work dashboard sets
no quick-access label, so cell two is free: `▸` marks a pinned row, giving `█▸` when the
cursor is on it and ` ▸` when not. No new gutter, no width change, no alignment that shifts
between launches. The glyph is directional where the cursor is a block, so the two never read
as one smeared bar, and it repeats on every pinned row rather than heading the block — each of
those rows is attributed, not just the first.

**5. Re-derived on every rebuild, from facts read once.** The pane facts are still one
`display-message -p` at launch, carried thereafter; the dashboard's own pane does not move.
What changes between rebuilds is the container set, so a pin may appear, move, or vanish
mid-session. ADR-0201 decision 4 refused exactly this for a cursor, and was right to: a target
that outlives the human's navigation teleports them back. A pin never moves the cursor, so the
objection does not transfer, and the events that change attribution — starting a drain,
binding a set — are the human's own, which makes the row moving feedback rather than
interference.

**6. Pinned rows are not sticky in the viewport.** They scroll away like any other row. The
pin is a starting position, not a frame.

**7. Presets and filters are absolute.** A row hidden by the active Work view preset or the
live filter query is not pinned, and pinning never widens either. This is ADR-0201's "a launch
does not get to overrule a deliberate preset", unchanged.

**8. Total silence.** No flash, ever. `Attribution.Note` and `attributionHiddenLine` are
deleted. Attributed means pinned; unattributed, or attributed to a hidden row, means the
dashboard looks exactly as it always does. ADR-0201 decision 6 spent a line on the hidden case
because a cursor sitting at row one with no explanation was an anomaly; with no cursor moved,
there is no anomaly, and the line has nothing left to defend.

**9. Both pages.** ADR-0201 opened on page A only, because *launching* onto the other page
would be jarring. A pin has no such cost — it simply waits at the top of page B when the human
toggles there — so Routines join as an attributor. They get the tag rung (`@pop_routine`) and
no neighbourhood rung: routines are project-scoped rather than checkout-scoped, so a cwd rung
would pin a project's whole routine list for any shell in the repo.

## Considered Options

**Attribution as the primary sort key.** The literal reading of "reorder so it's on top".
Rejected in decision 1: it cannot lift a Map above the task-set block without dissolving kind
precedence, and it would silently outrank **Task set priority**, which is a number a human
wrote down.

**Keep a quieter ambiguity line ("3 sets bound to this checkout").** Rejected. Three pinned
rows are visibly three pinned rows; the count adds nothing and re-establishes the habit of a
sentence on launch.

**Announce hidden candidates when some pinned and some did not.** Rejected in decision 8 —
the same random-looking sentence in a new costume, which is the thing being removed.

**Freeze the pinned set at first render.** Would stop rows moving mid-session. Rejected: it
is a pending target that survives, one layer down.

## Consequences

- `tasks/setkind`'s `breakBoundTie` survives as an ordering function and stops being a
  decision; `Attribution` loses `Note`, and `attributionHiddenLine` goes with it.
- The tests that assert flash content (`TestHiddenAttributedSetIsNamedAndThePresetIsNotWidened`,
  `TestCheckoutClaimDecidesAmongSeveralBoundSets`, `TestBoundTieBreaksOnDrainRecencyThenSortOrder`)
  become assertions about pin order and pin absence.
- A routine pin will be rare: a routine pane is short-lived, so page B's pin mostly will not
  fire. It is wired anyway because the alternative is a seam with one implementor missing.
- Attribution remains a general capability spent, still, on one surface. A later "what am I
  working on?" view should consume the same ladder.
