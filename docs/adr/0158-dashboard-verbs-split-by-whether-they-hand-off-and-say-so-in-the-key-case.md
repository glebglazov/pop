# Work dashboard verbs split by whether they hand off, and say so in the key's case

Every **Work dashboard** row verb is either a **Handoff verb** — it moves the
operator into a tmux pane and quits the dashboard — or an **In-place verb**,
which acts and leaves the dashboard standing. Handoff verbs take an uppercase
key (`I` drain, `V` verify, `F` fold, `S` assist, `O` shell, `I` wayfinder on a
map row), in-place verbs a lowercase one (`b` bind, `u` unbind, `a` auto-drain,
`s` status, `r` unpark, `x` archive, `y` copy name). Every handoff runs the same
sequence in the same order: spawn or focus the pane, `SelectPane` +
`SwitchClient`, quit. When it moves the operator nowhere it does not quit — it
says why in the status line and stays put.

## Context

The dashboard had grown four different post-spawn behaviours across six
spawning verbs. `i` drain and `v` verify spawned a pane and never focused it, so
they looked like no-ops: the work started in a `pop-queue` window of the
*project's* session, possibly a session the operator was not attached to, while
the dashboard sat there and reloaded. `S` assist focused only when a pane
already existed, but quit either way — so the first assist on a set closed the
dashboard and took you nowhere. `p` preview focused without spawning. `f` fold,
`s` status, and `O` shell ran inline through `tea.ExecProcess`, suspending the
TUI. Nothing in the naming or the key case distinguished these.

Worse, verify spawned under `TagSet` — the drain's **Pane tag** — so "find the
verify pane for this set" returned the drain pane and `SendKeys` typed
`pop tasks verify` into whatever agent was running there.

## What follows from it

- **One pane tag per activity**, not per set: drain, verify, fold, and assist
  each get their own, so a lookup can never reach another activity's process.
- **`tea.ExecProcess` leaves the queue dashboard entirely.** Fold becomes a
  spawned pane because its conflict prompt needs a TTY that outlives the
  dashboard; shell becomes a spawned pane so it survives the dashboard exiting;
  the status verbs stop shelling out to `pop tasks …` and write in-process, the
  way archive and unpark already did.
- **Preview is deleted.** A **Live-pane affordance** colours each handoff key by
  what its pane is doing — dark (spawn), grey (finished, respawn), green
  (running, jump) — so the verb that starts a thing is the verb that returns you
  to it. Liveness is read from tmux per poll, not from the `DrainPane` store,
  which outlives the pane it describes; that store drops to audit only.
- **Fold keeps its dashboard-side preflight.** A cheap in-place eligibility check
  lets the dashboard refuse without spawning a pane whose only content would be
  an error. The confirmation itself belongs to `pop tasks fold` in the pane.
- The case rule governs **row verbs only**. Mode and navigation keys (`a`/`A`
  action menu, `/`, `f` filter, `G`, `gg`) are outside it.

## Considered and rejected

**Keeping lowercase aliases** for `i`/`v`/`p`/`f`. Rejected: the case rule is
only worth having if the case is reliable, and `f`/`v` already mean filter menu
and routines view at list level.

**A fixed preview priority order** (drain > fold > verify > assist) instead of
deleting preview. Rejected: it takes you to the wrong pane exactly when a set is
busy enough to have several, which is when you would reach for it.

**Exempting handoff verbs from quitting while the action menu is pinned** (`A`).
Rejected: a verb that behaves differently inside a mode is the per-verb
inconsistency this decision removes, reintroduced one level down.
