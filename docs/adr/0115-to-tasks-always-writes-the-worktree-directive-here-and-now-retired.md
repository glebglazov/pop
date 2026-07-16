---
status: accepted
---

# to-tasks always writes the worktree directive; to-tasks-here-and-now is retired

> **Relates:** supersedes [ADR-0113](0113-to-tasks-here-and-now-binds-current-worktree-at-the-skill-layer.md) (the here-and-now wrapper is deleted), amends [ADR-0059](0059-task-set-may-declare-a-worktree-directive.md) (the directive is no longer opt-in), and depends on [ADR-0072](0072-worktree-directive-is-queue-only-foreground-implement-binds-the-current-checkout.md) (the directive stays Queue-only). The N-sets-to-one-checkout sharing it enables is handled by [ADR-0116](0116-managed-worktree-teardown-is-reference-counted.md).

## Context

ADR-0059 made the **Worktree directive** opt-in: planning skills wrote it "only when the human explicitly requests it; otherwise omitted", so the default `to-tasks` output left every set unbound and Queue-undrainable until a human bound a checkout by hand. ADR-0113 then bolted on a second skill, `to-tasks-here-and-now`, that force-bound the current worktree behind a guard refusing the trunk, pop-managed, and already-bound checkouts. Two skills, and the common case (plan from the feature worktree you're sitting in, then let it run there) still took extra ceremony.

## Decision

`to-tasks` **always** writes a worktree directive — it never omits the key.

- **Default:** `{ "name": "<current-worktree-basename>" }`, resolved via `basename $(git rev-parse --show-toplevel)` — the portable operator-facing name, never a path, never the literal `current`. Written **uniformly** for whatever checkout the skill runs in — feature worktree, **Trunk worktree**, pop-**managed**, or already-bound alike — with no guard, warning, or refusal.
- **`managed`/`isolated` argument:** overrides to `{ "managed": true }` (pop forks its own isolated worktree). There is no "omit" arm.
- **`auto_drain`:** off by default; enabled **only** by an explicit `auto-drain`/`drain` argument, written silently alongside whichever worktree arm regardless of checkout.

`to-tasks-here-and-now` is deleted; its trigger phrases ("here and now", "let it run here") are **not** absorbed into `to-tasks`.

**Trunk is not special.** ADR-0113 refused the trunk on the premise that a `{ "name": "<trunk>" }` directive "would never be Queue-drainable". That premise is false: `resolveNamedWorktree` matches the directive name against every entry of `git worktree list --porcelain`, and the main/trunk working tree is always in that list — so the Queue *does* adopt and drain there. The real consequence — an unattended `auto_drain` drain committing onto the main branch of the checkout you are actively using — is **accepted, not guarded**.

## Considered options

- **Keep the two-skill split (0113).** Rejected — the default skill leaving sets unbound is friction, and the guard's central justification was factually wrong.
- **Auto-bind only in an adoptable feature worktree; omit/warn/refuse elsewhere.** Rejected — the operator repeatedly chose one uniform rule over per-checkout special-casing; simplicity of "always write the name you're in" won over guarding unlikely footguns.
- **Fold auto_drain in as the default too.** Rejected — that collapses `to-tasks` into the old here-and-now (every plan auto-drains); auto_drain stays an explicit opt-in.
- **Guard only the `auto_drain` + trunk combo.** Rejected — the operator chose to keep even that flat, accepting the commit-onto-main consequence.

## Consequences

- Every `to-tasks` set is Queue-routable by name on first registration; the "needs bind" state no longer arises for skill-authored sets (except a `managed` directive with no resolvable trunk).
- A foreground `pop tasks implement` still ignores the directive and binds the current checkout (ADR-0072), so authoring in a feature worktree and then implementing there is unchanged.
- Writing `{ "name": ... }` for a checkout another set already uses creates N-sets-to-one-checkout sharing — its teardown hazard is resolved by ADR-0116.
- `cmd/catalog.go` drops the `to-tasks-here-and-now` source and its listing; `cmd/skills/pop/to-tasks-here-and-now/` is removed.
