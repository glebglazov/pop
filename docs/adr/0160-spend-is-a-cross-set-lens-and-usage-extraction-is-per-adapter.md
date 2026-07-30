---
status: accepted
---

# `pop tasks spend` is a cross-set lens and usage extraction is per-adapter

**Run spend** — the token and cost accounting of a Captured run — gets its own lens, `pop tasks spend`, which aggregates *across* Task sets. Extracting that spend from a stream is **per-adapter**, and the thing that differs between adapters is not the field names but **where the authoritative usage lives and whether it accumulates or replaces**.

This refines two prior decisions rather than reversing them. [ADR-0016](0016-captured-stream-is-a-durable-telemetry-substrate.md) rejected a cross-set rollup on the grounds that "the unit of analysis is the task and its sequence of attempts; aggregating across a set conflates unrelated work." That reasoning holds for *timing* — attempt durations are only comparable within a task — but not for *spend*, which is denominated in the same unit everywhere and is meaningless until compared across sets. [ADR-0090](0090-stream-supersedes-timings-as-the-sole-stream-lens.md) promised that a summary view would return "as a `--summary` flag on `stream`, not as a revived command." A rollup takes no `TASK_SET` argument, so it is a different noun; `spend` is that command, and `stream` stays a per-set replay.

## Why per-adapter extraction, and why it is not a field-name map

The obvious implementation — map each adapter's usage keys onto the canonical `TokenUsage` — is wrong, and wrong in a way that produces confidently incorrect numbers rather than errors. The three structured adapters place authoritative usage in three different places:

- **claude** emits a `usage` block on every assistant message. Each block covers that one API call. Summing every block is correct.
- **cursor** emits usage exactly once, on the terminal `result` event, as a whole-run total. Summing anything is wrong; you read the one event.
- **pi** emits a *cumulative* usage block on every `message_update` delta, and a settled one on `message_end`. Summing the deltas over-counts by roughly the number of deltas per message — measured at ~4× on real runs, from 26,419 delta events in a single attempt.

A key map would have translated pi's `input`/`cacheRead` correctly and still reported ~4× its real spend. So the adapter seam is a rule — *which events carry authoritative usage, and do they accumulate or replace* — not a translation table.

This is why an **over-count guard** is load-bearing rather than defensive polish: a run's summed usage may not exceed the total its own terminal event reports, and violating that fails loudly instead of reporting. The failure mode being guarded is not a crash; it is a plausible number. A 4× over-count on one agent is enough to argue for dropping that agent from the effort ladders, which is exactly the wrong conclusion drawn from exactly the right-looking table.

**Tokens are the primary unit; dollars are captured only where they are free.** pi already reports `cost.total` per message in dollars, so discarding it would be throwing away data we hold. cursor and claude report none, so cost is partial by construction and is rendered only where present, never inferred. A pricing table for the other adapters is deliberately deferred — it is a maintenance burden that goes stale silently, and the tokens answer the question on its own.

**Verify runs charge to the set, not to a task.** A verify run has no `task_id`; it validates a whole set. Attributing it to individual tasks would be a fiction, and it is not a small one — verification is the most expensive single run type per-run in the current data. So the headline metric, **tokens per completed task**, charges every implement attempt (including failed and retried ones — that is what makes retry waste visible rather than averaged away) to its task, and reports set-scoped verification beside it. The per-set breakdown lists verification runs as their own rows.

**Token-blind runs are counted and named.** A run whose adapter reports no usage sums to zero, which silently understates every total that contains it. Every figure `spend` prints carries the count of token-blind runs behind it, so a total is never quietly wrong.

## Considered Options

- **A `--summary` flag on `stream`, per ADR-0090's promise.** Rejected: `stream` is argument-bound to one set, and the question that motivated this work ("where do the tokens actually go across recent work") cannot be asked of a single set.
- **A field-name translation table across adapters.** Rejected on evidence: it reproduces pi's ~4× over-count while looking correct.
- **Reading the pre-ADR-0094 `streams/<task>/attempt-NNN.jsonl.gz` layout as well.** Rejected: the legacy window is a small and now-unrepresentative slice of history, and supporting two layouts doubles the surface where a lens can silently drop half its input.
- **A pricing table so every adapter reports dollars.** Deferred, not rejected: prices drift and a stale table lies more confidently than an absent column.
- **Excluding token-blind runs from the denominator.** Rejected: it makes them invisible, which is the failure this decision exists to prevent. Count them, name them.

## Consequences

`pop tasks spend` with no argument rolls up the **most recent ten** Task sets, one row per set. With a `TASK_SET` argument it breaks that set down per task, with verification runs listed as their own rows. `--json` emits the same data machine-readably.

The ten-set default is a display bound, not a data bound — it keeps the bare command readable rather than asserting anything about how far back the substrate goes.

Adding a new agent adapter now carries an obligation that is easy to miss: declaring its usage-extraction rule. An adapter without one produces token-blind runs, which is a visible, named state rather than a silent zero — that visibility is the mitigation, and it is deliberate.

`spend` is an instrument, not a remedy. The tool-call mix it measures (currently ~61% read/grep/glob across recent implement runs, correlating with token spend at r≈0.77) suggests work on reducing agent search. That work is a separate thread with its own evidence; `spend` exists to make the number visible first.
