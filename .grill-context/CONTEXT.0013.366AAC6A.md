---
fragment: 366AAC6A
generation: 0013
branch: master (grill: out-of-band mutators + pause vocabulary)
---

+ Out-of-band mutation
  A change to a Task set's verdicts or manifest made from outside a drain — e.g. the Accept or Remediate disposition issued from the standalone CLI. Permitted only under Checkout quiescence.
  avoid: external mutation, offline edit

+ Checkout quiescence
  The state of a checkout with no live drain executing and no Checkout gate hold registered. Precondition for any Out-of-band mutation.
  avoid: idle checkout, unlocked

~ Remediation task
  An AFK task spawned to fix Verify findings — by the Verifier on FIXABLE (auto origin) or by a human via the Remediate disposition (human origin). Every Remediation task carries its Remediation origin.
  was: AFK task the Verifier writes on FIXABLE; loop bounded by max_remediation_depth, after which the set parks at VERIFY-FAILED.

+ Remediation origin
  Whether a Remediation task was spawned by the Verifier (auto) or by a human disposition (human). Determines whether the task counts toward Remediation depth.

+ Remediation depth
  The count of consecutive auto-origin Remediation tasks since the last human-origin one. When it would exceed the configured maximum, the Verifier stops spawning and the set parks at VERIFY-FAILED. A human Remediation resets the count — human intervention grants fresh auto budget.
  avoid: remediation count, loop counter

+ Spawn deferral
  The read-side answer to why a Ready set is not being spawned right now: a reason plus an optional until-instant. Three species — Crash backoff (timed), Parked (indefinite, human-cleared), Agent quota recovery wait (owned by the paused process). One vocabulary over deliberately separate mechanisms.
  avoid: spawn hold, pause, suppression, block
