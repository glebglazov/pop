---
fragment: 670C8D55
generation: 0003
branch: master
---

+ Agent proceed verdict
  An **Agent adapter**'s own report, on a result shape shared by every adapter,
  of whether it can carry on with the work it was given — and if not, at what
  scope (this model, or this whole preset), whether the attempt it was on
  should count against the **Task retry cap**, and what would heal it. The
  orchestrator never inspects provider text; each adapter owns recognising its
  own refusals and answers in this one vocabulary.
  avoid: proceed check, adapter health, can-run flag
  under: Agents

+ Effort model skip
  Advancement to the next entry of the current **Effort ladder** tier when the
  head model reports a model-scoped **Agent proceed verdict** — typically a
  per-vendor allowance spent while the same preset still has runnable
  cursor-native models. Consumes no attempt: the attempt restarts on the next
  model. The skipped model joins a skip list so resolution does not re-pick it.
  Exhausting the tier escalates to **Agent fallback**, which advances the
  preset. This is the tail that **Effort ladder** has reserved since ADR-0032.
  avoid: model fallback, ladder fallthrough
  under: Agents
