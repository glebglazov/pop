---
status: accepted
---

# Managed-worktree teardown happens only at Archive; Unbind is forget-only

> **Relates:** amends [ADR-0026](0026-archive-is-a-reversible-task-state-flag.md) (Archive gains one confirm-gated destructive branch) and inverts the managed-teardown half of the Worktree binding model from [ADR-0036](0036-implement-adopts-its-checkout-into-the-binding-model.md). Depends on [ADR-0070](0070-worktree-set-integration-is-removed-merging-is-the-humans-own-concern.md) removing the old "torn down on integration" trigger.

With integration gone, a pop-created (**managed**) worktree needs exactly one place to die. We make that place **Archive**, and only Archive. Archiving a set that holds a managed Worktree binding prompts `delete managed worktree? [y/N]`; on confirm pop runs `git worktree remove` plus branch deletion and releases the binding. Declining aborts the archive.

Correspondingly, **Unbind worktree becomes always forget-only** — it never deletes, even for a managed binding (previously it tore the managed checkout down). So the "file the set but keep its worktree" path is explicit: Unbind first (non-destructive), then Archive the now-unbound set metadata-only.

## Why

The old model deleted managed worktrees on two implicit triggers (integration and unbind). One is now gone, and the other — unbind — conflated "I'm done with this association" with "destroy my checkout", which made unbind feel dangerous and made keeping a worktree awkward. Routing *all* destruction through a single confirm-gated verb (Archive) means there is exactly one place a directory can disappear, it always asks first, and the reversible-metadata guarantee of ADR-0026 still holds for everything except that one consciously-confirmed deletion. `--yes` archives skip the per-set prompt and delete, since `--yes` is already the "I accept the consequences" channel.

## Consequences

- ADR-0026's "fully reversible / non-destructive" invariant is narrowed: it holds for the Task-set metadata, markdown, manifest, progress, streams, and statuses. The optional managed-worktree deletion is a separate confirmed act that Unarchive cannot undo.
- Unbinding a managed set leaves a pop-created worktree on disk owned by nobody for teardown; the human removes it via `pop worktree` if they don't re-bind. This is the deliberate cost of making unbind safe.
- A foreground `pop tasks implement` that rebinds a set off an idle managed binding is the one *other* place a managed worktree can be deleted — and it, too, asks first (ADR-0072).
