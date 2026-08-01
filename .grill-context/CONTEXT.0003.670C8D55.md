---
fragment: 670C8D55
generation: 0003
branch: master
---

+ Agent proceed verdict
  An **Agent adapter**'s own report, on a result shape shared by every adapter,
  of whether it can carry on with the work it was given. Carries a scope
  (`Model` — this `--model` token is unrunnable, the CLI is healthy; `Preset` —
  this CLI can run nothing), a recovery (`Time`, `Human`, `Permanent`), an
  optional reset instant, and whether the attempt counts against the **Task
  retry cap**. The orchestrator never inspects provider text: each adapter owns
  recognising its own refusals and answers in this one vocabulary. **Agent
  unavailability** is its `Preset`-scoped case. Designed in ADR-0168, deferred.
  avoid: proceed check, adapter health, can-run flag
  under: Agents

+ Effort model skip
  Advancement to the next entry of the current **Effort ladder** tier when the
  head model draws a `Model`-scoped **Agent proceed verdict** — typically a
  broker's per-vendor allowance spent while the same preset still has runnable
  native models. Consumes no attempt: the attempt restarts on the next entry.
  The skipped model is recorded with an expiry (the adapter's parsed reset
  instant, else one hour) in a machine-global list that resolution filters
  against, which is also the loop guard — every restart shortens the candidate
  list. An exhausted tier escalates to **Agent fallback**, which advances the
  preset. This is the tail **Effort ladder** has reserved since ADR-0032.
  Designed in ADR-0168, deferred.
  avoid: model fallback, ladder fallthrough
  under: Agents

~ Plan gate
  An agent's report that the resolved model is permanently unrunnable on this
  account — concretely kimi's HTTP 401 `does not have access to …`
  (`provider.auth_error`) when an **Effort ladder** entry names a
  subscription-gated model such as `kimi-k2.7-code-highspeed`. Today it
  triggers **Agent fallback** fall-through like an **Agent quota pause** but
  records no cooldown, since the gate is deterministic for that account+model.
  It is a statement about a *model* answered by advancing the *preset*:
  ADR-0168 folds it into an **Effort model skip** with `Recovery=Permanent`,
  after which this term retires.
  was: An agent's report that the resolved model is permanently unrunnable on
  this account — concretely kimi's HTTP 401 `does not have access to …`
  (`provider.auth_error`) when an **Effort ladder** entry names a
  subscription-gated model such as `kimi-k2.7-code-highspeed`. A plan gate
  triggers **Agent fallback** fall-through like an **Agent quota pause** but
  records no cooldown: the gate is deterministic for that account+model, so
  re-probing is cheap and the pause machinery's reset-time semantics do not
  apply. It is not a task failure — the next preset simply takes the task.
