---
status: accepted
---

# Drain execution lifecycle is a durable store; Drain is the first-class entity

Pop's execution and integration state — currently scattered across drain-outcome
records keyed by runtime path, three flavours of file lock, a mergeability
record, a binding store, an append-only queue journal, and per-repo `state.json`
— moves into one machine-global SQLite store in pop's data dir. The **Drain**
(one supervised execution of draining a Task set) becomes a first-class,
durably-tracked entity carrying an explicit lifecycle, with full per-set Drain
history. Manifest-derived **Task set status** (layer 1) is untouched and stays on
the filesystem; only the non-derivable execution and integration facts (layer 2)
move to the store.

## Context

The Queue dashboard's `DONE · unknown` is the visible symptom of a deeper
fragility: every non-derivable execution fact is stored as a separate file, and
several states are *inferred from a missing file* — `crashed` = no drain-outcome
record, `unknown` mergeability = no mergeability record, `integrated` = binding
released. Each store carries its own read-modify-write race and its own lock. The
locks, the reinvented atomic-rename writes, and the absence-as-state conventions
all exist only because there is no transactional store.

Layer 1 — what work remains (Ready / Done / Failed / Blocked / Unverified /
Deferred) — is reliably derived from the manifest and must stay that way:
ADR-0006 deliberately persists no completion flag so a human editing task
markdown is reflected instantly. Layer 2 — running, terminal exit reason,
mergeability, integration, backoff, parking — is none of it derivable from the
manifest, and is exactly the scattered, fragile part.

## Considered Options

- **One global SQLite store for layer 2, files stay for layer 1 (chosen).**
  Layer-2 data is machine-local by nature (a binding points at a local checkout,
  a Drain describes a local process) and was never portable; the only portable
  artifact — the set's manifest, markdown, progress record, and captured streams
  — stays on disk (ADR-0016). One store gives the machine-global dashboard a
  single indexed query, makes Repository identity a foreign key, and replaces
  every hand-rolled lock with a transaction.
- **One SQLite DB per repository (rejected).** Preserves write-sharding for
  parallel cross-repo drains, but reintroduces the N-directory fan-out the global
  dashboard exists to avoid, and layer-2 data has no portability need that
  per-repo storage would serve.
- **Keep files, add discipline (rejected).** Does not remove the absence-as-state
  inferences or the multi-file partial-write races; the fragility is structural,
  not a matter of care.

## Consequences

- `crashed`, `unknown`, and `integrated` stop being inferred from missing files
  and become explicit, transactionally-guaranteed states and events. Nothing in
  layer 2 is ever read from absence.
- The runtime execution lock, state lock, supervisor lock, drain-outcome records,
  mergeability records, binding store, and queue journal collapse into tables in
  one store. The **Queue journal** becomes a view over Drain history.
- **Backoff** and **parking** are derived from Drain history (consecutive
  abnormal terminals) plus a durable park-clear event — no persisted timer or
  flag to drift.
- **Reconciliation is opportunistic:** every layer-2 reader runs a cheap bounded
  reconcile transaction (dead-PID `running` → `crashed`; SHA-gated mergeability
  refresh) before reading, so a foreground drain that crashes is healed by
  whoever next opens the dashboard — no new always-on daemon.
- A single SQLite writer serialises layer-2 writes across processes (daemon,
  foreground drains, dashboard). At pop's scale (a handful of concurrent drains)
  with WAL this is negligible; many-dozen parallel cross-repo drains would
  revisit it.
- Crash detection still faces PID reuse; the Drain row stores process start-time
  alongside PID — the same liveness logic the lock file used, now on a row.
- Existing on-disk layer-2 state must be migrated into the store.

Cross-references: preserves [ADR-0006](0006-manual-issue-state-overrides.md)
(derived task status) and [ADR-0016](0016-captured-stream-is-a-durable-telemetry-substrate.md)
(streams stay files); amends [ADR-0051](0051-integration-backlog-is-binding-driven.md)
— the dashboard poll becomes reconcile-then-read, but binding-driven backlog
membership and the no-per-poll-`git merge-tree` stance are retained via the SHA
gate. The exit-reason terminal model is [ADR-0056](0056-drain-outcome-is-the-process-exit-reason.md).
