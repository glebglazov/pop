---
fragment: 296247e6
generation: 0005
branch: master
---

~ Agent fallback
  The task executor's policy for choosing an **Agent preset**, owned by `pop tasks implement` rather than the **Queue**. Implement takes an ordered list of agents — one or more repeated `--agent` flags, else the `[workload] default_agents` config list, else the built-in `claude` — and runs each task on the first live agent, falling through to the next only on an **Agent quota pause**; a machine-global cooldown store records quota pauses per preset, and the Queue spawns plain `pop tasks implement` so manual and queue-triggered implements share the same fallback memory. When attended **Integration conflict** assistance needs an agent, it uses only the first entry of that same list — no quota-fallback walk, no separate queue-scoped config, and no `--agent` flag on `pop tasks integrate` for now. Standalone integrate resolves the list from config only; the post-drain epilogue inherits the list already resolved for that implement invocation (including explicit `--agent` flags on implement).
  avoid: Queue agent fallback, executor agent policy, default-agent, agent pin, agent rotation, [queue].agents
  was: The task executor's policy for choosing an **Agent preset**, owned by `pop tasks implement` rather than the **Queue**. Implement takes an ordered list of agents — one or more repeated `--agent` flags, else the `[workload] default_agents` config list, else the built-in `claude` — and runs each task on the first live agent, falling through to the next only on an **Agent quota pause**; a machine-global cooldown store records quota pauses per preset, and the Queue spawns plain `pop tasks implement` so manual and queue-triggered implements share the same fallback memory.
