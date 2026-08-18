---
fragment: 3EB2F9F0
generation: 0023
branch: master
---

~ Turn
  One model message in a **Captured run** — a single call to the model,
  regardless of how many tool invocations it carries. Counted per-adapter,
  because each agent's stream marks the boundary differently: claude's
  `assistant` events deduped by message id, cursor's distinct `model_call_id`,
  pi's `turn_end`, codex's `token_count` events added by the **Rollout
  splice** — codex's native `turn.completed` is never a Turn, because it fires
  once per whole headless run (one real run counted 1 against 164 model
  calls). Never a raw stream event (pi emits ~26k cumulative deltas in one
  attempt) and never a tool count (one cursor run showed 67 turns against 270
  tool events).
  avoid: message, step, iteration, exchange
  was: One model message in a **Captured run** — a single call to the model, regardless of how many tool invocations it carries. Counted per-adapter, because each agent's stream marks the boundary differently: claude's `assistant` events deduped by message id, cursor's distinct `model_call_id`, pi's `turn_end`. Never a raw stream event (pi emits ~26k cumulative deltas in one attempt) and never a tool count (one cursor run showed 67 turns against 270 tool events).

~ Peak input
  The largest context any single **Turn** of a **Captured run** fed the model:
  the maximum over model calls of input + cache-read + cache-write tokens. The
  sum, not the uncached input field alone, which is near-meaningless in a
  cache-heavy stream (pi reports `input: 6` against `cacheRead: 9115`).
  Available where the stored stream carries usage per call — claude and pi
  natively, codex through the **Rollout splice**, whose `input_tokens` already
  includes cached tokens and must not have cache-read added again. Absent for
  adapters reporting only a run total (cursor), which are peak-blind.
  avoid: peak context, context high-water mark, max input
  was: The largest context any single **Turn** of a **Captured run** fed the model: the maximum over model calls of input + cache-read + cache-write tokens. The sum, not the uncached input field alone, which is near-meaningless in a cache-heavy stream (pi reports `input: 6` against `cacheRead: 9115`). Available where an adapter reports usage per call (claude, pi); absent for adapters reporting only a run total (cursor), which are peak-blind.

+ Rollout splice
  The capture-time enrichment that makes a codex **Captured run** self-contained:
  when a codex attempt ends, the run is joined to codex's session rollout file
  by the stream's `thread.started` thread id (which names the rollout file),
  and the rollout's per-call `token_count` events are spliced into the stored
  stream before any extraction rule reads it. It exists because codex's exec
  stream reports usage only as one whole-run rollup on a single
  `turn.completed`, hiding both the **Turn** count and **Peak input**. A run
  whose rollout cannot be found is stored unspliced and reads turn-blind and
  peak-blind — absent, never a known-wrong count.
  avoid: rollout join, session merge, sidecar read
  under: Lifecycle
