---
fragment: aad04869
generation: 0017
branch: fix-worktree-branch-picker-cursor
---

+ Attempt stream replay
  The `pop tasks stream TASK_SET[/FILE.md]` command — the read-only lens over
  the Captured attempt stream. It supersedes and retires the earlier `pop tasks
  timings` lens, of which it is a strict superset. For each structured attempt
  it renders the full Attempt timing breakdown as a header (including
  read-time-derived token spend), then the
  attempt's event sequence as human- and agent-legible text: per event a `+Xs`
  offset, assistant text, tool invocation name and arguments, and tool result,
  with oversized tool payloads truncated to a head+tail excerpt. It is pure
  read-side telemetry: it captures nothing new, deriving everything from stored
  streams, and never mutates. All of a task's attempts print chronologically;
  a bare set concatenates every task in manifest order. `--last` narrows to the
  most recent attempt, `--full` disables payload truncation, and `--raw` dumps
  the stored gzipped JSONL verbatim. Like `timings` it is a read-only snapshot
  verb that resolves archived sets by explicit identifier.
  avoid: log, transcript, agent output log, agent output dump
  under: Telemetry & timing

~ Attempt timing breakdown
  The agent-specific accounting of where a Task attempt's wall-clock time went,
  derived from its Captured attempt stream: each attempt's outcome and total
  duration, its read-time-derived token spend (input/output/cache, claude-first
  and absent for adapters that report none), and — for agents whose stream pairs
  a tool invocation with its result — a per-tool count and duration, followed by
  Model time. Tool figures are reported under the agent that ran the attempt
  because tool vocabularies differ by agent. It is the shared header rendered in
  two places: implement prints it as a task finishes, and Attempt stream replay
  prints it above each attempt's replayed events (ordered by attempt start
  time). The standalone `pop tasks timings` lens that once reprinted the
  per-task history is retired in favour of stream. There is no cross-Task-set
  rollup.
  was: The agent-specific accounting of where a Task attempt's wall-clock time went, derived from its Captured attempt stream: each attempt's outcome and total duration, and — for agents whose stream pairs a tool invocation with its result — a per-tool count and duration, followed by Model time. Tool figures are reported under the agent that ran the attempt because tool vocabularies differ by agent. Implement prints the breakdown for a task as the task finishes, showing the attempts made in that invocation; `pop tasks timings` reprints the full per-task history, ordered by attempt start time. There is no cross-Task-set rollup.
