---
fragment: bb6dac73
generation: 0024
branch: master (config-merge-engine grilling session)
---

~ Include
  A sidecar TOML file the global `config.toml` pulls in via `includes`, carrying only a whitelisted subset of config — registered **Project**s, task config (`[tasks]`, plus the deprecated `[workload]` alias), per-agent **Effort ladder**s, **Repo override** blocks, workbenches, and `[workbench]` options — so a user can keep which directories they work on out of the main file. Precedence is parent first, then includes in listed order; the first definition of any whitelisted key sticks, and any non-whitelisted section in an include is warned about and ignored. Distinct from `.pop.toml`, which rides in a repo and describes one already-registered project.
  was: A sidecar TOML file the global `config.toml` pulls in via `includes`, carrying only a whitelisted subset of config — registered **Project**s and **Repo override** blocks — so a user can keep which directories they work on out of the main file. Precedence is parent first, then includes in listed order; the first definition of a repo key sticks, and any other config section in an include is ignored. Distinct from `.pop.toml`, which rides in a repo and describes one already-registered project.
