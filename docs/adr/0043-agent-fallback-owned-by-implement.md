---
status: accepted
supersedes:
  - ADR-0034 (in part — cooldown ownership moves out of the daemon)
---

# Agent fallback is owned by `tasks implement`, not the queue

The ordered "agents to try" pool and its quota cooldown move out of the queue daemon and into the task executor, so every `pop tasks implement` — manual or queue-spawned — walks the list itself.

## Context

Previously the queue owned agent selection: `[queue].agents` was an ordered pool, `selectDefaultAgent` picked the first agent not on cooldown, and the daemon passed it to implement as a single `--default-agent`. Fallback happened only *across* daemon scans, via `AgentCooldowns` persisted in the daemon's `state.json`. Manual implements had no list and no cross-run quota memory. We want the "try agents in order" behavior on **all** implements.

## Decision

`pop tasks implement` accepts an ordered agent list (repeated `--agent`, else the config fallback list, else `claude`) and walks it: each task runs on the first live agent and falls through to the next **only on an Agent quota pause**. The per-preset cooldown graduates from the daemon's `state.json` into a tasks-owned, machine-global, file-locked store (reusing the `state_lock.go` discipline) that every implement reads before spawning and writes on a pause. Implement skips cooling agents pre-spawn and returns a quota pause carrying the earliest reset when the whole list is exhausted. The queue stops selecting agents entirely — it spawns plain `pop tasks implement` and only reads the store for status display; `selectDefaultAgent` and `--default-agent` are deleted.

## Consequences

- Many ephemeral implement processes now write one machine-global cooldown file (the daemon fans drains in parallel per ADR-0027), so the store needs file-locked read-modify-write — accepted over re-coupling writes to the daemon, which would deny manual implements the shared memory.
- Advancement is quota-only (interchangeable agents); failure-based or per-attempt agent diversity is explicitly out of scope and could layer on later.
- The config fallback list, named `[workload] default_agents` when this ADR landed, later moved to `[tasks.implement].agents` — see [0092-task-config-parented-under-tasks-with-verb-named-sub-tables.md](0092-task-config-parented-under-tasks-with-verb-named-sub-tables.md). The ownership decision above is unchanged; only the key name is superseded.
- ADR-0034's reset-aware cooldown survives unchanged in mechanism; only its owner moves from daemon to tasks module.
