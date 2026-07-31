---
fragment: a06102a9
generation: 0002
branch: master
---

+ Project scan fan-out
  Resolving every configured **Project**'s scan — project root, definition path,
  session name, **Repository identity** — with one git invocation per project, to
  find the one that owns a set. It is the operation **SetRef** exists to make
  unnecessary, and it is only legitimate where several candidate checkouts must
  genuinely be compared (representative/trunk selection for an unbound set), where
  it runs concurrently. A verb addressed by a SetRef never performs it.
  avoid: project scan, scan loop, DetectProject fan-out
  under: Queue

~ Handoff verb
  A **Work dashboard** action that hands the operator's attention to a tmux pane it
  spawns or focuses, rather than acting in place. Every handoff verb performs the
  same steps in the same order — spawn the pane, or focus the existing one when that
  activity is already running for the set rather than re-sending the command into
  it; focus (`SelectPane` + `SwitchClient`); quit the dashboard — so no verb invents
  its own post-spawn behaviour. It is bound to an **uppercase** key, which is the
  operator's only cue that the key navigates away; lowercase keys act in place and
  leave the dashboard open. A handoff may put one step in front of itself (a picker
  modal) and still be a handoff. When it moves the operator nowhere (no pane to
  focus, no checkout bound, ineligible row, focus unavailable outside tmux) it does
  not quit: it reports why in the dashboard's status line and stays put. Because a
  handoff ends the surface that could report progress, it resolves from the
  **SetRef** it is given — never a **Project scan fan-out** — and says so the moment
  it dispatches: a handoff that works silently for seconds is indistinguishable from
  a dead key.
  was: A **Work dashboard** action that hands the operator's attention to a tmux
  pane it spawns or focuses, rather than acting in place. Every handoff verb
  performs the same steps in the same order — spawn the pane, or focus the existing
  one when that activity is already running for the set rather than re-sending the
  command into it; focus (`SelectPane` + `SwitchClient`); quit the dashboard — so no
  verb invents its own post-spawn behaviour. It is bound to an **uppercase** key,
  which is the operator's only cue that the key navigates away; lowercase keys act
  in place and leave the dashboard open. A handoff may put one step in front of
  itself (a picker modal) and still be a handoff. When it moves the operator nowhere
  (no pane to focus, no checkout bound, ineligible row, focus unavailable outside
  tmux) it does not quit: it reports why in the dashboard's status line and stays
  put.

~ Live-pane affordance
  The colouring of a **Handoff verb**'s key — in the **Work dashboard** action menu
  and, as a compact per-activity cluster, on the row itself — showing what that
  activity's pane for this set is doing, and thereby what the key will do. Dark: no
  pane, the key spawns one. Grey: a **Pane tag**ged pane exists but sits at a bare
  shell, its command finished — the key respawns. Green: the command is running —
  the key jumps to it. Read from tmux at open and once per dashboard poll, never
  from pop's own store, because a pane that dies leaves `list-panes` at once while a
  stored record outlives it. It is present in the dashboard's first paint: an
  affordance that arrives a poll late has already misinformed the operator about
  what the key does. It replaces a separate preview verb outright: the verb that
  starts a thing is the verb that returns you to it. It covers only the activities
  pop supervises — drain, verify, fold, assist, and the wayfinder session (keyed by
  its window name, which is the map id, rather than by a tag). A **Runtime shell**
  is the operator's process, not pop's: it is never tagged, never tracked, always
  dark, and every press spawns a fresh one.
  was: (as above, but "Read from tmux once per dashboard poll" and no first-paint
  clause — the cache was nil until the first reload landed.)
