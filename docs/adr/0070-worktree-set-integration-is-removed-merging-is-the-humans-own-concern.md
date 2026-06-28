---
status: accepted
supersedes: [ADR-0030, ADR-0051]
---

# Worktree-set integration is removed; merging is the human's own concern

> **Relates:** amends [ADR-0052](0052-drain-checkout-is-chosen-not-auto-provisioned.md) and [ADR-0062](0062-no-directive-drain-persists-a-default-binding.md) (the Integration-target fallback they relied on is gone). Does **not** touch `pop integrate <agent>` (ADR-0010/0011/0064/0065) — that is the unrelated monitor-setup feature that keeps the word "integrate".

Pop no longer has a worktree-merge concept. We delete the `Integrate` verb, the **Integration backlog** view, **Mergeability** (the `git merge-tree` dry run), `auto_merge_clean`, the dashboard `I` key, and the "ready-to-merge / conflicts" bucket in `pop queue status`. When a drain lands Done in a worktree, pop says nothing about merging: the human reconciles that branch into wherever they want via a pull request or a manual merge, entirely outside pop.

We also retire the **Integration target** abstraction in *both* of its roles. Its merge-target role dies with integration. Its second, quieter role — "the checkout an unbound Queue drain falls back to" — is deliberately *not* replaced: the Queue now never invents a checkout (see ADR-0072). An unbound auto-drain set with no directive surfaces as a needs-bind fault instead of silently landing on trunk.

## Why

"Integration" was the most overloaded word in pop. It named two unrelated features — `pop integrate <agent>` (monitor wiring) and worktree-set merge reconciliation — and the merge half carried real machinery (backlog, mergeability, merge-tree, auto-merge consent) that the author consistently found confusing and rarely trusted. Merging a branch is something git and forge tooling already do well; pop owning a half-automated, consent-gated merge path added surface area and a second source of truth for "what's mergeable" without removing the human judgement a merge needs. Dropping it leaves pop responsible only for *running* work in a checkout, never for landing it.

## Consequences

- A Done set's work is discoverable via the dashboard destination column (its branch) and `pop worktree`; pop offers no merge action and computes no mergeability verdict.
- The Queue dashboard keeps showing a Done set **only while it still holds a managed Worktree binding**, as a clean-up reminder, since the backlog that used to track it is gone.
- The managed-worktree lifecycle loses its "torn down on integration" trigger; teardown moves entirely to Archive (ADR-0071).
- ADR-0030 and ADR-0051 are tombstoned — their entire subject (attended integration, binding-driven backlog membership) no longer exists.
