---
status: accepted
---

# Drain outcome is the process exit reason, not the set's work disposition

A Drain's terminal state records only *how its process ended* — `finished`,
`quota_paused`, `interrupted`, or `crashed` — never the set's resulting work
disposition. Whether a `finished` Drain left the set done, failed, blocked,
unverified, or deferred is read from the manifest-derived **Task set status**,
not copied onto the Drain.

## Context

The pre-store `DrainOutcome` enum carried eight values, five of which
(done / failed / blocked / unverified / deferred) restate the manifest-derived
Task set status. Persisting those on the Drain duplicates layer 1 and
reintroduces exactly the drift ADR-0006 exists to avoid. The remaining outcomes
— `quota_paused`, `interrupted`, and the new `crashed` — are the only ones the
manifest cannot derive: they describe the process, not the work.

## Considered Options

- **Exit-reason-only terminal (chosen).** Four terminals describing how the
  process ended. The set's disposition is always derived. `quota_paused` carries
  the exhausted agent preset and reset instant — a fact about that execution —
  and produces a machine-global agent cooldown.
- **Full eight-value disposition on the Drain (rejected).** Convenient for the
  supervisor (no re-derivation) but duplicates layer 1 and can drift from the
  manifest.

## Consequences

- The two scheduling signals the supervisor needs survive on this axis:
  clean = {`finished`, `quota_paused`} resets backoff; abnormal =
  {`interrupted`, `crashed`} drives it.
- Operator-facing "X done / X failed" lines are produced by joining a `finished`
  Drain with the set's derived status at that moment, not by reading the Drain
  alone.
- `quota_paused` is the only outcome that drives agent fallback; the agent
  cooldown it produces lives on the agent (machine-global), not the set.

Cross-references: refines [ADR-0055](0055-drain-execution-lifecycle-is-a-durable-store.md);
preserves [ADR-0006](0006-manual-issue-state-overrides.md).
