---
status: accepted
---

# Sibling packages borrow the process-cached store handle through tasks.Deps

## Context

[ADR-0118](0118-execution-state-store-handle-is-process-cached-and-liveness-injected-at-open.md) made the **Execution-state store** handle process-cached behind `tasks.Deps.Store(createIfMissing)` — but only `tasks/` converged on it. `routine/` kept its own per-call opener (fresh connection, WAL setup, and migration run per operation — exactly the cost ADR-0118 removed) with a verbatim copy of the test-isolation guard; `queue/` kept a third opener with its own path derivation and a divergent `/tmp/pop/pop.db` fallback. One file had three path derivations and two guard copies, kept in lockstep by convention. Meanwhile `tasks.AllRoutineRuns` closed the borrowed shared handle, poisoning the process cache for every later store call — masked today only because its lone caller (`pop queue log`) reads it last in a one-shot process, and `CloseStore` has no production caller to heal it.

## Decision

- **`tasks.Deps.Store(createIfMissing)` is the only opener.** `routine/` funnels through its existing `Deps.Tasks` field — its `openExecutionStore`/`openExecutionStoreIfExists` helpers stay as thin named delegations preserving the create/if-exists modes, minus the per-call `Close()`. `queue/`'s `openRoutineStore` dies. Both rival path derivations die with their openers, including the `/tmp` fallback: a machine where home resolution fails errors instead of scattering state.
- **The seam exposes the handle.** Borrowers use the `store` API directly. tasks-layer wrappers survive only where they add behaviour — `BeginDrain` (identity resolution, exit-code mapping), `ReadCheckoutClaim` (degrade-to-spawnable policy). The pure pass-through `AllRoutineRuns` is deleted, and its close-the-shared-handle bug with it. Borrowers never close; `CloseStore` remains the only closer.
- **One guard.** `routine/`'s copy of `guardTestStorePath`/`prodDataDirAtStartup`/`realProductionDataDir` dies with its opener; the guard fires only inside the accessor.

## Considered Options

- **Push the cache down into `store/`** so routine and queue need not reach through tasks — rejected: it drags XDG/env path derivation and liveness wiring into a package that is pure persistence, and every consumer already carries a `*tasks.Deps`.
- **Keep the handle private to tasks; sibling packages call operation wrappers** — rejected: it manufactures exactly the pass-through layer the deletion test condemns, one wrapper per store method per consumer.

## Consequences

- `routine/` depends on `tasks/` for storage access. Deliberate: the dependency already existed (`routine.Deps.Tasks` wired in `DefaultDeps` and queue's `routineDeps()`); this makes it load-bearing rather than incidental.
- Routine operations stop paying a connection + migration run per call; long-lived surfaces (Routine dashboard) hold the borrowed handle for process life, per ADR-0118's model.
- The remaining store mirror types (`DrainRecord`, `IntegrationEvent` and their converters) are a separate cleanup under ADR-0118's zero-converters principle — out of scope here and unchanged by this decision.
