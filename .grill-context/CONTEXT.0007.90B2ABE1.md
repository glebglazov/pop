---
fragment: 90B2ABE1
generation: 0007
branch: master
---

~ Effort model skip
  Advancement to the next entry of the current **Effort ladder** tier when the
  head model draws a `Model`-scoped **Agent proceed verdict** — kimi's
  subscription 401 today, a broker's spent per-vendor allowance next. Consumes
  no attempt: the attempt restarts on the next entry, on the same preset, and
  the Task's remaining tries are untouched. The skipped model is recorded
  machine-globally, with the adapter's parsed reset instant as its expiry, else
  one hour, else never for a `Permanent` recovery; resolution filters recorded
  models out of the tier, which is also the loop guard — every restart shortens
  the candidate list. The skipped invocation persists its own **Captured run**
  with outcome `model_skipped`, naming the refused model, so **Attempt stream
  replay** explains the gap; it is neither a failure nor an unusable agent and
  so stays out of the retry carry-forward digest a later attempt reads, and the
  drain prints one dim line naming the model skipped and the model taking over.
  Two read surfaces answer "why is it running the cheap model?" after the fact:
  the **Agent catalog** marks each skipped ladder entry with its remaining time
  (`∞` when permanent), and the **Work dashboard** carries a dim footer
  one-liner grouping every skip by preset — `skipped:
  cursor/claude-opus-5-thinking-high 47m · kimi/k2.7-code-highspeed ∞` — hidden
  when nothing is skipped and, like the two-line row rule, suppressed in a pane
  too short to spare the line. A tier with no candidate left escalates to
  `Preset` scope and **Agent fallback** advances the preset as it always did,
  persisting that run as `agent_unusable` — the outcome follows the scope the
  verdict finally had. A hand-pinned `--model` steps outside the ladder and so
  outside this. Shipped in ADR-0168; this is the tail **Effort ladder** has
  reserved since ADR-0032.
  avoid: model fallback, ladder fallthrough, plan gate
  under: Agents
  was: (identical, minus the two-read-surfaces sentence)

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
  configured provenance and, per entry, whichever **Effort model skip** is
  currently in force.
  was: (identical, but the closing sentence named only the built-in-versus-
  configured provenance)
