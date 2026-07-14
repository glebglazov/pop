# `to-tasks-here-and-now` binds the current worktree at the skill layer

`to-tasks-here-and-now` is a thin wrapper over `to-tasks` that pre-answers two manifest decisions: it seeds `auto_drain: true` (per [ADR-0047](0047-manifest-auto-drain-seeds-at-registration.md)) and binds the set to the checkout it was authored in. We resolve "here" **at the skill layer** — the wrapper reads the current worktree's operator-facing name (`basename $(git rev-parse --show-toplevel)`) and writes the existing `{ "name": "<worktree>" }` manifest arm — so the engine needs no change. Because managed worktrees, the trunk, and already-bound checkouts are excluded from the adopt list, the wrapper **refuses** when the current checkout isn't a plain, unbound feature worktree, telling the user to author from a feature worktree or use `{ "managed": true }` instead — rather than writing a binding that silently won't drain.

## Considered Options

- **Skill-layer reuse of `{name:<current>}` (chosen):** no engine footprint, honouring the constraint that pop's drain engine stays untouched; the trunk/managed limitation becomes an explicit guardrail.
- **New engine arm `{ "here": true }`:** `pop tasks register` would resolve it to `$PWD`'s checkout path via `AdoptWorktree`, covering trunk and managed checkouts too. Rejected: it is an engine change (schema + register wiring + reconcile) for a convenience the skill can express today.

## Consequences

- Here-binding is available only from a plain unbound feature worktree. From the trunk the wrapper refuses — even though the dashboard *does* support binding an inline drain to the trunk (ADR-0052). We deliberately trade that capability for a zero-engine-footprint skill; a user who wants unattended drain off trunk-HEAD asks for `{ "managed": true }`, which is the safer isolated default anyway.
- The wrapper is portable: it writes an operator-facing name, never a path, matching the manifest's cross-machine contract.
