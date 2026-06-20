---
fragment: b8f11714
generation: 0003
branch: master
---

+ Agent fallback
  The task executor's policy for choosing an **Agent preset**, owned by `pop tasks implement` rather than the **Queue**. Implement takes an ordered list of agents — one or more repeated `--agent` flags, else the `[workload] default_agents` config list, else the built-in `claude` — and runs each task on the first live agent, falling through to the next **only on an Agent quota pause** (a plain failure retries on the same agent). A machine-global, file-locked cooldown store, owned by the tasks module and keyed by preset because quota is per subscription, records each agent exhausted-until; it is **reset-aware** — the recovery instant comes from the pause's own reported reset time when present, else a fixed interval. Implement reads the store before spawning and **skips a cooling agent rather than burning an attempt**; when every agent in the list is cooling it returns a quota pause carrying the **earliest** reset across the list. The Queue no longer selects an agent: it spawns plain `pop tasks implement` and only reads the store to render cooldown status, so manual and queue-triggered implements behave identically and share the same cross-run memory.
  avoid: Queue agent fallback, executor agent policy, default-agent, agent pin, agent rotation
  under: Tasks

- Queue agent fallback

- Task agent

~ Curated model aliases
  A short, hand-maintained list of model aliases Pop ships for each recognized **Agent preset**, surfaced as a column in `pop tasks agents`, recommended value first. It is a suggestion surface to help a planner fill an **Agent preset**'s `--model` via `--agent` augmentation, or to read an **Effort** tier — never exhaustive, never a validation gate. Distinct from the **Effort ladder**, which is the resolution surface: `claude`'s curated aliases are structured into tiers to feed its built-in Effort ladder, while the curated list itself stays advisory. Only `claude`'s entries are stable auto-resolving aliases; other presets list pinned version ids that need maintenance.
  was: ... a suggestion surface to help a planner fill a **Task agent**'s `--model` — never exhaustive, never a validation gate ...
