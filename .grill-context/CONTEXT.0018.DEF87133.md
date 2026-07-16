---
fragment: DEF87133
generation: 0018
branch: master
---

+ Config show
  `pop config show`: prints the effective configuration as pop resolves it from
  the current directory — includes merged, repo keys canonicalized to absolute
  realpaths, folder-local overrides (`.pop.toml` + the current `[repo]` block)
  collapsed into effective values, and the current repo's resolved [[Trunk
  worktree]] (config-declared *or* git-derived) surfaced as an effective
  `trunk`/`bare`. Run outside any repo, the current-repo/trunk section is
  absent. Effective values only, no provenance annotation. TOML by default,
  `--json` for machines. Reaches config + git (for the derived trunk), never
  the task-binding store. The value counterpart to `pop config keys` (the
  accepted schema); renders the result of [[Config resolution]].
  avoid: config dump, config export
  under: Configuration
