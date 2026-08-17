---
fragment: 68695EAB
generation: 0021
branch: master
---

+ Authoring grant
  The named set of authoring acts an attended agent session may perform at a
  gate: write a new **Task set**, append a task to the set at hand, and run
  `pop tasks register` to validate what it wrote. It is a permission and grows
  over time, which is why it is stated separately from the prohibition beside it
  — widening what a session may write must not mean editing the rule that keeps
  it from deciding outcomes. Held identically by every attended session, since a
  follow-up thought arrives wherever the human is sitting.
  avoid: write surface, authoring permission, disposition invariant
  under: Unfiled (pending consolidation)

~ Remediation task
  An AFK task spawned to fix **Agent verification** findings — by the
  **Verifier** on FIXABLE (auto origin) or by a human via the **Remediate**
  disposition (human origin); every Remediation task carries its **Remediation
  origin**. A human-origin spawn is refused unless the set's **Verification
  mark** reads `verify-failed`, so remediation cannot be used as a general
  task-appender: adding work to a set that has not failed verification is an
  **Authoring grant** act, not a disposition. **Drain** picks it up like any
  eligible AFK task, bounded by the per-set **Remediation depth** cap, after
  which the set parks at VERIFY-FAILED. Spawning triggers **Verification
  invalidation** of the set's cached verdicts. Findings live only as a
  Remediation task's body — never as annotations inside another task's spec.
  was: An AFK task spawned to fix **Agent verification** findings — by the
    **Verifier** on FIXABLE (auto origin) or by a human via the **Remediate**
    disposition (human origin); every Remediation task carries its **Remediation
    origin**. **Drain** picks it up like any eligible AFK task, bounded by the
    per-set **Remediation depth** cap, after which the set parks at
    VERIFY-FAILED. Spawning triggers **Verification invalidation** of the set's
    cached verdicts. Findings live only as a Remediation task's body — never as
    annotations inside another task's spec.

- Disposition invariant
