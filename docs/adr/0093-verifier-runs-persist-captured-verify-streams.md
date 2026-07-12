---
status: superseded
superseded_by: ADR-0094
---

# Verifier runs persist Captured verify streams

> ⛔ **SUPERSEDED BY [ADR-0094](0094-captured-runs-are-uuid-pairs-with-chronological-replay.md).** Verify streams are no longer written under `streams/_verify/<work-sha>/attempt-NNN.jsonl.gz`; captured runs are UUID-keyed pairs (`streams/runs/<uuid>.meta.json` + `.events.jsonl.gz`) per ADR-0094. The `_verify` SHA-folder layout no longer describes any behaviour.

_Original rationale retired to cut LLM-misleading noise; the decision and its context now live in ADR-0094. See [ADR-0069](0069-adr-status-is-normalized-frontmatter-and-dead-adrs-are-tombstoned.md) for the tombstone policy._
