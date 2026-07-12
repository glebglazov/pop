---
status: superseded
superseded_by: ADR-0039
---

# Workload target references resolve from the current working directory

> ⛔ **SUPERSEDED BY [ADR-0039](0039-issue-sets-live-in-pop-data-dir-keyed-per-repository.md)** (target *shape* by [ADR-0015](0015-implement-is-one-verb-dispatching-by-target.md)). CWD-relative path-form targets are gone; targets are bare `<task-set>[/file.md]` identifiers resolved via repository identity, dispatched by shape. The path-only resolution rule no longer describes any behaviour.

_Original rationale retired to cut LLM-misleading noise; the decision and its context now live in ADR-0039 and ADR-0015. See [ADR-0069](0069-adr-status-is-normalized-frontmatter-and-dead-adrs-are-tombstoned.md) for the tombstone policy._
