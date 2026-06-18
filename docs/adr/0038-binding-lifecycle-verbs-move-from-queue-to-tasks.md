# Binding lifecycle verbs move from `queue` to `tasks`; `abandon` becomes `unbind-worktree`

Status: accepted — deferred/unbuilt (same posture as ADR-0036); amends ADR-0035 (which introduced `queue bind-worktree`/`queue abandon`), builds on ADR-0036 (binding is shared/module-owned and trigger-agnostic), and refines ADR-0013 (issues→tasks rename, `queue` reserved for the scheduler)

## Context

ADR-0031 recorded **Worktree binding**s in **Queue** daemon-private state, so the verbs that
manage them — `bind-worktree`, `abandon`, `integrate` — naturally lived under `pop queue`. ADR-0036
then moved the binding model into a shared per-repository store owned by a dedicated binding module
that **both** `pop queue run` (the AFK provisioner) and `pop tasks implement` (the attended adopter)
call, establishing its thesis: **how a set is triggered (queue vs implement) is orthogonal to which
checkout it drains in**. The binding is the "which checkout" axis — trigger-agnostic.

All three verbs take a **Task set identifier** and act on that set's drain checkout, exactly like
`pop tasks status`, `pop tasks archive`, and `pop tasks implement`. Their `queue` home is a vestige
of the daemon-private era ADR-0036 already ended; under `queue` they are the only subcommands not
about running the scheduler.

## Decision

The trigger-agnostic binding lifecycle moves to the **Task set** namespace.

- **`pop tasks bind-worktree <set>`, `pop tasks unbind-worktree <set>`, `pop tasks integrate <set>`**
  replace the `queue` verbs. `queue` shrinks to the AFK daemon surface: `run`, `status`, `log`.
- **`abandon` is renamed `unbind-worktree`** — the symmetric inverse of `bind-worktree`. Behavior is
  unchanged: forget-only for an **adopted** binding (checkout retained), teardown for a **managed**
  binding (`git worktree remove` + `branch -D`), gated on the `Provisioned` bit. The per-binding
  confirmation prompt still conveys the managed-teardown danger, so the neutral name does not hide it.
- **Old `queue` verbs are hard-removed**, no aliases. The Queue daemon performs integration and
  release through the `queue` package functions (`queue.IntegrateWithOptions`, `queue.AbandonWithOptions`)
  internally — never by shelling out to the CLI — so its runtime path is untouched; only human/script
  callers change.

## Considered options

- **Move only `bind-worktree` + `unbind-worktree`, leave `integrate` in `queue`.** Rejected — splits
  one trigger-agnostic lifecycle across two namespaces; integration is trigger-agnostic per ADR-0036.
- **Keep the verbs in `queue`, add `tasks` aliases for discoverability.** Rejected — two homes for one
  concept, the model muddiness this removes.
- **Keep the name `abandon` in `tasks`.** Rejected — `pop tasks abandon <set>` reads as "discard the
  task set's tasks," which it never does; bind/unbind symmetry aids discovery and the teardown warning
  already lives in the confirmation prompt.
- **Hidden deprecated `queue` aliases for one CalVer cycle.** Rejected — unnecessary cruft for a
  personal single-user tool on a monthly cadence; the daemon does not depend on the CLI verbs.

## Consequences

- `cmd/queue.go` loses `bind-worktree`/`abandon`/`integrate` and their flags; `cmd/tasks.go` gains
  `bind-worktree`/`unbind-worktree`/`integrate`. The `queue.*WithOptions` package functions stay put
  (the daemon calls them); only the cobra command wiring and shell completion relocate.
- Glossary: **Abandon worktree** is renamed **Unbind worktree**; **Bind worktree** and **Worktree
  binding** command citations move from `pop queue …` to `pop tasks …`. Recorded live in a CONTEXT
  fragment.
- Deferred: like ADR-0036, this records the decision; no code ships from it yet.
