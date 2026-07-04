---
status: superseded
superseded_by: 0094
---

# Verifier runs persist Captured verify streams

ADR-0016 made structured **Task attempt** streams durable under `streams/<task-stem>/`; ADR-0086 added a **Verifier** that runs at set scope and caches only the parsed **Verify verdict** (PASS/FIXABLE/NEEDS-HUMAN + findings) in the drain store. That verdict is enough for status gating but discards the agent transcript — the part you need to analyze *why* verification passed, failed, or fell through agents. Verifier invocations now persist **Captured verify stream**s: the same gzipped JSONL envelope as implement, under `streams/_verify/<work-sha>/attempt-NNN.jsonl.gz`, with header fields `work_sha` and `phase: verify`. Every structured adapter-mode invocation in a verify run gets its own file (quota-paused fall-through included); the invocation whose output was parsed may annotate its footer `reason` with `verdict: …`. Persistence is best-effort — a write failure warns but never blocks verification. The drain-store verdict does not link to stream paths; replay is via **Attempt stream replay** (`pop tasks stream <set>/_verify`, optionally `/<work-sha>`).

## Considered Options

- **Verdict-only (status quo).** Rejected for analysis: findings in SQLite and assistant text on the terminal are not a durable, replayable substrate.
- **Flat `streams/_verify/attempt-NNN` without SHA folders.** Rejected: verification is SHA-gated; grouping by work SHA matches how verdicts are reasoned about and how re-runs at the same SHA accumulate.
- **Link stream paths from `VerifyVerdict`.** Rejected: blurs the State/Telemetry split (CONTEXT.md three-store model); filesystem discovery via `pop tasks stream` is enough for v1.
- **New top-level directory beside `streams/`.** Rejected: would fall out of **Task set export** unless extended; `_verify` under `streams/` keeps one telemetry tree.

## Consequences

- `pop tasks stream` gains `_verify` as a pseudo-stem; set-level replay lists every verification attempt across SHAs chronologically.
- Stream header schema gains optional `work_sha` and `phase` fields; existing implement streams remain valid (fields absent ⇒ implement).
- Relates to ADR-0016 (envelope and best-effort write), ADR-0086 (verifier scope and verdict cache), ADR-0090 (single replay lens).
