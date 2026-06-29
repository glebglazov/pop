---
fragment: 43eb3988
generation: 0003
branch: master
---

+ Session template
  A named blueprint for the shape of a tmux Session — its named windows and,
  within each window, an explicit weighted split tree of Pane specs plus the
  window's pane arrangement. Defined in global config (`[[session_templates]]`),
  in a repo's `.pop.toml`, or in a global `[repo."<path>"]` block; resolved per
  checkout as a most-specific-wins union by name. Because `.pop.toml` resolves
  from Repository identity, a bare repo's template propagates to all its
  worktrees for free. Instantiated into a live Session on demand by
  `pop template apply`. The per-window pane geometry keeps tmux's own word,
  layout (main-vertical, tiled); a Session template is the whole-session thing
  one level up.
  avoid: Layout (that is the per-window pane geometry), workspace, session preset
  under: Language

+ Pane spec
  A leaf node in a Session template's window tree: a declaration of a pane to
  create — its optional name (→ pane title), command, cwd, and weight. Distinct
  from a Pane: a spec has no pane ID or attention status and carries a
  birth command/weight; a Pane is the live tracked result it produces when a
  template is applied. Internal (non-leaf) tree nodes are unnamed splits
  (children = "rows"/"columns" over weighted children), not Pane specs.
  avoid: Pane (the live tracked pane), pane template, pane definition
  under: Language
