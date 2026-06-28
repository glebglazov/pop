---
status: accepted
supersedes:
  - ADR-0018 (tasks may declare a per-task agent)
---

# Tasks declare effort, not agent

A task manifest's only execution knob is `effort:`; the per-task `agent:` key is removed. Agents are interchangeable and chosen by the **Agent fallback** list, while effort selects model strength within whichever agent runs.

## Context

ADR-0018 let a manifest pin a per-task `agent:` key, which created a precedence tree (explicit `--agent` > task `agent:` > `--default-agent` > default) and the opposite-precedence flag pair `--agent` (overrides pins) vs `--default-agent` (yields to pins). Relocating agent fallback into implement (see the companion ADR) made that tree the central source of friction. Effort (ADR-0032) already resolves a tier to a model *per agent*, so it is agent-independent by construction.

## Decision

Drop the manifest `agent:` key and `--default-agent`. Agents come solely from the **Agent fallback** list (`--agent` / `[workload] default_agents`); a task's `effort:` re-resolves to the right model for whichever agent the list lands on. Per-entry `--agent "claude --model ..."` augmentation (ADR-0017) still overrides effort for that entry.

## Consequences

- Manifests are JSON-decoded without `DisallowUnknownFields`, so a lingering `agent:` key is silently ignored — no parse break, no migration pass. `validateManifestAgentSpec` is deleted.
- The capability lost is "this one task must run on this specific agent." Accepted: it is consistent with treating agents as interchangeable (quota-only fallback), and effort remains the per-task lever.
