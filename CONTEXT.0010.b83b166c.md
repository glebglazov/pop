---
fragment: b83b166c
generation: 0010
branch: master
---

+ Preferred workbench
  A personal, per-checkout choice of which **Workbench** auto-applies when a
  session is born for that checkout, skipping the create-time prompt. Stored
  per-worktree in **Integration runtime config** (`[workbench.preferred]`,
  path-keyed; set via the picker's `ctrl+w` or `pop workbench prefer`), with a
  coarser per-repo `preferred_workbench` default on a global `[repo."<path>"]`
  block. Never in `.pop.toml` — it is personal taste, not committed team config.
  Resolves finest-first: this worktree's entry → the **Trunk worktree**'s entry
  (inheritance, dynamic at open) → the repo default → none (then
  `pick_on_create` decides prompt vs flat). Three-valued per worktree: unset
  (inherit), a name, or explicit none (flat/prompt here, overriding any
  inherited default). A resolved value that auto-applies suppresses the
  create-time prompt regardless of `pick_on_create`; a stored name that no
  longer resolves is skipped with a warning and resolution continues.
  avoid: preferred layout, default workbench, preferred worktree, default worktree
  under: Workbench

~ Config merge order
  How pop resolves effective configuration across embedded defaults, **Integration
  runtime config** (`config.runtime.toml`), and user `config.toml`. The governing
  rule is **most-specific scope wins**; only when the same scope is set in more
  than one file does hand-authored `config.toml` beat CLI-written runtime config.
  Scopes, finest→coarsest: per-worktree > per-repo (`[repo."<path>"]`) > global.
  So `[integrations] skills` (a global-scope key set in both files) resolves to
  `config.toml` (the equal-scope tie-break, ADR-0065), while a **Preferred
  workbench** per-worktree runtime entry beats a per-repo default (a finer scope,
  not a file-precedence flip). `config.runtime.toml` is a storage location, not a
  precedence tier — "runtime always wins" and "runtime always loses" are both the
  wrong mental model.
  was: How pop resolves effective configuration: embedded pop defaults, then Integration runtime config (config.runtime.toml), then user config.toml — each layer overrides the previous for the fields it sets. The merge mechanism is global (all config keys); v1 only integrate writes the runtime file, for [integrations] only. Integrate, refresh, and Doctor consume the merged config.
