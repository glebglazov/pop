---
status: accepted
---

# Work view presets replace the dashboard's inclusion toggles, and a preset may carry its own sort

## Context

ADR-0121 gave the Work read surfaces one uniform rule: DONE is hidden, revealed
by `--include-done` or the `f` menu's **Show done** toggle. ADR-0186 added a
second, independent toggle for archived rows. Two composable booleans is the
whole filtering vocabulary the surfaces have.

That vocabulary cannot express the one question an operator actually asks about
finished work. A **Task set** that is DONE but still holds a *managed* checkout —
bound to a pop-provisioned worktree, not yet folded — is not filed away; pop is
holding a worktree for the human and nothing says so. ADR-0121 saw this and
recorded it as an accepted trade-off: "the uniform DONE-hide drops the
dashboard's standing managed-worktree teardown reminder." In practice the
reminder is unreachable, because the only way to ask for it is **Show done**,
which is all-or-nothing: turning it on floods the table with every set ever
completed, and the handful that need folding are invisible in the flood. The
filter that reveals the signal also buries it.

The same all-or-nothing shape defeats "what was I working on recently" — a view
with no expression at all today.

Two smaller defects sit on the same surface. The `f` menu renders past the bottom
of the pane on a long table: `viewWithFilterMenu` lets the table consume the full
height budget and then appends the toggle block after it, bypassing the shared
frame that ADR-0079 made responsible for reconciling budget with render. And once
the menu closes nothing indicates a filter is engaged, so a short list and a
filtered list are indistinguishable.

## Decision

**A Work view preset is the unit of row selection.** One named, self-contained
answer to "which rows, in what order", selected one at a time. The `f` menu
becomes a single-select numbered list of presets; the composable toggles are
retired.

1. **Closed declarative vocabulary.** A preset declares `name` (required),
   optional `label`, and any of `status`, `unfolded`, `archived`
   (`exclude`|`include`|`only`, default `exclude`), `created_within`, `sort`
   (absent, `created_desc`, `created_asc`), and one `hide` clause carrying the
   same fields nested one level. Fields AND-combine; `hide` subtracts its match.
   Unknown keys and bad values are findings, not load failures (ADR-0054).

2. **The shipped roster is written in that vocabulary.** `active` (default),
   `unfolded`, `recent-7d`, `recent-30d`, `all`. A system preset pop cannot
   express in the user's language is proof the vocabulary is too weak — this is
   what forced the `hide` clause, since `active` is "NOT (done AND folded)", a
   negated conjunction no AND-only field set can state. The two `recent-Nd`
   presets carry `archived: include`: a window asks a time question, so a set
   created inside it belongs there whether or not it was later filed away, and
   the `exclude` default belongs to the state-scoped presets. The window still
   reads creation only — an old set archived yesterday stays out.

3. **Config shape.** `[[work.dashboard.tasks.presets]]`, `include:"replace"`: a
   user's list replaces the shipped one wholesale, and `{ system = "<name>" }`
   references a shipped preset so its position may change without forking its
   predicate. The first entry is the default. Numbering is positional, `1`–`9`; a
   tenth or later preset is a finding and stays reachable by j/k plus Enter.

4. **Recency is read from the identifier.** `created_within` compares against the
   `YYYY-MM-DD[-HHMM]` prefix every Task set and Map id already carries — the
   same chronological prefix `pop tasks transfer export` completion already sorts
   on. No new persisted state, and no lifecycle instants.

5. **`unfolded` is derived.** A set is unfolded when it holds a *managed*
   (provisioned) Worktree binding and is DONE or Awaiting-approval — exactly the
   foldable condition, and exactly the checkout pop will tear down. An adopted
   binding (the main checkout on master, or any path outside the managed-worktree
   root) is bound but not unfolded: there is nothing to fold. Bindings are
   already loaded per row for the worktree column, so the predicate is free.

6. **A preset may carry its own sort, above which the tiers still float.** Absent
   `sort`, rows order by the ADR-0121 status scheme. A declared `sort` replaces
   the status scheme only: the live-drain, auto-drain and orphaned membership
   tiers float above every preset.

7. **Both surfaces, one vocabulary.** `pop work status --preset <name>` takes the
   preset *name*, never its number, since numbers move when the config is
   reordered. `--include-done` becomes a deprecated alias for `--preset all`,
   read with a warning. The daemon inherits the default preset only and is never
   configurable per-user, so its output can be read at face value. `pop tasks
   status` stays outside the preset system — it is the per-repo authoring view
   where everything is wanted.

8. **The active preset is always named in the page header**, whether or not it is
   the default.

9. **The `f` block reserves its height through the shared frame**, and below the
   short-pane height floor the menu takes the whole screen, as the help overlay
   does.

10. **Routine presets are deferred.** `work.dashboard.routines.presets` is not
    shipped: `status`, `unfolded` and `created_within` are Task-set vocabulary, a
    Routine has no creation date, and no Routine preset worth writing has been
    identified. Adding the key later is additive.

## Considered options

- **Keep the toggles and add a third for unfolded.** Rejected: a reminder the
  operator must switch on is not a reminder, and a fourth and fifth boolean
  compose into states nobody wants.
- **Presets layered above the toggles.** Rejected: two mechanisms answering
  "which rows" is precisely the drift ADR-0121 set out to end.
- **A preset expression language** (`status:done and age<30d`). Rejected: owning
  a grammar forever, with its own parser and error messages, for a feature whose
  job is "show me last month's work". Pop's config is declarative-and-validated
  throughout.
- **A shell-out predicate**, `UserDefinedCommand`-style. Rejected outright: a read
  surface that forks a user command per row is not a read surface.
- **`any = [...]` unions instead of one `hide` clause.** Rejected: `hide` covers
  the actual cases as De Morgan's other half without turning the config into a
  nested boolean tree.
- **`archived` as a bool.** Rejected: it has three genuine states, and today's
  default is exclude — a bool would make `all` silently mean "everything except
  archived".
- **Persisted finalization instants** (`folded_at`, `archived_at`, last-verified,
  last-AFK-complete) backing a "recently finalized" filter. Deferred: creation
  date answers "what have I been working on lately" with no new state, and the
  instants' preserve-then-renew semantics are subtle enough to be worth observing
  before freezing a config grammar over them.
- **Declared preset numbers.** Rejected: position already determines the default,
  and two facts that can disagree is one too many.
- **Hard load error past nine presets.** Rejected as the wrong severity — a tenth
  preset must not be able to blank a dashboard.
- **Moving the `f` block to the top of the view** to dodge the overflow. Rejected
  in favour of fixing the height budget: the block belongs beside the footer hint
  that describes it, and the standing header indicator solves the visibility half.

## Consequences

- **Amends ADR-0121 decision 2.** "DONE is hidden by default, uniformly" becomes
  "the default preset hides done-and-folded work". The teardown reminder ADR-0121
  gave up is restored, as a status distinction the default view carries rather
  than as the carve-out ADR-0121 rejected.
- **Amends ADR-0121 decision 3.** "One shared comparator" becomes "one shared
  comparator, which a preset's `sort` may replace below the membership tiers".
- **Amends ADR-0186.** The show-archived toggle is retired into the `archived`
  field; `pop work status` gains the ability to see archived work, which it never
  had.
- **Trade-off, accepted:** recency is creation-dated, so work created long ago and
  finished yesterday does not appear in `recent-30d`. This is the deliberate cost
  of adding no persisted state; the deferred instants above are the way back.
- **Trade-off, accepted:** the preset field set is a public config grammar and so
  is expensive to narrow later. It is kept deliberately small for that reason.
- Glossary: **Work view preset** and **Unfolded Task set** are added; **Work
  dashboard filter menu** and **Work surface sort order** are redefined; **Done
  inclusion** is retired.
