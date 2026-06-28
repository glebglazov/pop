---
status: accepted
---

# ADR status is normalized frontmatter; dead ADRs are tombstoned

Every ADR carries a machine-readable `status:` in YAML frontmatter, one of `accepted | superseded | deferred`. A `superseded` ADR adds `superseded_by: ADR-NNNN`; an ADR that retires another adds `supersedes: [ADR-NNNN]` (forward direction only — the legacy `superseded:` field that was used to mean "this supersedes X" is renamed to `supersedes:` so the word never points the wrong way). Fully-superseded ADRs are **tombstoned**: their body is gutted to a one-line pointer, leaving only title, frontmatter, and a banner. Decisions that were designed but never built are marked `deferred` with a loud banner and keep their body.

## Why

The corpus grew to 68 ADRs with three different, inconsistent supersession conventions and ~28 ADRs carrying no status at all. The concrete failure mode is an agentic reader: grep or embedding-chunk retrieval surfaces a dead ADR's body (e.g. ADR-0018's per-task-agent design, reversed by ADR-0044), the reader treats it as current, and re-proposes a decision we already abandoned. A status banner at the top of the file doesn't fully protect against chunked retrieval that never sees the top — so the misleading *text itself* has to go, not just get a label.

Gutting an ADR's body breaks the usual "ADRs are immutable historical records" orthodoxy. We accept that trade deliberately: the "why we changed our minds" trail is already duplicated in the superseding ADR's Context, so the dead body is redundant noise, not unique history. This ADR exists so a future reader who finds a stub doesn't read it as data loss or vandalism — it is policy.

## Consequences

- A tombstoned ADR keeps its number and filename, so inbound `ADR-NNNN` links and the sequential id sequence stay intact; only the prose dies.
- `status` is the single source of truth and lives in frontmatter only; relationship prose ("amends ADR-0027…") moves to a `> **Relates:**` note under the title so the status value stays a clean enum.
- `deferred` is a first-class status distinct from `accepted`: an unbuilt design is now self-evidently aspirational rather than masquerading as shipped behavior.
- New supersession only ever uses `supersedes:` (on the newer ADR) and/or `superseded_by:` (on the older). The ambiguous `superseded:` field is retired.
