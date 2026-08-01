---
fragment: 8F4E7104
generation: 0005
branch: master
---

- Plan gate

~ Effort model skip
  Advancement to the next entry of the current **Effort ladder** tier when the
  head model draws a `Model`-scoped **Agent proceed verdict** — kimi's
  subscription 401 today, a broker's spent per-vendor allowance next. Consumes
  no attempt: the attempt restarts on the next entry, on the same preset, and
  the Task's remaining tries are untouched. The skipped model is recorded
  machine-globally, with the adapter's parsed reset instant as its expiry, else
  one hour, else never for a `Permanent` recovery; resolution filters recorded
  models out of the tier, which is also the loop guard — every restart shortens
  the candidate list. A tier with no candidate left escalates to `Preset` scope
  and **Agent fallback** advances the preset as it always did. A hand-pinned
  `--model` steps outside the ladder and so outside this. Shipped in ADR-0168;
  this is the tail **Effort ladder** has reserved since ADR-0032.
  avoid: model fallback, ladder fallthrough, plan gate
  under: Agents

~ Agent proceed verdict
  The one answer every **Agent adapter** gives to "can you carry on?", carried
  on a result shape shared by all adapters so the orchestrator never reads
  provider text. Absent means yes. Present, it means the agent cannot do the
  work it was given — as distinct from an attempt that ran and failed — and says
  at what **Agent proceed scope**, with what **Agent proceed recovery**, a reset
  instant when one is known, and whether the attempt is charged to the **Task
  retry cap**. **Agent quota pause** is one flavour; an **Agent authentication
  failure** is another, as are a binary missing from PATH and a model the
  account cannot run. Detected on two channels — a passive read of the capture
  pop already consumes (like **Agent quota detection**), and an active **Agent
  availability probe**. The passive channel catches a session that lapsed
  mid-drain; the probe catches one already lapsed on arrival.
  was: (identical, but the flavour list ended "… and a plan gate")

~ Agent proceed scope
  How much of an agent an **Agent proceed verdict** condemns. *Preset* condemns
  the whole entry in the **Agent fallback** list — one adapter, one CLI, one
  login: it abandons the remaining **Task retry cap** for that preset, hands the
  turn to the next preset, and is the only scope that reaches the preset
  cooldown store and **Agent quota recovery wait**. *Model* condemns only the
  token the **Effort ladder** tier resolved, leaving the CLI healthy: it drives
  an **Effort model skip**, and escalates to *preset* once the tier has no entry
  left. Dispatch reads the scope rather than the flavour, so a new flavour lands
  without editing the orchestrator.
  was: (identical, minus the *Model* elaboration, and ending "… without editing
  the orchestrator; every shipped detector answers *preset*")

~ Effort ladder
  A per-agent, per-tier ordered list of **(model, Reasoning effort)** bundles
  that resolves an **Effort** to a concrete `--model` plus a reasoning channel
  for whichever agent was chosen. Pop ships built-in ladders for `claude`,
  `codex`, `cursor`, `pi`, and `kimi`; every other agent (e.g. `opencode`) has
  none built-in and is configured in `config.toml` under `[effort.<agent>]`,
  which fully replaces the built-in for an agent it names. Each tier is a TOML
  array of `{ model, reasoning }` tables, reasoning optional. Resolution takes
  the first tier entry not recorded as an **Effort model skip** and each entry
  carries its own reasoning, so the tail runs when the head is refused rather
  than sitting inert. Reasoning is rendered per-adapter — `claude --effort`,
  `codex -c model_reasoning_effort=`, `pi --thinking`, `kimi` via a
  `KIMI_MODEL_THINKING_EFFORT` environment variable on the invocation — except
  for `cursor`, which selects a full concrete model name per tier and does not
  emit a separate reasoning parameter. Agents with no reasoning mechanism
  (`opencode`) or no ladder make that part a graceful no-op. kimi's built-in
  ladder is heavy `moonshot-ai/kimi-k3`@high, standard `moonshot-ai/kimi-k3`@low,
  light `moonshot-ai/kimi-k2.7-code-highspeed` model-only — k3 accepts only
  `low`/`high`/`max` on the wire (a `medium` env value is a server 400), so
  `@low` is the lightest native k3 rung; the light tier is a single entry, so
  its subscription gate exhausts the tier and **Agent fallback** advances the
  preset. Surfaced per agent in `pop tasks agents` with built-in-versus-
  configured provenance.
  was: (identical, but resolution "uses the head of the chosen tier; the ordered
  tail is reserved for a deferred runtime fallback", and the kimi note ended
  "the light tier's plan gating surfaces as **Agent fallback** fall-through via
  **Plan gate**")
