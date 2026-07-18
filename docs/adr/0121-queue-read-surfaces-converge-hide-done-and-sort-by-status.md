---
status: accepted
---

# Queue read surfaces converge: status mirrors the dashboard, DONE is hidden by default, and rows sort by status

## Context

Pop has two machine-global read surfaces answering the same question — "what is in the queue right now?" — in two different shapes:

- `pop queue status` was a labeled-line inventory: a `Summary` headline over per-bucket detail sections (`Picked-up sets`, `Active worktrees`, `Queued ready sets`, `Blocked`, `Awaiting approval`, `Skipped repositories`, a trailing idle-count line).
- `pop queue dashboard` was a task-set table (PROJECT / TASK SET / STATUS / WORKTREE plus a live-drain indicator).

Two shapes for one question drift apart and double the surface area a change has to touch. Separately, both surfaces surfaced finished work: the dashboard deliberately kept DONE sets that still held a managed Worktree binding as a teardown reminder, cluttering the common case with completed sets. And the dashboard sort grouped rows by Project within a running tier, so an in-progress set in a late-alphabet project sorted below idle work of an early-alphabet one — the reader could not scan "what is running / ready" across the machine at a glance.

## Decision

Three cohesive changes to the queue read surfaces.

1. **`pop queue status` mirrors the dashboard.** It becomes a static, non-interactive render of the *same rows, columns, row filter, and sort* as the Queue dashboard, under a one-line `Summary` headline (`N running, queued, blocked, awaiting approval`). Both surfaces share one row builder and one comparator. Every former inventory section is retired except a trailing `Scan errors` section. Status stays non-interactive so it remains greppable/pipeable and serves as the Queue run baseline.

2. **DONE is hidden by default, uniformly.** DONE Task sets are omitted from both surfaces — including sets that still hold a managed Worktree binding. `--include-done` (both surfaces, sets the initial state) reveals them; the dashboard additionally exposes a **Show done** toggle inside a new `f` **filter menu** popup (a session-only view state). `/` remains the fuzzy text filter; `f` is the inclusion-settings popup.

3. **Rows sort by status, not project.** One shared comparator, precedence: live-drain tier → auto-drain tier → orphaned tier → status scheme. In the status scheme, **IN PROGRESS** and **READY** rows float cross-project as two leading bands (each ordered Project asc, then Task set identifier desc); every remaining status groups by **Project** first, then by status in the order AWAITING-APPROVAL → NEEDS-VERIFY → VERIFY-FAILED → FAILED → BLOCKED → DEFERRED → DONE → MISSING/MALFORMED, then Task set identifier desc.

## Considered options

- **Keep two distinct surface shapes** (inventory vs. table). Rejected: they answer the same question and drift; one shared row builder + comparator is cheaper and keeps them honest.
- **Keep the DONE-with-managed-worktree carve-out visible.** Rejected: a uniform DONE filter is one mental model; the teardown reminder survives behind `--include-done` / Show done, and teardown itself stays gated at Archive (ADR-0071, ADR-0116).
- **A bare `d` toggle key for include-done.** Rejected in favour of an `f` filter-settings popup, so future row-inclusion filters (by status, by project) have a home instead of consuming one top-level key each.
- **Pure status sort with no tiers.** Rejected: live drains, auto-drain consent, and orphaned bindings each warrant floating to the top regardless of derived status — a reader needs those before idle work.

## Consequences

- **Amends ADR-0111.** Its "sort keeps a running tier that floats live-drain rows to the top" is generalized: live-drain stays the top tier, but auto-drain and orphaned join as explicit tiers above the status scheme, and the within-rest ordering flips from Project-first-then-status-sink to the status scheme above.
- **Trade-off, accepted:** the uniform DONE-hide drops the dashboard's standing managed-worktree teardown reminder. A pop-created worktree left on a DONE set is no longer visible by default; it is reachable via `--include-done` / Show done, and teardown remains gated at Archive.
- Glossary: `Queue status summary` and `Queue dashboard` are redefined; `Queue surface sort order`, `Queue dashboard filter menu`, and `Done inclusion` are added.
- `pop queue status` and the dashboard now key on one row builder and one comparator — a change to filter or order lands in both surfaces at once.
