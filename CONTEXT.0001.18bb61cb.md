---
fragment: 18bb61cb
generation: 0001
branch: master
---

+ Include
  A sidecar TOML file the global `config.toml` pulls in via `includes`, carrying only a whitelisted subset of config — registered **Project**s and **Repo override** blocks — so a user can keep which directories they work on out of the main file. Precedence is parent first, then includes in listed order; the first definition of a repo key sticks, and any other config section in an include is ignored. Distinct from `.pop.toml`, which rides in a repo and describes one already-registered project.
  avoid: import, partial, sidecar config, overlay
  under: Configuration
