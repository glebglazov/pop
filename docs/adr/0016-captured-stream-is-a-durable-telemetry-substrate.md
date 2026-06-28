---
status: accepted
---

# The captured agent stream is a durable telemetry substrate

When an **Agent output adapter** runs in adapter mode, pop already tees the structured agent stream to an in-memory capture buffer that is the single source of truth for completion-sentinel assessment and **Agent quota detection** (ADR 0008). This decision makes that capture *durable*: for each structured **Task attempt**, pop also writes the raw stream — every event verbatim, each tagged with its arrival time relative to the attempt's start — to a per-attempt file under the Task set's `streams/` directory. The files are gzipped and accumulate over the task's lifetime; they are the **Captured attempt stream**, and every timing or usage figure pop reports is *derived* from them rather than computed inline.

This refines ADR 0008. The invariant there — the live-render parse never feeds assessment — still holds, but the honest framing is no longer "live rendering is a cosmetic side-channel." Live rendering is the primary thing a human watches, and plain-text processing may eventually be removed, making structured capture universal. What is actually load-bearing is that the **captured stream is authoritative** and everything else — the live render, the `+Xs` stream-entry timing, the per-attempt timing breakdown — is a derived view that must never become the source of truth.

## Why

The capture buffer was thrown away when the process exited, so the richest record of what an agent did — its full event stream, including tool invocations and their results — survived only as long as the run. "Where did the time go, and on which tools" is unanswerable after the fact, and impossible to improve against across attempts. Persisting the stream turns each attempt into a re-analyzable artifact.

We persist the *full raw* stream rather than a compact pre-digested event log. A lightweight log (arrival time + event type + tool name) would answer today's timing questions in a fraction of the space, but it forecloses tomorrow's: token and cost accounting, prompt mining, attempt-to-attempt diffing, failure replay. The stream is the substrate; named lenses read it. `pop tasks timings` is the first lens; the directory is `streams/`, not `timings/`, precisely so a second lens does not turn the name into a lie. The cost — disk growth, accumulating forever — is bounded in practice by gzip (~5× on real sessions) and by the modest absolute size of a task set's attempts; pop owns the read path (`compress/gzip`, stdlib) so a human never needs an external decompressor for the common breakdown view.

Timestamps are recorded on the **capture tee, not the live renderer**. Recording them where the authoritative bytes are kept — rather than where they are rendered — is what keeps the live render a derived view: a rendering bug, or an unrendered event type, cannot distort the persisted record or any figure derived from it. This is the same separation ADR 0008 drew between rendering and assessment, now extended to telemetry.

True per-tool durations require pairing each `tool_use` with its `tool_result` by id, which is per-adapter parsing work because the stream shape differs across agents. We pair for `claude` (the default preset) first; other structured adapters record each attempt's outcome and total duration immediately, with their per-tool breakdown added later from the *same* stored streams, no re-run. Plain-output and custom-command attempts have no structured events to pair and are not recorded at all.

## Considered Options

- **Persist a lightweight derived event log instead of the raw stream.** Rejected: smaller, but it bakes in today's metric and cannot answer questions not anticipated when it was written. The stored substrate should be at least as rich as the source.
- **Compute the timing breakdown inline and persist only the result.** Rejected: contradicts "store raw, derive views"; a new metric would require re-running the agent. Also re-entangles a stateful streaming computation with execution, the coupling ADR 0008 deliberately avoided.
- **Keep the capture ephemeral (status quo).** Rejected: the lost record is the problem being fixed.
- **Name the directory `timings/`.** Rejected: it labels the substrate with one derived view; the second lens (tokens, cost, replay) makes the name wrong.
- **Pair tool results for all adapters up front.** Rejected for v1 in favor of claude-first; the substrate is stored for every structured adapter, so back-filling a parser is a pure offline addition.
- **A cross-Task-set rollup.** Rejected: the unit of analysis is the task and its sequence of attempts; aggregating across a set conflates unrelated work. Breakdowns are per task, attempts ordered by start time.

## Consequences

For each structured attempt, pop writes `streams/<task-stem>/attempt-NNN.jsonl.gz` — a self-contained record (a header naming the agent and attempt, the timestamped raw events, a footer recording outcome and total duration). NNN is monotonic over the task's lifetime; display orders attempts by start time and shows no ordinal, so the persisted sequence never contradicts the executor's per-invocation "Attempt 1/3" line. Writing is best-effort: a storage failure is reported but never fails an `implement`, and the footer is finalized on the timeout, interrupt, and quota-pause paths so a SIGKILLed attempt still leaves a readable file.

`streams/` lives in **Task storage**, which is shared by all worktrees of a repository, while the **Runtime execution lock** is per runtime path — so two implements on different worktrees of one repo can target the same attempt number. The file is opened exclusively and the number bumped on collision; the lock is not widened to cover task storage.

A future contributor should not "save the second parse" by deriving telemetry from the live renderer, nor persist a digested log in place of the raw stream — both reintroduce exactly the coupling and the foreclosure this decision exists to prevent.
