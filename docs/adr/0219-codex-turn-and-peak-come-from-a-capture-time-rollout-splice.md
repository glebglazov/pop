# codex Turn and Peak input come from a capture-time rollout splice

codex's `exec --json` stream cannot answer the two audit questions the Spend
lens leads with. Verified against the four codex **Captured run**s in the
tdg-cli store: the stream carries exactly one `turn.started`/`turn.completed`
pair per headless run and one whole-run usage rollup on that `turn.completed` —
no per-call usage anywhere. So the existing Turn rule (count `turn.completed`,
ADR-0165) always returns 1 — a run that did 180 tool items over 26.2M input
tokens counted the same as one that did 14 — and Peak input is unobtainable. A
constant 1 is worse than blind: it ranks the longest-grinding runs as the
shortest. Meanwhile codex writes a session rollout
(`~/.codex/sessions/YYYY/MM/DD/rollout-<ts>-<thread_id>.jsonl`) containing one
`token_count` event per model call with `last_token_usage` and
`model_context_window`, and the captured stream's `thread.started.thread_id` is
the rollout's filename — an exact, mechanical join. The joined 26.2M-token run
resolves to 164 model calls with a peak context of 243,453 tokens, ~94% of the
model's 258,400 window: precisely the context-pressure signal `peak-in` exists
to surface, invisible today.

Decision: **enrich at capture time.** When a codex attempt ends, the capture
seam locates the rollout by thread id and splices its `token_count` events into
the stored stream. The Captured run stays self-contained, and every extraction
rule remains a pure, fixture-backed function of the stored stream (ADR-0165's
invariant). codex's Turn rule becomes "count spliced `token_count` events" —
one per model call, matching the glossary's Turn and claude/pi semantics — and
a Peak input rule reads the maximum per-call context from `last_token_usage`.
One wire fact the peak rule must respect: codex's `input_tokens` already
includes cached tokens (unlike claude/pi, where cache-read is a separate
addend), so adding cache-read again would double-count.

When the rollout cannot be found — pruned sessions, another machine, a codex
layout change — the run is stored unspliced and reads **turn-blind and
peak-blind**: absent, never a fallback to the known-wrong count of 1. The Usage
extraction rule is unchanged; the `turn.completed` rollup remains authoritative
for **Run spend**, so this lifts only the turn and peak blindnesses.

## Considered Options

- **Read-time lookup**: extraction rules go find `~/.codex/sessions` when
  asked. Rejected — the rollout can be pruned, the store can move machines, and
  every read surface would gain a filesystem dependency on a directory pop does
  not own.
- **Stay blind, fix only the degenerate count**: re-declare codex turn-blind
  and leave peak alone. Rejected — the data exists and the join is exact;
  honesty about absence is the fallback, not the design.

## Consequences

Stored codex streams gain events their CLI never printed on stdout, and pop
takes a dependency on codex's undocumented session-rollout layout — if codex
moves or reshapes it, new runs degrade to blind (visibly, via the blind-run
counters) rather than erroring. Existing unspliced runs stay blind; the
degenerate always-1 turn counts already stored should be treated as wrong, not
historical.
