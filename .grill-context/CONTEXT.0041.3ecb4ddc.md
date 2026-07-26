---
fragment: 3ecb4ddc
generation: 0041
branch: master (store-handle grilling session)
---

~ Execution-state store
  The machine-global SQLite database in pop's data dir holding every layer-2 execution fact — Drains, Worktree bindings, Verify verdicts, agent cooldowns, spawn intents, gate holds (ADR-0055/0118). Layer-1 Task set status stays manifest-derived on disk and is never stored here. A process holds exactly one lazily-opened cached handle, and every subsystem borrows that handle — nothing opens the database through a second path, and borrowers never close the shared handle (ADR-0140). Pure readers never create the database as a side effect. Process liveness (the PID + start-time predicate) is a policy the store receives at open, not a closure callers pass per operation.
  avoid: drain store, pop.db, daemon state, per-repository drain state
  under: Tasks
  was: The machine-global SQLite database in pop's data dir holding every layer-2 execution fact — Drains, Worktree bindings, Verify verdicts, agent cooldowns, spawn intents, gate holds (ADR-0055/0118). Layer-1 Task set status stays manifest-derived on disk and is never stored here. A process holds one lazily-opened cached handle to it; pure readers never create the database as a side effect. Process liveness (the PID + start-time predicate) is a policy the store receives at open, not a closure callers pass per operation.
