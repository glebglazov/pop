# Dormant: Queue execution in pop-managed worktrees

**Status:** dormant — not implemented, deliberately deferred. Pick this up only after the
`pop queue` v1 (parallel per-project supervisor) has shipped and proven out.

## The idea

v1 of the Queue serializes execution *within* a project: the **Runtime execution lock** allows
one drain per checkout at a time, so a project's Ready Task sets run one after another even
though different projects run in parallel. That serialization is a property of sharing a single
working tree, not a fundamental limit.

If a project's environment can stand up an **isolated worktree** that is ready to run — tests
pass, servers start, dependencies install without manual steps — then the Queue could drain
several of that project's Task sets (or even several tasks) *concurrently*, each in its own
pop-managed worktree with its own checkout and therefore its own Runtime execution lock.

## Why it's deferred

- v1's whole point is the supervisor + cross-project fan-out + agent fallback. Worktree
  parallelism is an orthogonal multiplier that adds real complexity (worktree lifecycle,
  per-worktree env readiness, cleanup, merge/branch strategy) and should not gate v1.
- It only pays off for projects whose environment is *automatable* in a fresh worktree. That is
  a per-project capability the user must declare; most projects won't have it day one.

## What v1 already gets right for this future

- **"Picked-up" is keyed on the Runtime execution lock, which is per-checkout.** A worktree is a
  distinct checkout with its own lock, so per-worktree running-set tracking falls out for free —
  no redesign of the source-of-truth.
- **Cross-project parallelism already exists.** Extending "parallel across projects" to "parallel
  across worktrees within a project" reuses the same supervisor loop; the new work is worktree
  provisioning and env-readiness gating, not the scheduler.
- CONTEXT's *Runtime path* already anticipates this: "Durable runtime path configuration is
  deferred until worktree-oriented execution needs it." This is that need, when it comes.

## Open questions to resolve when reviving

- How does a project *declare* that its environment is worktree-ready (config flag? a probe
  command the Queue runs once to verify tests/servers come up)?
- Worktree lifecycle: who creates them, where, how many concurrently, and when are they torn
  down (on set DONE? kept for inspection like panes are in v1)?
- Branch/commit strategy: each worktree drains on its own branch — how do implementation commits
  reconcile back (auto-merge, leave for human, open a PR)?
- Per-project concurrency cap, and how it interacts with the global agent-quota cooldown (more
  parallel worktrees burn quota faster).
- Does dispatch granularity drop from per-set to per-task in worktrees (the v1 reason for
  per-set dispatch — lock contention — disappears once each unit has its own checkout)?

## Pointer

When revived, this likely warrants its own ADR and updates to the **Queue** glossary entry
(currently: "Global cross-project priority ordering is a non-goal" and per-project serial).
