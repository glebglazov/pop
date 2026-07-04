---
status: accepted
---

# `pop tasks stream` supersedes `timings` as the sole lens over the captured stream

[ADR-0016](0016-captured-stream-is-a-durable-telemetry-substrate.md) framed `pop tasks timings` as "the first lens" over the Captured attempt stream and named the directory `streams/` so a *second* lens would not make the name a lie. That second lens — **Attempt stream replay** (`pop tasks stream`) — renders each attempt's full event sequence beneath the very same Attempt timing breakdown that `timings` printed. Since `stream` is a strict superset of `timings`, we retire the standalone `timings` command rather than carry two lenses over one substrate.

The Attempt timing breakdown itself is unchanged and still shared: `implement` prints it inline as a task finishes, and `stream` prints it as each attempt's header. Only the standalone `timings` command surface is removed; the substrate, the breakdown, and the "store raw, derive views" invariant of ADR-0016 all stand — this refines its "first lens" framing, it does not reverse it.

## Consequences

- The one thing `timings` offered that `stream` does not is a compact, numbers-only view — `stream` always includes the replayed event body. That is an acceptable loss for now; if a summary-only view is ever wanted it returns as a `--summary` flag on `stream`, not as a revived command.
