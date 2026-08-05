---
status: accepted
relates: "gives the fork-free static resolution of [ADR-0060](0060-carried-coordinates-resolve-fork-free-from-markers.md) a read-path budget to defend, extends the per-load memo of [ADR-0173](0173-work-is-one-kind-interface-with-data-shaped-returns-and-kind-side-adapters.md)'s Work seam with a process-lifetime **Manifest memo**, and constrains what the supervisor of [ADR-0176](0176-the-work-supervisor-drives-a-second-seam-advancer.md) recomputes per tick"
---

# A Work read surface pays only for what it renders

## Context

The **Work dashboard**'s first paint measured ~600-650ms and `pop work status`
~4.6s, on a machine with 10 repo groups, 66 registered Task sets, 127 **Worktree
binding**s and 2 Maps. Both figures came from work whose result nothing on the
screen used.

The dashboard's snapshot is built synchronously in `dashboardshell.newShell`
before `tea.NewProgram(...).Run()`, so all of it lands before the first frame.
Two thirds of that was git *process startup*, not repository work: 14
`rev-parse` forks from the verify overlay, run over all 66 refresh rows
including the ones hidden by the row filter, on a machine whose `/usr/bin/git`
is Apple's stub at 32ms per exec against 11.5ms for a real binary. The paint
also built page B's snapshot even when the operator opened page A, and each 2s
poll rebuilt the whole thing from scratch — a fresh wiring list, so a fresh
**Git fact memo** and a fresh repo-group resolution every time, with nothing
memoized across loads.

`pop work status` was worse and for a sharper reason. 92% of it sat in
`bindingSetDone`, whose cache is keyed `runtimePath+setID` while the work behind
it — a full `tasks.RefreshWith` of the whole definition path, every set's
manifest loaded and every task markdown re-validated — is per *checkout*. With K
bindings in one checkout that scan ran K times: 45,656 `ReadFile`s in one
command. It filled `RunView.WorktreeBindings`, a field `RenderStatus` never
reads, the dashboard never builds, and `DiffRunView` never inspects. The
**Work supervisor** recomputed it every tick to render it once, at daemon start.

## Decision

A read surface pays only for what it renders, and a fresh first paint is the
budget that enforces it. Concretely:

- **The first paint stays fresh.** No stale-then-fresh repaint, no snapshot
  cache spanning process boundaries. The target is ~100ms, which is one pass
  over the visible manifests and nothing else. "Instant" is bought by removing
  work, not by showing yesterday's rows.
- **Expensive derived fields are lazy.** `RunView.WorktreeBindings` becomes a
  thunk, so only a caller that renders it pays; `bindingSetDone` gains a
  two-level cache (refresh memoized per checkout, row lookup per set).
- **The verify overlay resolves only rows the page will render.** Narrowing keys
  off the session flags `drain.Deps` already carries, so the `f` toggles that
  reveal done or archived rows resolve their verdicts on the next reload — one
  tick of unresolved **Verification mark**s in exchange for zero forks on every
  open. The invariant this buys, and the one a test must pin: a verdict-derived
  aggregate may only be computed over rows whose verdicts were resolved. Mixing
  resolved and unresolved rows in a roll-up would report a wrong count silently,
  which is the one way this narrowing could become incorrect rather than merely
  late.
- **Page B is built on first `v`**, with an immediate reload rather than a wait
  for the tick.
- **Manifest load and validation are memoized process-wide** as the **Manifest
  memo**, keyed on set-directory content. This is what makes the 2s poll nearly
  free, and it collapses the three independent passes `pop work status` makes
  over the same repo groups (`drain.Scan` plus both page snapshots) onto one.
- **Per-group loads fan out**, bounded at `GOMAXPROCS`, with store reads hoisted
  out of the fan-out so they do not contend on the single connection
  (ADR-0140). `MigrateStorageLayout` is hoisted too and for a stronger reason: it
  *writes*, and concurrent groups migrating on one connection is the one place
  this fan-out could corrupt rather than merely slow. It runs to completion
  before the fan-out starts, or stays serialized behind a lock.
- **The git seam resolves a real binary once per process**, preferring `PATH` and
  falling through to a `Stat`ed candidate list — config/env override, Homebrew,
  `/usr/local`, the Xcode developer path — only when `PATH` resolves to
  `/usr/bin/git`, Apple's stub. The stub's ~20ms of exec overhead is the only
  thing being avoided, so pop must not second-guess a deliberately installed
  git. Stats, never a fork; probing via `xcrun -f git` would cost the fork it
  saves.
- **The guard is the invariant, not the clock.** A counting git seam pins the
  first paint's fork ceiling, and a counted-`ReadFile` assertion pins that
  `WorktreeBindings` is not computed unless read. Wall-clock assertions are
  flaky; these two facts are the ones that actually rotted.

## Consequences

The **Manifest memo** is the first memo in pop with a lifetime longer than one
load, which is why it is bounded (LRU) rather than unbounded like the **Git fact
memo** — the supervisor is long-lived and would otherwise grow without limit.
Its key must cover the whole set directory (`index.json` bytes plus each `.md`'s
mtime and size plus the directory's name set), not `index.json` alone: an
unlisted markdown file flips a set to MALFORMED through the orphan check
(ADR-0183), so a manifest-only key goes stale on a file the manifest never
mentions. Memoization belongs at `LoadManifest`, below `refreshWith` — that
caller is impure, calling `MigrateStorageLayout`, a write.

Lazy page B moves a page-B build error from aborting `pop work dashboard` at
startup to rendering as page B's own error chrome. That is the better behaviour:
an unreadable Routines page should not stop the operator seeing Task sets.

Pausing the poll while a menu or modal owns the keyboard lengthens the window in
which the operator acts on a row that has since changed. This is safe only
because the write path never trusts the row it was opened from: `LaunchDrain`
re-scans through `dashboardBindContext` and then `refuseUnusableBoundCheckout`,
`CreateWorktree` through `refuseDashboardBindWhileLocked`. A **Work container**
supplies **Carried coordinates**, not state — so a verb that started validating
against its opening row instead of a fresh scan would break this decision, not
just its own contract.

The fork ceiling is now a property worth defending rather than an accident. It
is the read-path counterpart to ADR-0060: what cannot be derived from a marker
is at least only forked for a row somebody is looking at.
