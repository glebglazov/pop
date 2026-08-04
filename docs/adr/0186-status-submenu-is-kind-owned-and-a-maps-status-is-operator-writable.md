---
status: accepted
relates: "gives the Map the operator-writable status [ADR-0172](0172-pop-owns-the-wayfinding-lifecycle-and-pop-wayfinder-becomes-pop-map.md) deliberately left it without, and extends the in-process status-write rule of [ADR-0158](0158-dashboard-verbs-split-by-whether-they-hand-off-and-say-so-in-the-key-case.md) to a second kind"
---

# The status submenu is kind-owned, and a Map's status is operator-writable except arrival

## Context

A Map row on the Work dashboard had no way off it. It leaves the dashboard only
when it is archived, abandoned or judged BROKEN — an arrived Map deliberately
stays, because a Map's terminal state must not inherit the DONE-hiding rule
written for Task sets (ADR-0172) — and none of those exits was reachable from the
dashboard. Archiving was a CLI-only verb. Abandonment had no verb anywhere: the
`abandoned` status word was read by the loader and written by nothing, so the only
way to set it was hand-editing `map.md`. Killing the Map's tmux session changed
nothing, because a Map row is nothing but its directory re-read on every poll.

The obvious place for those verbs was the dashboard's nested status submenu, and
that submenu was task-set-shaped end to end: a hardcoded five-item list in
`dashboard.go`, an item type whose `verb` field was documented as "pop tasks
subcommand", an opener gated on the Task-set kind's own status verb, and a
`switch` over those five verb strings in a queue-side file. The Work seam
(ADR-0173) had already removed every other kind vocabulary from the surface —
row verbs, item verbs, status cells, columns and summaries are each asked of the
kind that owns the row — and the status submenu was the last piece of one kind's
vocabulary written into the dashboard.

## Decision

**1. Each Work kind declares its own status verbs, and they are performed like any
other verb.**

`work.Kind` gains `StatusActions(Container) []Action` beside `Actions`. The
submenu opener is a *shared* verb, `work.VerbStatus`, because opening the submenu
is the surface's act while the items inside it are the kind's: the dashboard
recognises one verb id and lays out whatever list it is handed. Selecting an item
dispatches it down the single path every row verb already takes, ending at the
kind's `Perform`. There is no status-specific dispatch, message type or
apply-function left on the surface, and no kind is named in the dashboard's status
code.

The Task set's five verbs (`c` complete, `o` open, `s` skip, `x` archive, `u`
unarchive) moved to the Task-set kind unchanged in key, label, order and
behaviour — every one still writes in-process, with no subprocess and no TUI
suspend (ADR-0158). Its whole-set complete/open/skip are the same verb ids as its
per-task ones; which one runs is decided by whether `Perform` was handed an item.
A Routine declares no status verbs and therefore offers no opener: its one
enable state is the pause bit, already a row verb, and an obsolete Routine is
deleted rather than archived.

**2. A Map's status is operator-writable from the dashboard: archive, unarchive,
abandon, open.**

All four are in-place writes offered under the same `s` opener, ungated, each
going through the writer the command line uses — `ArchiveMap`/`UnarchiveMap` for
the cross-kind registry bit, and `arrive`'s own `map.md` writer for the two status
words. Archiving an unregistered Map therefore reports the identical
`pop map register <id>` corrective rather than failing quietly. `pop map abandon`
becomes a real command-line verb writing the status word the loader already
understood, and `pop map open` reverses arrival *and* abandonment, so no word on
disk is a dead end.

**Arrive is offered nowhere in the dashboard.** It is not a status write: it ends
the effort, tears the Map's tmux session down and renders an arrival report the
human is meant to read — a ceremony whose output needs somewhere to go. Reopen in
the submenu still reverses it, so arrival is not a one-way door. The reason is
recorded beside the Map's status verbs in code, because "why isn't arrive here" is
the first question a reader of that list asks.

The submenu's `open` writes the word only and moves nobody. `pop map open` still
does both halves; the dashboard's row already carries three verbs whose whole
purpose is taking the operator to the Map's session.

**3. Archived rows are reachable through a show-archived view toggle.**

An archived row is hidden, so unarchiving would otherwise be unreachable from the
surface that offers it. The dashboard's `f` filter menu gains a **show archived**
toggle beside show done: cross-kind, off by default, session-only and unpersisted,
flipping a view flag the next build reads. With it on, archived containers are
listed *beside* the active ones rather than instead of them, and each carries an
`archived` suffix in its STATUS cell — without it an archived row is
indistinguishable from a live one, and the operator cannot tell which of
archive/unarchive to press.

Two consequences are deliberate. An archived Task set is shown regardless of the
Done-inclusion flag: nearly every archived set is DONE, so letting that rule veto
a second time would make the toggle reveal nothing. And show-archived does not
reveal abandoned or BROKEN Maps — it is about filing, not about status; an
abandoned Map is recovered with `pop map open`.

## Consequences

- Every kind now answers two verb questions instead of one. A future kind with no
  status to write says so in one line, and gets a correct (empty, unopenable)
  submenu for free.
- The Task-set archive verb is no longer dispatched queue-side: both the row's `x`
  and the submenu's `x` land in the kind's `Perform`, so the retired
  `drain.Deps.ArchiveTaskSet`/`UnarchiveTaskSet` methods are gone and the injected
  seam behind them (`SetArchived`) writes both directions.
- `Container` grows one cross-kind status fact, `Archived`. It is the only status
  bit every kind shares — the archived bit is one registry row's column — and the
  show-archived view is what makes it need to be legible per row.
- A Map's lifecycle status still has no column of its own. The STATUS cell remains
  the ticket tallies (ADR-0130): what a reader needs from a Map row is how much
  thinking is left, and the only lifecycle words that reach the cell are the
  `archived` suffix and, by absence, the Maps the view hides.
- `pop tasks status --archived` is unchanged. The archived-only view and the new
  archived-included view are distinct answers from one refresh, and only the
  former reads as "this is the archive view" to the verify pass and the "N
  archived hidden" footer.
