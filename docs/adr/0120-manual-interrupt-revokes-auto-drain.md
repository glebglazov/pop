---
status: accepted
---

# A manual interrupt revokes auto-drain and is no longer an abnormal exit

## Context

**Auto-drain** is a sticky, multi-drain consent bit ([ADR-0098](0098-auto-drain-clears-at-drain-finalization.md), [ADR-0108](0108-auto-drain-marker-silenced-on-picked-up-rows.md)): it stays set across the drain → verify → remediation → drain progression and clears only at finalization to a terminal disposition. Queue eligibility is exactly `status == Ready && AutoDrain` (`queue/queue.go:959-963`), evaluated before backoff/binding. Separately, an interrupt was classified **abnormal** (`store.drainStateAbnormal`) and drove **Queue backoff**, because a pre-gate interrupt left the set Ready with nothing cooled and the daemon would re-spawn immediately.

The reported irritation: interrupt a queue-spawned drain and, because the bit is sticky, `pop queue run` picks the set back up and keeps working it — the human's Ctrl-C did not mean "keep going unattended."

## Decision

A manual interrupt of a live drain **clears the Auto-drain bit unconditionally at the moment of interrupt**. Because clearing removes the set from queue eligibility outright, the daemon stops re-firing it — no reliance on backoff.

The pre-interrupt value is snapshotted. Choosing **Continue** at the **[Interrupt gate prompt](0119-interrupting-a-live-drain-lands-on-an-interrupt-gate.md)** **revives** the snapshotted value (announced to the user); **Exit**, or a crash before the human chooses, leaves it cleared. Net: consent is truly discarded only when the human does not resume, yet it is cleared throughout the at-gate window so a crash-at-gate cannot let the daemon grab the set mid-decision. Re-enabling after Exit is a fresh human mark.

Consequently the `interrupted` terminal is **reclassified as a clean exit**: it is dropped from `drainStateAbnormal` (only `crashed`/kill remain abnormal) and no longer drives Queue backoff. The reason interrupt was ever lumped with crash — immediate re-spawn thrash — is gone, since interrupt now clears consent.

## Considered options

- **Clear auto-drain on first pick-up ("schedule once").** Rejected: this is exactly [ADR-0108](0108-auto-drain-marker-silenced-on-picked-up-rows.md)'s rejected option. A set normally needs several drains (drain → verify → remediation → drain); clearing on first pick-up makes auto-drain one-shot and strands the set mid-progression until a human re-marks, and also breaks crash recovery (reconcile re-fires an auto-drain Ready set). ADR-0098's finalization-clear already stops the daemon once the work is actually DONE/AWAITING-APPROVAL, which is what "schedule until done" really wants.
- **Clear only on Exit from the interrupt gate (conditional).** Rejected: leaves a window where a crash after interrupt-but-before-choosing keeps the bit set and the daemon can re-grab. Clearing unconditionally and reviving on Continue closes that window while preserving peek-and-continue.
- **Keep interrupt abnormal and rely on backoff to stop re-firing.** Rejected: backoff only delays and eventually parks; it never expresses "the human took over," and re-marking would immediately re-trip stale backoff.

## Consequences

- Auto-drain now has two clear triggers (terminal finalization, ADR-0098; manual interrupt, here); a set interrupted and exited must be re-marked to rejoin the queue.
- Sort/park/backoff logic that keyed on interrupt-as-abnormal must re-key on crash/kill only.
- The auto-drain revocation is shared by wait-state interrupts (quota-recovery, retry backoff) even though those keep their current exit path rather than showing the interrupt gate (ADR-0119 scope).
