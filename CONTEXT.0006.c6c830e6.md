---
fragment: c6c830e6
generation: 0006
branch: master
---

+ Frame
  The shared screen-chrome module the budgeted list views stand on: from one
  declaration of which regions are present (update notice, header, input box,
  warnings, hints) it both computes the body height the caller may fill and
  renders the header/footer around a caller-supplied body string. The single
  region declaration feeds budget and render together, so the reserved-line count
  can no longer drift from the view the way the hand-counted `Height-N` magic
  numbers did. Warnings are reserved like any other region; the body is floored so
  it never collapses. Pairs with List: List owns the body (rows, cursor, anchor),
  Frame owns everything around it.
  avoid: chrome, header/footer helper, Layout (that is the per-window tmux tier, a
  different sense)
  under: Pickers

- Note

~ Topic
  A normalized lowercase kebab slug (≤5 words) naming the subject of an Agentic
  pane, single-sourced as a per-pane tmux property any tmux surface can display.
  pop fills it in stages — an instant seed from truncating the prompt, then
  optionally a higher-quality agent-derived value, and optionally re-derived as
  the conversation drifts. It is now the sole display subject of a pane: the
  user-authored Note that used to outrank it has been removed.
  was: A normalized lowercase kebab slug (≤5 words) naming the subject of an
  Agentic pane, single-sourced as a per-pane tmux property any tmux surface can
  display. pop fills it in stages — an instant seed from truncating the prompt,
  then optionally a higher-quality agent-derived value, and optionally re-derived
  as the conversation drifts. A Note still outranks it in display.
  under: Pane status
