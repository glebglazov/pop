---
fragment: 40e009ea
generation: 0004
branch: master
---

+ Work view preset
  One named, self-contained answer to "which rows, in what order" on a Work read
  surface — the unit the **Work dashboard filter menu** now selects between, and
  what `pop work status --preset <name>` takes. A preset carries a closed set of
  declarative fields (`name`, optional `label`, `status`, `unfolded`, `archived`,
  `created_within`, `sort`, and one `hide` clause holding the same fields), all
  AND-combined, with `hide` subtracting its match from the result so a preset can
  express "everything except this conjunction". Exactly one is active at a time —
  presets are not composable toggles — and the pop-shipped roster is written in
  the same vocabulary a user's own presets use, so a system preset pop cannot
  express is proof the vocabulary is too weak. Declared at
  `[[work.dashboard.tasks.presets]]`, replacing the shipped list wholesale; the
  first entry is the default, and the reference form `{ system = "<name>" }`
  keeps pop's definition of a shipped preset while relocating it.
  avoid: filter preset, view filter, saved filter, row filter, named view
  under: Work supervision

+ Unfolded Task set
  A **Task set** whose work is finished but whose checkout is still held: bound,
  and **DONE** or **Awaiting-approval**. It is exactly the foldable state named
  under **Fold**, given a noun so a read surface can show it — an unfolded set is
  a standing reminder that pop is holding a checkout for the human, which is the
  signal ADR-0121's uniform DONE-hide traded away. Derived, never persisted: the
  binding is already loaded to render the worktree column, so the predicate costs
  nothing.
  avoid: unfinalized, pending fold, outstanding set, unlanded work
  under: Work supervision

~ Work dashboard filter menu
  The modal popup opened with `f` on the **Work dashboard**: a single-select list
  of **Work view preset**s, numbered by position, with `1`–`9` selecting directly
  and j/k plus Enter reaching a tenth or later preset that has no number. Exactly
  one preset is active, its name always rendered in the page header so a narrowed
  list can never be mistaken for an empty one. It reserves its own block height
  through the shared frame rather than rendering past the bottom of the pane, and
  below the short-pane height floor it takes the whole screen. Its state is
  session-only, reset to the default preset on relaunch. Distinct from `/`, the
  fuzzy text filter, which is a transient query over already-included rows.
  was: The modal popup opened with `f` on the **Work dashboard**, holding
    row-inclusion filter toggles — a **Show done** toggle (see **Done
    inclusion**) and a show-archived toggle — composable and independently
    settable, extensible to future inclusion filters. A sibling of the `a` action
    menu, session-only, reset to default (done hidden) on relaunch.

~ Work surface sort order
  The row ordering shared by `pop work status` and the **Work dashboard** when the
  active **Work view preset** declares no `sort`. Precedence: (1) a **live-drain**
  tier, then (2) an **auto-drain** tier, then (3) an **orphaned** tier — each
  floating above the status scheme; then (4) the status scheme itself. In the
  status scheme, **IN Progress** and **READY** rows float cross-project as two
  leading bands (each ordered by Project ascending, then Task set identifier
  descending), and every remaining status groups by **Project** first, then by
  status in the order AWAITING-APPROVAL, NEEDS-VERIFY, VERIFY-FAILED, FAILED,
  BLOCKED, DEFERRED, DONE, MISSING/MALFORMED, then Task set identifier descending.
  A preset's `sort` replaces the status scheme only — the three membership tiers
  float above every preset, because a live drain is the one row a human always
  needs to see whatever they asked for.
  was: The single row ordering shared by `pop work status` and the **Work
    dashboard**, with no per-view override — the same four-level precedence, but
    asserted as singular.

- Done inclusion
