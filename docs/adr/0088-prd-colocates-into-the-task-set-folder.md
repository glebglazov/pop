---
status: accepted
---

# PRD co-locates into the task-set folder

A PRD moves from the sibling `prds/<slug>.md` into its Task set's own directory as `tasks/<set>/prd.md`, so one folder holds a feature's whole context — the PRD, the task markdown, and `index.json` together. `to-prd` creates the set directory early (writing only `prd.md`); `to-tasks` fills in the task files and manifest; the set stays inert until `pop tasks register`. The `prds/` directory is retired and existing PRDs are migrated by slug (full removal tracked in CLEANUP.md, section D).

## Why

The Verifier (ADR-0086) reads the PRD as optional enrichment; a single self-contained folder makes that a trivial "read this directory" and means a PRD travels, archives, and deletes with its set instead of drifting in a parallel tree. This does **not** couple PRDs to execution: `prd.md` remains optional, PRD-less sets are normal, and acceptance criteria stay the authoritative contract — so the "PRD existence is irrelevant to scheduling and execution" stance is preserved, not reversed.

## Consequences

- A set folder may now contain a non-task `prd.md`; the Task set definition (manifest + task markdown) is unchanged, with `prd.md` noted as optional co-located context.
- `to-prd` and `to-tasks` path logic changes, and a `prds/<slug>.md` → `tasks/<set>/prd.md` migration ships with this feature; both the sibling read-path and the migration are fully removed later (CLEANUP.md).
