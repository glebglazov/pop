---
fragment: fc757908
generation: 0014
branch: master
---

+ Repo-local override
  `.pop.local.toml` — a personal, gitignored in-tree file beside `.pop.toml`
  that overrides it for the same checkout. The two form a base+override pair:
  `.pop.toml` is the committed, team-shareable in-tree config; `.pop.local.toml`
  is your untracked personal layer, so personal beats committed at the same
  scope. pop reads it as just another hand-authored config locus and never
  inspects git-tracking — committing or ignoring either file is entirely the
  user's own choice, not a pop policy.
  avoid: local config, .pop.override.toml, personal pop.toml
  under: Configuration

+ In-tree config anchors
  How pop finds repo/worktree-scope in-tree config (`.pop.toml`,
  `.pop.local.toml`): git-agnostically, by filesystem path, at two anchors —
  this worktree (worktree scope) and the **Trunk worktree** (repo scope, via the
  same trunk resolver as **Preferred workbench** inheritance). Presence decides
  the layer: a file in this worktree wins; a file present only at the trunk fills
  in the repo layer and propagates dynamically to un-overridden children. The
  repo-scope anchor is the trunk worktree's tree, falling back to the
  **Repository identity** root so a bare repo's shared file (beside `.bare`)
  keeps working. Committed vs gitignored only affects how a file arrived, never
  how pop reads it.
  avoid: pop.toml inheritance, config walk, trunk snapshot

~ Repo override
  A `[repo."<path>"]` section in the global `config.toml`, keyed by any path that
  canonicalizes to a **Repository identity**, carrying per-repo behaviour. It is
  now the **lowest** repo-scope hand-authored locus — below in-tree
  `.pop.local.toml` and `.pop.toml` — an escape hatch for a repo you cannot or
  will not drop a file into. Machine-global settings (the project registry, queue
  agent rotation, daemon knobs) remain global-only and never move to any in-tree
  file, but `.pop.toml` is intended to grow toward a fuller per-project surface.
  was: A section in the global config.toml — [repo."<path>"], keyed by any path
  that canonicalizes to a Repository identity — carrying the per-repo behaviour
  subset (the .pop.toml RepoConfig schema) at higher priority than the repo's
  .pop.toml. Resolution is global override → .pop.toml → built-in default.
