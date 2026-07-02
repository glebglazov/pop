---
fragment: fc757908
generation: 0014
branch: master
---

~ Config merge order
  How pop resolves effective configuration, by an ownership/modality-first law:
  (1) hand-authored (user-written) config always beats runtime-generated config
  at any scope; (2) the user's central `config.toml` beats a repo's in-tree
  `.pop.toml`. Ladder, highest→lowest: config.toml `[repo."<path>"]` → config.toml
  global → this worktree's `.pop.toml` → the **Trunk worktree**'s `.pop.toml`
  (→ **Repository identity** root fallback) → runtime (`config.runtime.toml`:
  worktree, then trunk, then global integrations) → embedded default. Runtime is a
  gap-filler: to override, remove or edit the hand-authored value. Integrations
  (config.toml beats runtime skills) is preserved as the tier-1-over-tier-3 case.
  was: How pop resolves effective configuration: embedded pop defaults, then
  Integration runtime config (config.runtime.toml), then user config.toml — each
  layer overrides the previous for the fields it sets. The merge mechanism is
  global (all config keys); v1 only integrate writes the runtime file, for
  [integrations] only.

~ Repo override
  `.pop.toml` (flat, in the repo) and `[repo."<path>"]` (central, in global
  config.toml) decode ONE shared repo-scope key schema: authoring a repo-scoped
  setting in either place is equivalent, and adding a new repo key makes both
  accept it. Per the **Config merge order**, the user's central `config.toml`
  (including its `[repo]` blocks) outranks the committed `.pop.toml`. Repo scope is
  a curated set of genuinely repo-specific keys (`workbenches`,
  `preferred_workbench`), never a mirror of global config — `projects`, `queue`,
  and daemon knobs stay global-only. `trunk` is the one central-only exception
  (per-checkout machine topology, never in `.pop.toml`).
  was: A section in the global config.toml — [repo."<path>"], keyed by any path
  that canonicalizes to a Repository identity — carrying the per-repo behaviour
  subset (the .pop.toml RepoConfig schema) at higher priority than the repo's
  .pop.toml. Resolution is global override → .pop.toml → built-in default.

+ In-tree config anchors
  How pop finds repo-scope in-tree config (`.pop.toml`): at two anchors — this
  worktree and the **Trunk worktree** (falling back to the **Repository identity**
  root for a bare repo). Presence decides: a worktree with its own `.pop.toml`
  overrides the inherited trunk one; a worktree without inherits trunk's,
  dynamically. Reuses the trunk resolver of **Preferred workbench** inheritance.
  avoid: pop.toml inheritance, config walk, trunk snapshot
