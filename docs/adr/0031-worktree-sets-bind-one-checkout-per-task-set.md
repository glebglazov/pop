---
status: accepted
---

# Worktree sets bind one checkout per Task set

ADR-0029 made Pop own `git worktree add` for **Worktree sets**, but the first implementation
provisioned a **new timestamped checkout on every queue spawn**. That contradicted ADR-0028's
"serial in one worktree" model and the task executor's posture on **Failed** tasks and **Agent
quota pause** — partial runtime changes are preserved for resume, yet a re-spawn forked afresh
from trunk and stranded prior work in orphaned directories.

A **Worktree binding** is now the durable 1:1 association between a **Task set identifier** and
its provisioned checkout for the set's active execution lifetime. Bindings live in the SQLite
`bindings` table (ADR-0055), keyed by repository identity plus set id (not runtime path), and carry
a `Provisioned` flag distinguishing managed from adopted checkouts (ADR-0052). Lifecycle verbs live
under `pop tasks` (ADR-0038). Re-spawns reuse the bound
checkout; Pop never silently re-provisions when a binding exists. If the bound checkout is missing
or no longer registered with git, spawn fails loudly and directs the human to repair git state or
**Abandon worktree**. Bindings release — and the checkout is torn down — on integration or
`pop queue abandon`; **Archive** does not release a binding.

Checkouts use a **stable path** derived from the set identifier (no timestamp suffix per spawn).
Branch names may still carry a stamp for git-ref uniqueness across abandon-and-retry cycles.
Pre-binding orphan checkouts are not auto-adopted; a one-time manual seed is the operator's escape
hatch. All other set-scoped queue state uses the same repository-plus-set key shape so crash backoff
follows the set when the checkout path is stable. (Mergeability was later removed entirely — ADR-0070.)

After **Abandon worktree**, the stable slot is free; a later drain may provision a fresh binding
from current trunk **HEAD**. Abandon confirms interactively for both **Failed** and **Done** sets
awaiting integration; `--yes` skips confirmation.

## Considered options

- **Keep per-spawn timestamped checkouts.** Rejected — re-spawn after failure loses in-progress
  implementation work and accumulates orphan directories.
- **Auto-adopt the newest orphan when no binding exists.** Rejected — ambiguous when multiple
  orphans exist; silently picks the wrong checkout.
- **Silently re-provision when a binding's checkout is missing.** Rejected — repeats the data-loss
  failure mode; the human must **Abandon worktree** or repair git state explicitly.
- **`pop queue bind` to seed bindings.** Rejected — one-time manual state edit is enough for
  migration; YAGNI for a permanent command.
- **Re-key only bindings; leave backoffs path-keyed.** Rejected — crash backoff already demonstrated
  the fragmentation problem when path and set diverge.

## Consequences

- The SQLite `bindings` table (ADR-0055) holds the worktree bindings; set-scoped keys move from
  `runtimePath + setID` to `repoIdentity + setID`. (The design originally landed these in a
  `state.json` map, migrated once into the store.)
- `prepareWorktreeDrain` looks up bindings before calling `git worktree add`.
- `pop queue abandon <set>` is the human release path without integration.
- ADR-0028's deferred lifecycle note for non-DONE paths is fulfilled for failure/resume; DONE
  integration lifecycle remains as ADR-0030 describes.
