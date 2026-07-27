---
status: accepted
supersedes: [ADR-0072]
---

# Managed worktrees are provisioned eagerly, at the operator's request

> **Relates:** supersedes [ADR-0072](0072-worktree-directive-is-queue-only-foreground-implement-binds-the-current-checkout.md), carrying forward its surviving foreground/Queue routing split (below). Amends [ADR-0052](0052-drain-checkout-is-chosen-not-auto-provisioned.md) — provisioning stays an explicit act, but now happens *at* the explicit act rather than at the drain that follows it.

`--managed` recorded an *intent* and provisioned nothing: the worktree was forked from the Trunk worktree at the first unbound Queue drain. That left a set in a state the binding model has no word for — registered, managed, and placed nowhere. Every consumer of the binding-to-runtime-path resolution silently substituted the trunk for such a set (the queue's dispatch claim gate and verdict resolution, the Work dashboard, the work snapshot), which produced a deadlock in practice: an unprovisioned managed set's claim target resolved to the trunk, so while anything drained on the trunk the set was deferred as claimed — and it could never provision the worktree that would have moved it off the trunk, because provisioning only happens at the drain it was being denied.

Decision: **`--managed` provisions immediately.** `pop tasks register --managed` and `pop tasks bind-worktree --managed` both fork a worktree from the Trunk worktree and record a Worktree binding at the moment the human asks. "Registered managed" and "has a checkout" become the same instant, so no surface has to answer where an unplaced set lives. Correspondingly, **binding-to-runtime-path resolution stops falling back to the trunk**: a set with no binding resolves to no checkout and is reported as unplaced. A repository with no resolvable Trunk worktree refuses the registration outright, and `--trunk <path>` supplies one — persisting it to repo config, since a bare repo would otherwise re-answer the same question on every managed register.

The fork base moves from drain-time trunk to register-time trunk, so a managed worktree whose branch carries no commits yet is **fast-forwarded onto current trunk at its first drain**; once the set has real work on its branch it is left alone.

**Carried forward from ADR-0072** (unchanged, restated here because that ADR is superseded): foreground `pop tasks implement` always runs in the current checkout and rebinds the set there, while `--in-worktree` provisions a managed worktree forked from the *current checkout's* HEAD; the Queue has no "current" and forks from trunk. The Queue never invents a checkout — a set with no binding is a needs-bind fault, not a silent landing on trunk.

## Considered Options

- **Keep lazy provisioning; make the claim gate managed-aware.** Rejected as the primary fix — it patches one of four consumers of a resolution that lies, leaving the same defect latent in the dashboard, the snapshot, and verdict resolution.
- **Provision during the dispatch decision, before the claim check.** Rejected — the supervisor's decision pass is deliberately fork-free (ADR-0060); running `git worktree add` inside it makes a pure decision function destructive.
- **Accept the staleness and never refresh the fork base.** Rejected — a set registered hours before it drains would branch off a trunk that has since moved, silently reintroducing work the drain was meant to build on.

## Consequences

- A managed set that never drains still owns a worktree and branch on disk until released. That cost is accepted: `--managed` is an explicit request, and teardown already has defined triggers.
- `pop tasks bind-worktree --managed`'s "nothing is adopted or provisioned now" contract inverts; its help text changes with it.
- Sets registered under the old lazy behaviour keep healing through the drain-time provisioner, which becomes reachable again once the trunk fallback is removed. No migration.
