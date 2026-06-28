---
status: deferred
---

# Task execution modules split from queue

> ⚠️ **DEFERRED — intended design, not shipped behavior.**

Worktree binding, drain routing, integration, and Implement orchestration were scattered across root `binding/`, `cmd/tasks.go`, and `queue/` (including a `DaemonState.WorktreeBindings` mirror of `bindings.json`). ADR-0036 and ADR-0046 established shared routing semantics; ADR-0038 moved binding lifecycle CLI verbs to `pop tasks` but left implementations under `queue/`.

Three task-scoped modules replace that layout:

- **`tasks/binding`** — binding store, **Drain routing** (`RouteDrainCheckout` with trigger policy), **Bind worktree** / **Unbind worktree**, provision/adopt. Queue and Implement call this module directly; `DaemonState` no longer mirrors bindings.
- **`tasks/integration`** — **Mergeability** cache (persisted beside bindings, not in daemon state), **Integrate**, **Integration backlog** helpers. Trigger-agnostic; replaces `queue.IntegrateWithOptions` and related mergeability paths.
- **`tasks/implement`** — whole-set **Implement** orchestration: route → drain → integration epilogue. `cmd/tasks.go` keeps Cobra wiring only; single-task runs stay in `tasks/executor.go`.

**Considered:** one package (`tasks/binding` absorbing integration) — rejected because **Integration backlog** and **Worktree binding** are distinct glossary concepts with different persistence and callers.

**Consequences:** `queue/` retains scheduling (daemon, dashboard spawn, journal, backoff). Routing inconsistencies (e.g. Queue provision fork point vs **Execution base**) are fixed when `RouteDrainCheckout` lands, not deferred. ADR-0038's note that the daemon calls `queue.IntegrateWithOptions` is superseded — the daemon calls `tasks/integration` instead.
