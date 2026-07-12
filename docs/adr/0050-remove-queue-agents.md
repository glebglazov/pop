---
status: accepted
supersedes:
  - docs/adr/0043-agent-fallback-owned-by-implement.md (in part — `[queue].agents` is deleted, not merely unused)
---

# Remove `[queue].agents`; integration reads `[workload] default_agents`

> **Relates:** two names below have since drifted — the surviving key `[workload] default_agents` is itself now deprecated in favour of `[tasks.implement].agents` (ADR-0092), and the **Integration conflict** consumer this ADR points at was removed entirely with worktree-set integration (ADR-0070). The core decision — `[queue].agents` is deleted; **Agent fallback** owns agent selection — is unchanged.

`[queue].agents` is deleted. Agent selection is owned entirely by **Agent fallback** under `[workload] default_agents` — since renamed to `[tasks.implement].agents` (ADR-0092) — plus implement's `--agent` flags. The only surviving consumer of the old queue field was attended **Integration conflict** assistance, which took the first entry of that same resolved list — not a queue-scoped pool; that integration path was later removed wholesale (ADR-0070).

Standalone `pop tasks integrate` resolves the list from config only (`default_agents[0]` → `claude`). The post-drain epilogue inherits the list already resolved for that implement invocation, so explicit `--agent` on implement still flows into conflict help. Integration does not walk the quota-fallback list: one agent, attended, human in the loop. A dedicated `--agent` flag on integrate is descoped for now.

Configs that still set `[queue].agents` load with a warning pointing at `[workload] default_agents`; no auto-migration.

## Consequences

- `config.example.toml` must stop documenting `[queue].agents` as a queue drain fallback pool.
- `integrationAgentPreset` should call the same `resolveDefaultAgentPresets` helper implement uses, then take index `[0]` only.
