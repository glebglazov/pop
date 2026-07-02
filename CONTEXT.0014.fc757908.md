---
fragment: fc757908
generation: 0014
branch: master
---

~ Repo override
  `.pop.toml` (flat, in the repo) and `[repo."<path>"]` (central, in global
  config.toml) decode ONE shared repo-scope key schema: authoring a repo-scoped
  setting in either place is equivalent, and adding a new repo key makes both
  accept it. `.pop.toml` is the committed/in-repo form; `[repo."<path>"]` is the
  user's personal central form and **wins** over a committed `.pop.toml` for the
  same key (personal beats committed at repo scope). Repo scope is a curated set
  of genuinely repo-specific keys (`workbenches`, `preferred_workbench`), never a
  mirror of global config — `projects`, `queue`, and daemon knobs stay
  global-only. `trunk` is the one central-only exception (per-checkout machine
  topology, never in `.pop.toml`).
  was: A section in the global config.toml — [repo."<path>"], keyed by any path
  that canonicalizes to a Repository identity — carrying the per-repo behaviour
  subset (the .pop.toml RepoConfig schema) at higher priority than the repo's
  .pop.toml. Resolution is global override → .pop.toml → built-in default.

+ In-tree config anchors
  How pop finds repo-scope in-tree config (`.pop.toml`): at two anchors — this
  worktree (worktree scope) and the **Trunk worktree** (repo scope, falling back
  to the **Repository identity** root for a bare repo). Presence decides: a
  worktree with its own `.pop.toml` overrides the inherited trunk one; a worktree
  without inherits trunk's, dynamically. Reuses the trunk resolver of **Preferred
  workbench** inheritance.
  avoid: pop.toml inheritance, config walk, trunk snapshot
