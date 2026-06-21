---
fragment: 58b9c959
generation: 0004
branch: grill-auto-commit
---

+ Unverified Task set
  A Task set whose only remaining open work is human-in-the-loop verification: every AFK task is done or skipped, and one or more open HITL tasks stand between the set and Done. It is the post-agent end of the HITL lifecycle — the agents are finished and the only thing left is a human's eyes on the result. Distinct from a **Human-blocked Task set**, where open AFK work still sits behind a human gate. Derives status UNVERIFIED and a matching `unverified` **Drain outcome**, so the queue surfaces "awaiting your check" rather than "blocked".
  avoid: Blocked Task set, Human-blocked Task set, pending verification, review state
  under: Tasks

~ Human-blocked Task set
  A Task set with at least one still-open AFK task that cannot run because human-in-the-loop work must happen first — the pre-agent or mid-flow end of the HITL lifecycle (a setup/harness HITL at the bottom, or a decision HITL in the middle). It derives status BLOCKED and a `blocked` **Drain outcome**: real agent work remains, gated on a human. Contrast an **Unverified Task set**, where no open AFK work remains and only human verification is left. Implement reports the condition and stops; the task executor never automatically runs HITL tasks. On stopping, pop prints the blocking task body verbatim and advises the recovery paths: Complete the task once the human work is done, edit the task file and re-run, or skip the task to defer it and unblock its dependents (Skipped task). The blocked row also shows a copy-paste complete hint, symmetric with the open hint on Failed rows.
  was: A Task set with unfinished tasks but no eligible AFK task because human-in-the-loop work must happen first. Implement reports the condition and stops; the task executor never automatically runs HITL tasks. On stopping, pop prints the blocking task body verbatim — the human sees what to do without opening the file — and advises the recovery paths for the blocking HITL task: Complete task once the human work is done, edit the task file and re-run, or skip the task to defer it and unblock its dependents (Skipped task). The blocked row also shows a copy-paste complete hint, symmetric with the open hint on Failed rows.

~ Task set status
  The status derived from a discovered Task set whenever a tasks command runs. A **Ready** Task set has at least one eligible task; a **Done** Task set has only done tasks; a **Failed** Task set has at least one failed task. When no AFK task is eligible and the set is neither Done, Failed, nor Deferred, the disposition splits by whether agent work remains: an **Unverified** Task set has no open AFK task left — only human verification (HITL) stands before Done; a **Blocked** Task set still has an open AFK task gated behind a human-in-the-loop task. Pop does not persist a separate completion flag, so artifact changes naturally affect the next derived status.
  was: The status derived from a discovered Task set whenever a tasks command runs. A **Ready** Task set has at least one eligible task; a **Done** Task set has only done tasks; a **Failed** Task set has at least one failed task; a **Blocked** Task set is unfinished but has no eligible task. Pop does not persist a separate completion flag, so artifact changes naturally affect the next derived status.

+ Drain outcome
  The terminal disposition of a task-set drain, written to a machine-readable record on exit so the queue supervisor can react without parsing human output: done, failed, blocked, unverified, deferred, quota-paused, or interrupted. The HITL gate splits into two: `unverified` when only human verification remains (no open AFK work), `blocked` when open AFK work still sits behind a human gate. The queue's status, run baseline, and journal surface the two as distinct buckets so "awaiting your check" is not lost inside "blocked".
  avoid: drain disposition, drain result
  under: Tasks
