# Queue surfaces show the repository display label, not the trunk worktree's picker name

## Context

The **Queue dashboard** PROJECT column (and `pop queue status` / daemon output,
all fed by the same `repoName()` derivation) showed the *representative (trunk)
worktree's* picker name — assembled for a bare repo as `displayName + "/" +
wt.Name`, e.g. `game server/main`. But the queue keys on **Repository identity**:
a repo's worktrees collapse to one scheduling unit, the row denotes the *repo*,
and the bound worktree already has its own WORKTREE column. So the trailing
`/main` was doubly wrong — it named a worktree in a column that means "repo," and
duplicated what WORKTREE already shows. The project picker, by contrast,
legitimately wants `game server/main`: there each worktree is its own row.

## Decision

Persist the depth-aware `displayName` (already computed during expansion, then
discarded) onto `project.ExpandedProject.ProjectLabel` and carry it through
`projectScan.ProjectLabel`; `repoName()` returns `ProjectLabel`, falling back to
the full picker `Name`. The result is the picker display name **minus the
trailing worktree segment**, so a bare repo reads as `game server` while a
`display_depth = 2` repo still reads `work/game server` — `display_depth`
disambiguation is preserved (and matters more here, since the queue is
machine-global across every repo). Only the queue path changes; the project
picker's `ui.Item.Name` is untouched.

## Considered Options

- **Reuse the git-identity basename** (`repoLabelFromScan` →
  `tasks.RepoBasename(commonDir)`). Already available and already the queue's
  *identity* key, but it ignores config — a `display_depth = 2` repo would
  collapse to a bare basename, losing the disambiguation the project picker
  keeps. Rejected: identity ≠ display.
- **Strip the segment after the last `/` from `Name`.** No new field, but a
  non-bare repo with `display_depth = 2` has `Name = "work/repo"` and *no*
  worktree suffix, so stripping would wrongly chop `work/`. Rejected as unsafe —
  which is why the label is captured at expansion, where bare-vs-non-bare is
  known, rather than reconstructed downstream.

## Consequences

Two "repo label" concepts now coexist and must not be conflated: **identity**
(`RepoLabel` / `repoLabelFromScan` — the git basename, used for keying and
binding paths) and **display** (`ProjectLabel` — the depth-aware label shown in
PROJECT). Collapsing display back into identity would silently drop
`display_depth`; keeping them separate is deliberate. Dashboard search and sort
now key on the repo label, so `/main` is no longer a matchable substring there.
