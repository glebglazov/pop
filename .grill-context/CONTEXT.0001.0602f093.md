---
fragment: 0602f093
generation: 0001
branch: master
---

+ Turn
  One model message in a **Captured run** — a single call to the model,
  regardless of how many tool invocations it carries. Counted per-adapter,
  because each agent's stream marks the boundary differently: claude's
  `assistant` events deduped by message id, cursor's distinct `model_call_id`,
  pi's `turn_end`. Never a raw stream event (pi emits ~26k cumulative deltas in
  one attempt) and never a tool count (one cursor run showed 67 turns against
  270 tool events).
  avoid: message, step, iteration, exchange
  under: Language

+ Turn-blind run
  A **Captured run** whose adapter declares no Turn rule, so its Turn count is
  absent rather than zero — the same honesty **Token-blind** already applies to
  usage. Rendered as a blind marker so an unsampled adapter can never rank as
  the cheapest thing in a **Task set**.
  avoid: zero-turn run, unknown turns
  under: Language

+ Peak input
  The largest context any single **Turn** of a **Captured run** fed the model:
  the maximum over model calls of input + cache-read + cache-write tokens. The
  sum, not the uncached input field alone, which is near-meaningless in a
  cache-heavy stream (pi reports `input: 6` against `cacheRead: 9115`).
  Available where an adapter reports usage per call (claude, pi); absent for
  adapters reporting only a run total (cursor), which are peak-blind.
  avoid: peak context, context high-water mark, max input
  under: Language

~ Agent adapter
  The preset-specific bridge between Pop and a supported agent, declaring every
  **Adapter capability** explicitly — there is no capability an adapter simply
  omits. Attended assistance launches the preset's own interactive binary and is
  owned by the adapter rather than the HITL gate prompt. An adapter reports
  assistance Unavailable only when it has no usable interactive command at all
  (e.g. custom headless `--agent-cmd`).
  was: The preset-specific bridge between Pop and a supported agent. An adapter
    may provide headless invocation, headless output handling, agent-assistance
    invocation, and a **Model source**; attended assistance launches the
    preset's own interactive binary and is owned by the adapter rather than the
    HITL gate prompt. An adapter reports assistance Unavailable only when it has
    no usable interactive command at all (e.g. custom headless `--agent-cmd`).

+ Adapter capability
  One stance an **Agent adapter** must declare about a supported agent, in two
  families: *stream-shape* capabilities read a **Captured run** (usage, cost,
  tool timings, actual model, stream rendering, **Turn**), and *invocation-shape*
  capabilities describe the CLI (reasoning arguments, quota reset reading,
  availability probe, effort ladder, executable name). Every capability is either
  supported or blind-with-a-reason; an undeclared capability is not a state pop
  can hold, since the preset table fails at construction. A stream-shape
  capability claiming support must also ship a trimmed real captured stream as a
  fixture, because only real data can show the claim is wrong.
  avoid: adapter feature, adapter support, extraction rule (for the capability itself)
  under: Language

~ Attempt timing breakdown
  The agent-specific accounting of where a Task attempt's wall-clock time went,
  derived from its Captured attempt stream: each attempt's outcome and total
  duration, its read-time-derived token spend (input/output/cache, absent for
  adapters that report none), and — for agents whose stream pairs a tool
  invocation with its result — a per-tool count and duration, followed by
  **Model time**. Tool figures are reported under the agent that ran the
  attempt because tool vocabularies differ by agent. Name-level by default; the
  `--tool-detail` flag on **Attempt stream replay** deepens it to argument
  granularity (repeated identical invocations, unbounded file reads, largest
  payloads, error loops, image reads) for a **Spend audit**, kept behind a flag
  so the breakdown printed during a live drain stays terse. It is the shared
  header rendered in two places: implement prints it as a task finishes, and
  Attempt stream replay prints it above each attempt's replayed events (ordered
  by attempt start time). The standalone `pop tasks timings` lens that once
  reprinted the per-task history is retired in favour of stream. Timing itself
  does not roll up across Task sets — attempt durations are only comparable
  within a task — but spend does, through the **Spend lens**.
  was: The agent-specific accounting of where a Task attempt's wall-clock time
    went, derived from its Captured attempt stream: each attempt's outcome and
    total duration, its read-time-derived token spend (input/output/cache,
    claude-first and absent for adapters that report none), and — for agents
    whose stream pairs a tool invocation with its result — a per-tool count and
    duration, followed by **Model time**. Tool figures are reported under the
    agent that ran the attempt because tool vocabularies differ by agent. It is
    the shared header rendered in two places: implement prints it as a task
    finishes, and **Attempt stream replay** prints it above each attempt's
    replayed events (ordered by attempt start time). The standalone `pop tasks
    timings` lens that once reprinted the per-task history is retired in favour
    of stream. Timing itself does not roll up across Task sets — attempt
    durations are only comparable within a task — but spend does, through the
    **Spend lens**.

+ Spend audit
  The procedure for finding where a drain's tokens went: rank a **Task set**'s
  runs by Turn and Peak input via the **Spend lens**, trace the dearest run's
  tool mix and repeated work, classify the waste, and route the fix into repo
  instructions or a prompt. Ships as a pop skill rather than a repo document
  because it is run against other repositories.
  avoid: drain waste audit, token audit, cost review
  under: Language
