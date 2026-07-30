---
fragment: ef02bd0c
generation: 0055
branch: master
---

+ Handoff verb
  A **Work dashboard** action that hands the operator's attention to a tmux pane
  it spawns or focuses, rather than acting in place. Every handoff verb performs
  the same steps in the same order — spawn the pane, or focus the existing one
  when that activity is already running for the set rather than re-sending the
  command into it; focus (`SelectPane` + `SwitchClient`); quit the dashboard —
  so no verb invents its own post-spawn behaviour. It is bound to an
  **uppercase** key, which is the operator's only cue that the key navigates
  away; lowercase keys act in place and leave the dashboard open. A handoff may
  put one step in front of itself (a picker modal) and still be a handoff. When
  it moves the operator nowhere (no pane to focus, no checkout bound, ineligible
  row, focus unavailable outside tmux) it does not quit: it reports why in the
  dashboard's status line and stays put.
  avoid: dashboard action, launch verb, spawn action
  under: Queue

+ Live-pane affordance
  The colouring of a **Handoff verb**'s key — in the **Work dashboard** action
  menu and, as a compact per-activity cluster, on the row itself — showing what
  that activity's pane for this set is doing, and thereby what the key will do.
  Dark: no pane, the key spawns one. Grey: a **Pane tag**ged pane exists but sits
  at a bare shell, its command finished — the key respawns. Green: the command is
  running — the key jumps to it. Read from tmux once per dashboard poll, never
  from pop's own store, because a pane that dies leaves `list-panes` at once
  while a stored record outlives it. It replaces a separate preview verb
  outright: the verb that starts a thing is the verb that returns you to it.
  It covers only the activities pop supervises — drain, verify, fold, assist,
  and the wayfinder session (keyed by its window name, which is the map id,
  rather than by a tag). A **Runtime shell** is the operator's process, not
  pop's: it is never tagged, never tracked, always dark, and every press spawns
  a fresh one.
  avoid: pane indicator, preview, live badge
  under: Queue

+ Pinned action menu
  The **Work dashboard** action menu opened with `A` rather than `a`: it survives
  each **In-place verb** it fires and re-filters as `J`/`K` move the row cursor
  beneath it, so one verb can be swept down many rows. A **Handoff verb** fired
  from it still hands off and still quits — the pin is a convenience for verbs
  that stay, and exempting handoffs from their own contract inside a mode is the
  per-verb inconsistency this design exists to remove. `A`, like `/`, `G`, and
  `gg`, is a **mode** key: the verb case rule governs row verbs only, so a
  capital mode key that moves the operator nowhere is not an exception to it.
  avoid: sticky menu, persistent menu, multi-select

+ Pane tag
  A per-pane tmux option (`@pop_*`) pop writes onto a pane it spawns, naming the
  key that pane serves — a routine id or **Task set** id. It lives in tmux, not
  in pop's store, so it outlives the pop process that set it, and it is how pop
  later answers "which pane belongs to this?" without bookkeeping of its own.
  **One tag per activity, not one per set**: drain, verify, fold, and assist each
  get their own, so a lookup can never return another activity's pane and send
  keystrokes into a process already running there.
  avoid: pane marker, pane label, pane option

+ In-place verb
  A **Work dashboard** action that mutates state or opens a nested UI without
  moving the operator anywhere — bind, unbind, auto-drain toggle, status writes,
  archive, unpark, copy name. Bound to a **lowercase** key. The counterpart to a
  **Handoff verb**; the case of the key is the whole distinction.
  avoid: local action, quiet verb
  under: Queue
