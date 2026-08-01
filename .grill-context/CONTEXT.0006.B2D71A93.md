---
fragment: B2D71A93
generation: 0006
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
  drain prints one dim line naming the model skipped and the model taking over. A tier with no
  candidate left escalates to `Preset` scope and **Agent fallback** advances the
  preset as it always did, persisting that run as `agent_unusable` — the outcome
  follows the scope the verdict finally had. A hand-pinned `--model` steps
  outside the ladder and so outside this. Shipped in ADR-0168; this is the tail
  **Effort ladder** has reserved since ADR-0032.
  avoid: model fallback, ladder fallthrough, plan gate
  under: Agents
  was: (identical, minus the persistence and fall-through-line sentences)

~ Captured run
  Durable telemetry for one structured agent invocation — an implement **Task
  attempt** or a **Verifier** run — stored among **Task artifacts** as a
  uuid-keyed pair under `streams/runs/`: `<uuid>.meta.json` (index fields:
  `run_id`, `phase`, `task_id`, `task_file`, `work_sha`, `start_time`,
  `end_time`, `outcome`, `verdict`, `agent`, `requested_agent`, `model`) and
  `<uuid>.events.jsonl.gz` (timestamped raw events). `model` is written only
  when the run's own events cannot name the model it ran on — an **Effort model
  skip** never reaches the model — and stands in for the read-derived one in
  every lens. Each structured adapter-mode invocation gets a new random uuid;
  plain-output and custom-command invocations are not recorded. Persistence is
  best-effort and never blocks implement or verify. The **Verify verdict** in
  the drain store does not point at run paths. A cache hit that reuses an
  existing verdict at the current work SHA runs no agent and writes no new run.
  avoid: Captured attempt stream (when you mean the new pair), verify log,
  agent output log
  was: (identical, minus `model` in the index-field list and its sentence)
