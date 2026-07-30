---
fragment: d75cfa13
generation: 0057
branch: master
---

+ Run spend
  The token and dollar accounting of one **Captured run** — input, output, cache-read and cache-write tokens, plus cost where the adapter reports it. Tokens are the primary unit and are always present for a structured adapter with a **Usage extraction rule**; cost is captured only where an adapter ships it for free (currently pi's per-message `cost.total`) and is never inferred from a pricing table. Derived at read time from the stored events, never recorded as its own field.
  avoid: token usage, cost, billing
  under: Tasks

+ Usage extraction rule
  A per-adapter statement of **where a stream's authoritative usage lives and whether it accumulates or replaces** — the seam that turns raw events into **Run spend**. claude emits a per-API-call `usage` block on every assistant message (sum them); cursor emits one whole-run total on the terminal `result` event (read it, sum nothing); pi emits a cumulative block on every `message_update` delta and a settled one on `message_end` (sum `message_end`, ignore deltas). It is deliberately not a field-name translation table: names map cleanly while accumulation semantics do not, and getting the latter wrong produces a plausible wrong number rather than an error. An adapter without a rule yields **Token-blind run**s.
  avoid: usage parser, token field map, usage schema
  under: Tasks

+ Token-blind run
  A **Captured run** whose adapter reports no usage, so its **Run spend** is unknown rather than zero. Token-blind runs are counted and reported alongside every total they sit behind, so a figure is never quietly understated by runs it could not measure. A new adapter with no **Usage extraction rule** produces them; that visibility is the intended mitigation, not a defect.
  avoid: zero-usage run, unmeasured run, missing usage

+ Spend lens
  The `pop tasks spend [TASK_SET]` command — the read-only cross-set lens over **Run spend**, and the second lens over the same substrate as **Attempt stream replay**. Bare, it rolls up the ten most recent **Task set**s, one row each, sorted by total tokens; with a `TASK_SET` it breaks that set down per task, listing verification runs as their own rows. `--json` emits the same data machine-readably. The headline metric is **tokens per completed task**: every implement attempt charges to its task, including failed and retried ones, so retry waste stays visible instead of averaged away; verify runs have no task and charge to the set. It captures nothing and never mutates.
  avoid: tokens command, usage report, cost report, rollup

~ Attempt timing breakdown
  The agent-specific accounting of where a Task attempt's wall-clock time went, derived from its Captured attempt stream: each attempt's outcome and total duration, its read-time-derived token spend (input/output/cache, claude-first and absent for adapters that report none), and — for agents whose stream pairs a tool invocation with its result — a per-tool count and duration, followed by **Model time**. Tool figures are reported under the agent that ran the attempt because tool vocabularies differ by agent. It is the shared header rendered in two places: implement prints it as a task finishes, and **Attempt stream replay** prints it above each attempt's replayed events (ordered by attempt start time). The standalone `pop tasks timings` lens that once reprinted the per-task history is retired in favour of stream. Timing itself does not roll up across Task sets — attempt durations are only comparable within a task — but spend does, through the **Spend lens**.
  was: (identical, ending) "...retired in favour of stream. There is no cross-Task-set rollup."
  avoid: Workload report, run summary, set rollup
