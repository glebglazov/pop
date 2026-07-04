---
fragment: ac3dad12
generation: 0001
branch: master
---

~ Agent fallback
  The task executor's policy for choosing an **Agent preset**, owned by `pop tasks implement` rather than the **Queue**. Implement takes an ordered list of agents — one or more repeated `--agent` flags, else the `[tasks.implement].agents` config list, else the built-in `claude` — and runs each task on the first live agent, falling through to the next only on an **Agent quota pause**; a machine-global cooldown store records quota pauses per preset, and the Queue spawns plain `pop tasks implement` so manual and queue-triggered implements share the same fallback memory. The Verifier walks a parallel list, `[tasks.verify].agents`, with the same quota fall-through (plus a missing-binary skip). When attended **Integration conflict** assistance needs an agent, it uses only the first entry of the implement list — no quota-fallback walk, no separate queue-scoped config, and no `--agent` flag on `pop tasks integrate` for now. Standalone integrate resolves the list from config only; the post-drain epilogue inherits the list already resolved for that implement invocation (including explicit `--agent` flags on implement).
  avoid: Queue agent fallback, executor agent policy, default-agent, agent pin, agent rotation, [queue].agents, [workload] default_agents
  was: (named the `[workload] default_agents` config list as the implement fallback source and did not name the Verifier's list; the parent config table was `[workload]`. See ADR-0092 for the rename to `[tasks.implement].agents` / `[tasks.verify].agents`.)
