---
status: accepted
---

# A no-directive drain persists a default Worktree binding to its chosen checkout

> **Relates:** amends [ADR-0052](0052-drain-checkout-is-chosen-not-auto-provisioned.md) (the no-directive default ran transiently) and redefines the final step of Drain routing established in [ADR-0059](0059-task-set-may-declare-a-worktree-directive.md).

## Context

Drain-routing precedence is `binding → runtime-path override → registration worktree-intent → current checkout` (ADR-0059). The final step is **transient**: `RouteDrainCheckout` returns the current checkout's runtime path and records **no** binding. So a Task set with no worktree directive is never bound — it runs wherever the cwd happens to be on each drain.

Two costs follow. First, **fragile coherence**: a set drained from worktree A commits to branch-A; the next drain from worktree B can't see that work-in-progress. Multi-drain coherence (afk retries, continued work) depends on the operator manually draining from the same place every time. Second, an unbound set has no recorded execution checkout, so the dashboard must *derive* where it would land for display rather than reading a binding.

## Decision

The first no-directive drain **persists a default Worktree binding** to the checkout it resolved, rather than running transiently:

- **Foreground** (`pop tasks implement` from a checkout): the binding records the current checkout.
- **Queue** (headless, no cwd): the binding records the repo's **integration target** (ADR-0060: non-bare = main worktree; bare = config trunk), which is the checkout the queue routes into.

Subsequent drains resume via that binding (precedence step 1), so the set is sticky to where it first ran. The binding stores the branch too, so once bound the dashboard reads execution checkout and branch from the bindings table — no derivation, no git. An operator `bind-worktree`/override still wins (consulted first), and an explicit worktree directive still provisions/adopts ahead of this default.

So the routing precedence's final step changes from "run in the current checkout (transient)" to "**bind to the chosen checkout and resume there** (persisted)."

## Considered options

- **Keep the transient default (ADR-0052 as-is).** Rejected: it leaves multi-drain coherence dependent on operator discipline and keeps a class of sets perpetually unbound, forcing the dashboard to derive their landing spot. Persisting on first drain makes bindings universal-after-first-drain and the model uniform.
- **Bind eagerly at registration instead of first drain.** Rejected: registration has no checkout of the target repo in hand (ADR-0061 makes registration an in-repo verb, but it still registers in bulk from one cwd, not per-set per-checkout). First drain is the moment a checkout is actually, deliberately chosen — the binding belongs there.

## Consequences

- **Stickiness, deliberately.** This is the behavior ADR-0052 avoided: after the first drain, `cd` to a different worktree and drain the same set, and it **resumes the bound worktree**, not your cwd. The escape hatch is the operator binding/override, which takes precedence. The trade is coherence (a set keeps its checkout and branch) over follow-me, and it is chosen on purpose.
- Bindings become universal after a set's first drain, so the dashboard's fork-free read path (ADR-0060) covers every drained set directly from the bindings table.
- An unsatisfiable queue default for a bare repo with no config trunk is the same config-class error as ADR-0059, not a transient in-place fallback.
- Glossary: redefines **Drain routing** (final step now persists a **Default binding**); adds **Default binding** and **Integration target**.
