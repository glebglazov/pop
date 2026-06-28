---
status: accepted
---

# Drain checkout is an explicit choice; pop never auto-provisions a worktree

> **Relates:** supersedes ADR-0046 (worktree-default for unbound drains), amends ADR-0028 (auto-fanout), ADR-0035 (`execution_base` / representative resolution), ADR-0036 (adopt model), and ADR-0051 (integration target)

## Context

The drain-routing precedence tree silently chose a checkout for the operator: an unbound
whole-set drain in a `worktree_ready` repo auto-provisioned a fresh managed worktree
(ADR-0046), so the Queue fanned a repo's Ready sets into parallel worktrees with no human
decision. Two problems surfaced. First, the operator was forced to understand and pre-arrange
routing (`worktree_ready`, `execution_base`, bindings) *before* a drain, rather than deciding
*at* the drain when its consequences are visible. Second, the automatic fan-out manufactured an
**Integration backlog** of managed worktrees while the operator was away, each needing a human to
reconcile it — work the operator never asked for.

## Decision

**Routing stops deciding; the operator decides. Provisioning becomes an explicit act.**

- **Drain routing collapses** to: existing **Worktree binding** → explicit Runtime-path override →
  **current checkout**. The "auto-provision a managed worktree when `worktree_ready`" branch is
  removed. Pop never runs `git worktree add` while routing.

- **Provisioning is explicit, three ways:** `pop tasks implement --in-worktree` (provision a
  managed worktree forked from the trunk and drain there), the Queue-dashboard **Drain target
  picker**, or **Bind worktree** in advance. `--inline` is removed — the current checkout is now
  the baseline, and `--in-worktree` is the opt-in to isolation (a clean inversion of the old
  worktree-default + `--inline`-escape).

- **`worktree_ready` is removed.** It only gated AFK auto-provisioning, which no longer exists; a
  human who provisions is present to handle any setup. `.pop.toml` shrinks to `auto_merge_clean`
  (or vanishes).

- **`execution_base` becomes the `trunk` worktree** — kept, renamed, reframed as the repository's
  one canonical integration anchor and the fork base for managed worktrees. A non-bare repo
  defaults its trunk to the git main worktree; a **bare repo must declare `trunk = true`
  explicitly**, and an unconfigured bare repo can neither provision a managed worktree nor
  integrate — only drain in place in the operator's current checkout. This answers
  "integrate into which worktree?": always the trunk, so ADR-0051's binding-driven backlog stands
  with the trunk as the target rather than a per-binding recorded base.

- **AFK parallelism is opt-in.** The Queue spawns into the repo's representative checkout (the
  trunk for an unbound set), so unbound auto-drains land on trunk and serialize on its lock.
  Parallel fan-out across a repo's sets now requires pre-binding each set to its own worktree.

- **The Drain target picker** fuses target selection with the drain on the dashboard's `i` key for
  an unbound set: pick an existing non-managed worktree (adopt), a new managed worktree (default,
  forks from trunk), or the trunk itself — then bind and drain in one action. A bound set skips the
  picker and resumes. The picker is dashboard-only; bare `pop tasks implement` never prompts, so
  Queue-spawned drains never block.

## Considered options

- **Keep the auto-provision default (ADR-0046), add the picker as a convenience.** Rejected — leaves
  two routing models (silent tree + explicit picker) and keeps manufacturing an unattended backlog.
- **Remove `execution_base` entirely.** Rejected — a bare repo then has no integration target;
  renaming it to an explicit `trunk` keeps bare repos integrable when the operator opts in.
- **Record a per-managed-binding fork base instead of a repo trunk.** Rejected — a repo-level trunk
  is one concept to learn and makes every non-trunk binding integrate to the same place.
- **Run the picker for TTY `pop tasks implement` too.** Rejected — the Queue spawns implement into a
  TTY pane; a routing prompt there would hang the daemon. Keep the picker a dashboard affordance.

## Consequences

- `tasks/binding/route.go` loses the worktree-ready / `provisionManagedBinding` branch; routing ends
  at the current checkout. `--in-worktree` provisions on demand; `--inline` is deleted.
- Config: `worktree_ready` removed; `execution_base` → `trunk` (glossary and **Repo override**
  schema). Existing configs need migration, not a silent alias.
- Glossary (live in a CONTEXT fragment): retires **Worktree-ready project** and **Execution base**;
  adds **Trunk worktree** and **Drain target picker**; redefines **Drain routing**, **Implement**,
  and **Queue dashboard**.
- Bare-repo workflows must set `trunk` (or bind per set) to keep integration and managed worktrees.
