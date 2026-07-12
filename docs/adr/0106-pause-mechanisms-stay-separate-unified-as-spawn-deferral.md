# Pause mechanisms stay separate, unified read-side as Spawn deferral

Two subsystems answer "why is this Ready set not spawning": crash backoff/park (derived from drain history, owned by the queue, no live process) and recovery waiters (registered by a live quota-paused process, owned by implement per [ADR-0100](0100-quota-recovery-is-checkout-scoped-in-implement.md)). Readers such as `selectReadySets` consult both.

Decision: do **not** merge the mechanisms. Their owners and substrates genuinely differ — backoff being derived-not-stored is deliberate [ADR-0055](0055-drain-execution-lifecycle-is-a-durable-store.md) design, and a merged table would have the queue writing rows for sets with no live process, inverting ADR-0100 ownership. Instead unify the **read side only**: readiness selection returns a single **Spawn deferral** value (reason + optional until-instant) with three species — Crash backoff (timed), Parked (indefinite, human-cleared), Agent quota recovery wait (process-owned). This is the shape the future global scheduler consumes; the mechanisms behind it remain independent.

Rejected: merging into a generalized `recovery_waiters` table (regresses derived-not-stored, inverts ownership); leaving two vocabularies for the scheduler session to inherit.
