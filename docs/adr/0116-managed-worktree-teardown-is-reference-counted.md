---
status: accepted
---

# Managed-worktree teardown is reference-counted; checkout↔set exclusivity is retired

> **Relates:** enables the sharing introduced by [ADR-0115](0115-to-tasks-always-writes-the-worktree-directive-here-and-now-retired.md); amends the **Archive** teardown path and the [ADR-0072](0072-worktree-directive-is-queue-only-foreground-implement-binds-the-current-checkout.md) / [ADR-0036](0036-implement-adopts-its-checkout-into-the-binding-model.md) binding model.

## Context

Once `to-tasks` writes `{ "name": "<current>" }` uniformly (ADR-0115), a set authored from inside another set's checkout adopts the **same** path — N sets to one checkout. The prior model assumed each checkout belonged 1:1 to one set, and `Archive` tore down a managed worktree whenever the archived binding's `Provisioned` bit was set. Under sharing that bit-keyed teardown breaks two ways: it can delete a managed worktree out from under a still-active adopter, and — because an adopter's own binding is `adopted` (Provisioned=false, never self-torn-down) — if the original managed set is archived first, the worktree is skipped there and then leaks forever.

## Decision

Archive's managed-worktree teardown becomes **reference-counted**. Pop deletes a managed worktree and its branch only when:

1. the archived set's checkout lives **under the managed-worktree root**, and
2. **no other non-archived** Task set still holds a **Worktree binding** to that path.

The trigger is keyed on the **checkout path and its live referent count**, decoupled from the archived binding's `Provisioned` bit. So among several sets sharing one managed checkout, whichever is archived **last** fires the confirm-gated delete — and an adopting (`adopted`) set can be that trigger. The one-checkout-to-one-set exclusivity invariant is retired.

The interactive **Drain target picker** keeps excluding managed and already-adopted worktrees from its adopt list — a curated safe choice for the hands-on path — so N:1 sharing arises **only** through the manifest directive path, never by tempting a human into it interactively.

## Considered options

- **Keep the `Provisioned`-bit trigger, just skip delete when others are bound.** Rejected — leaks when the original managed set is archived before its adopters (the skipped-then-orphaned case).
- **Forbid sharing: to-tasks omits/warns on a managed or bound checkout.** Rejected — the operator chose the uniform "always write the name" rule (ADR-0115) over guarding this footgun.
- **Open the interactive picker to N:1 too.** Rejected — needless surface that invites humans into the shared-branch hazard; the picker stays 1:1.

## Consequences

- `TeardownManagedWorktree`'s "only for provisioned bindings; adopted checkouts are never torn down" contract relaxes: teardown now keys on checkout-under-managed-root + zero live referents, so an adopted binding can be the last referent that triggers the delete.
- A new helper counts non-archived sets binding a given checkout path; the archive path consults it before prompting.
- Sets sharing one managed checkout share its single branch; their commits interleave on that branch, serialized by the **Runtime execution lock**. This is an accepted consequence of sharing, not a bug.
- Engine implementation (in `tasks/binding/lifecycle.go` and the archive path) is a follow-up; this ADR records the decision reached during design.
