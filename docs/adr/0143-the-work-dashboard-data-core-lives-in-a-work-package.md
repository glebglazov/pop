---
status: accepted
---

# The Work dashboard's data core lives in a top-level `work` package; the TUI consumes it

> Partially superseded by [ADR-0173](0173-work-is-one-kind-interface-with-data-shaped-returns-and-kind-side-adapters.md).
> The no-TUI boundary and its guard test stand unchanged. What moved: `work` is
> now the Work *seam* and imports no kind, so the snapshot building, the ADR-0121
> comparator, the Done-inclusion filter and the STATUS-cell composition described
> below live kind-side (`tasks`, `tasks/setkind`) with the repository-group
> resolution in `repogroup`. `work.Row`/`SetRef` survive as a transitional
> projection of `Container` until the contract slices delete them.

## Context

`queue/dashboard.go` (4,538 LOC, ~230 decls) fuses the Work dashboard's data derivation — snapshot loading, row building (Task set and Map rows), the ADR-0121 sort tiers/bands/status order, the Done-inclusion filter, status-cell composition — with the Bubbletea model, its ~17 async message types, action side-effects, and all rendering. Its 5,668-LOC test file is the symptom of an interface too wide to pin down. The split half-exists already: the derivation functions are pure over `DashboardRow` (the model calls them, never the reverse), and `queue/status.go` already consumes rows + comparator + unstyled cells with zero bubbletea — ADR-0121's "one row builder, one comparator" realized. What's missing is a mechanical boundary. Meanwhile [ADR-0130](0130-the-queue-dashboard-becomes-the-work-dashboard.md) moved the surface to the `pop work` family while the code stayed in `queue`.

## Decision

- **A new top-level `work` package is the data core**: snapshot building (`BuildDashboard` and the static/overlay row assembly, including Map rows), the row/snapshot/`SetRef` types, the ADR-0121 comparator with its tiers/bands/status order, the shared Done-inclusion row filter, and unstyled cell composition (status label/cell, live indicator, worktree label). Exports drop the `Dashboard` prefix on the way in — `work.Row`, `work.Snapshot`, `work.BuildSnapshot`, `work.SortRows`, `work.ShowRow`, `work.StatusCell` — one-time churn over permanent stutter.
- **"Pure, no TUI import" is literal: no lipgloss in `work`.** Styled cell wrappers, the style maps, and column/layout math (which uses `lipgloss.Width`) stay queue-side as a render-shared layer used by both the static `pop queue status` render and the TUI. Style-selection facts the wrappers need (e.g. the destination kind) are exported on `work.Row`.
- **`work` owns its own `Deps`**, borrowing the process-cached Execution-state store handle per [ADR-0140](0140-sibling-packages-borrow-the-process-cached-store-handle-through-tasks-deps.md). It cannot take `queue.Deps` — queue imports work, not the reverse.
- **The TUI stays put.** The `QueueDashboard` model, message layer, action side-effects, rendering, and `dashboardshell` remain in place; `queue` becomes a consumer of `work`.
- **The row-assembly test seam stays unexported.** The public surface is snapshot-in, rows-out; derivation tests live in-package.
- Docs-only for now; code lands later through the task pipeline.

## Considered Options

- **Same-package split by file discipline** — rejected: the locality and deletion claims only hold if the boundary is compiler-enforced.
- **Subpackage `queue/dashboard`** — rejected: cements the dashboard as queue-family against ADR-0130's grain; queue is the scheduling concern.
- **Name it `workdash`** — rejected: `work` matches the `pop work` family and is the natural later home for the set-state derivation. The ADR-0136 "Work store" name adjacency is noted and accepted — different concept, same domain word.
- **Fold the set-state convergence in now** (the scheduler's `Scan`/`statusFromDecisions` vs `BuildDashboard`/status label — the same READY→IN PROGRESS rule twice) — rejected: split first; the core's surface is shaped so that convergence lands *inside* `work` later without contradiction.
- **Also consolidate the TUI scaffolding** (queue/routine dashboards duplicate ~20 table/list/peek helpers nearly line-for-line) — rejected as scope creep; logged as a separate follow-up.

## Consequences

- The ADR-0121 sort/band/status rules get one compiler-enforced home; a new status rule lands in `work` once and both read surfaces follow.
- The ~72 direct derivation assertions relocate to `work`'s tests; model-driven tests stay in `queue`; the 5.7K test file shrinks accordingly.
- `routine/dashboard.go` (1,708 LOC, same fused shape) adopts the same split later — a follow-up, not co-designed here.
- The queue's two set-state pipelines converge inside `work` when that candidate is taken up.
- TUI scaffolding consolidation across queue/routine/`dashboardshell` is a distinct follow-up candidate.
- No glossary changes: the split alters no user-facing meaning; "data core" is architecture, not domain language.
