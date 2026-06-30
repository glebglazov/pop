---
fragment: 705c0a07
generation: 0005
branch: master
---

+ Workbench
  A named blueprint for the shape of a whole tmux Session — its named windows
  and, within each window, a Layout (an explicit weighted split tree of Pane
  specs). Defined in global config, a repo's `.pop.toml`, or a global
  `[repo."<path>"]` block; resolved per checkout as a most-specific-wins union by
  name. Instantiated into a live Session by `pop workbench apply` (alias `wb`).
  The whole-session thing, one tier above a Layout. Formerly called "Session
  template".
  avoid: Session template, layout (that is the per-window tier), workspace, desk, session preset
  under: Language

- Session template

~ Layout
  The arrangement and sizing of panes within a single tmux window — pop's own
  weighted split tree (a window's `layout` field), the per-window tier. Keeps
  tmux's own word for the same scope; strictly per-window, never the whole
  session (that is a Workbench).
  was: (reserved-only) the per-window pane geometry, kept as tmux's word and listed under `avoid` for Session template; not a first-class glossary term.

~ Pane spec
  A leaf node in a Workbench window's Layout: a declaration of a pane to
  create — its optional name (→ pane title), command, cwd, and weight. Distinct
  from a Pane: a spec has no pane ID or attention status and carries a birth
  command/weight; a Pane is the live tracked result it produces when a Workbench
  is applied. Internal (non-leaf) tree nodes are unnamed splits
  (children = "rows"/"columns" over weighted children), not Pane specs.
  was: A leaf node in a Session template's window tree: a declaration of a pane to create — its optional name (→ pane title), command, cwd, and weight. Distinct from a Pane: a spec has no pane ID or attention status and carries a birth command/weight; a Pane is the live tracked result it produces when a template is applied. Internal (non-leaf) tree nodes are unnamed splits (children = "rows"/"columns" over weighted children), not Pane specs.
