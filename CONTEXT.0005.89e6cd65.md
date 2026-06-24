---
fragment: 89e6cd65
generation: 0005
branch: master
---

+ Drain
  One supervised execution of draining a Task set, tracked through an explicit
  lifecycle from start to a terminal disposition (its Drain outcome). A Task set
  may be drained many times — after a reset, a crash, or a quota pause — and each
  is a distinct Drain; a set's Drain history is the ordered record of them. The
  Drain, not the Task set, carries execution lifecycle state; the set's
  manifest-derived Task set status (what work remains) is a separate, derived
  concern.
  avoid: Run, attempt, drain record
  under: Queue

~ Drain outcome
  How a Drain's process ended — its exit reason, not the set's work disposition:
  finished (the drain ran to its own stopping point), quota-paused (an agent
  preset hit quota), interrupted (deliberate SIGINT teardown), or crashed (the
  process died unexpectedly, recorded by reconciliation rather than by the drain
  itself). The set's resulting work disposition — done, failed, blocked,
  unverified, deferred — is read from the manifest-derived Task set status, never
  restated on the Drain. finished and quota-paused are clean exits; interrupted
  and crashed are abnormal and drive crash backoff.
  avoid: Task set status, drain disposition, drain result
  was: The terminal disposition of a task-set drain, written to a machine-readable record on exit so the queue supervisor can react without parsing human output: done, failed, blocked, unverified, deferred, quota-paused, or interrupted. The HITL gate splits into two: `unverified` when only human verification remains, and `blocked` when open AFK work still sits behind a human gate.
