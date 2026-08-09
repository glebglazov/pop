---
status: accepted
relates: "renders the groups of [ADR-0194](0194-agent-lists-are-grouped-by-kind-of-work-and-attended-is-one-of-the-groups.md) and the entries of [ADR-0195](0195-an-attended-entry-owns-its-whole-invocation.md); converts the gate-menu siblings of [ADR-0163](0163-the-interrupt-gate-is-the-fourth-sibling-of-the-hitl-failed-and-verify-fail-gates.md) from text to inline TUI; adds a cross-kind key beside the kind-owned verbs of [ADR-0173](0173-work-is-one-kind-interface-with-data-shaped-returns-and-kind-side-adapters.md)"
---

# One agent-override picker, and attended gates become inline TUI

## Context

Which agent and model a session will use was not visible where the choice is
made. An inline gate menu printed the resolved argv after the action; a
dashboard-launched session — `aS` on a Task set, `S` on a Map, map grilling —
printed nothing at all until the pane was already up. Nothing anywhere could
change it: `--agent` exists on two commands, and editing `config.toml` mid-flow
is not an override.

The two surfaces could not share a mechanic even if one existed. The Work
dashboard is bubbletea with a modal vocabulary it already owns; the five gate
menus (HITL, verify-fail, failed, interrupt, fold-conflict) and the two Routine
menus are sequential `fmt.Fprintln` over a `tty.Reader`. Identical keystrokes in
both is the requirement, and one renderer is the only way to keep them identical
as they change.

## Decision

1. **Attended gate menus become inline bubbletea programs** — no altscreen. A
   drain's gate sits in a scrolling log whose failure output *is* the context
   for the choice, and altscreen would hide it. Inline also keeps the
   subprocess story simple: the program exits before an attended agent or shell
   launches and a fresh one starts when it returns, which is the loop these
   menus already have. `ui/` widgets become composable into them.
2. **The text renderers are deleted, not kept as a fallback.** There is no
   TTY-less human to render to: a non-promptable input, `--yes`, or piped output
   already takes a path that returns without prompting. Two renderers means
   every menu change made twice. Coverage moves to golden-frame tests, as
   `ui/`'s widgets are tested today.
3. **`routine/refine.go` and `routine/project_edit.go` convert in the same
   pass.** They are attended gates on the same reader, and a session with a
   different picker breaks the one-mechanic rule immediately.
4. **A tea program must claim and release the terminal the way the gates
   already do.** `tasks/runner.go` saves and restores foreground around each
   agent subprocess and `internal/tty` holds the SIGTTIN/SIGTTOU backstop; a
   menu that reads the terminal is subject to the same invariant.
5. **One global key opens the override picker, in every gate and every
   dashboard page: `alt+a`.** Not `ctrl+s` — that is XOFF, and on a terminal
   with `ixon` enabled it freezes rather than reaching the program, which is
   precisely the bare drain terminal this has to work in. The key is *not* live
   inside a picker (including the agent picker itself): `alt` is already
   `ui/quickaccess.go`'s default quick-access modifier, and needing to change
   agent while picking something else is not a flow.
6. **The picker is two levels, numeric.** Level one lists the four groups in
   declared order; a digit opens that group's entries; a digit there applies the
   override and closes. `0` goes back a level, `Enter` exits changing nothing —
   so the default entry is what you get by not choosing, which is the same thing
   "Enter takes the default" always meant. Past nine entries, arrow/`j`/`k`
   navigation with Enter-on-cursor takes over; the digits stay the fast path.
7. **An override promotes, it does not pin.** The picked entry moves to the head
   of that group's list for the session; the configured remainder stays behind
   it. For implement, verify and routine that ordering carries the quota
   fall-through — a pin would silently disable it, and an unattended drain that
   dies overnight because someone nudged a model at dinner is the worst version
   of this feature.
8. **An override lasts for one OS process** — the dashboard, one Assist session,
   one drain — and is never persisted. Attaching it to a Task set or a Map would
   make it config by another name, and an override set three days ago is exactly
   the invisible state decision 9 exists to prevent.
9. **The effective entry is rendered wherever the choice is about to be made:**
   one shared one-line render in the gate menu, in the dashboard's action-menu
   row for every attended verb, in the pane title, and as a persistent dashboard
   block naming the current entry and the key that changes it. Where pop cannot
   know the model — an entry whose `cmd` names none — it says so rather than
   guessing: pop does not read the agents' own config files, and inventing a
   model name is worse than admitting the agent decides.

## Consequences

- Five gate menus and two Routine menus are rewritten. Their plain-text form was
  load-bearing for nobody, but it *was* what their current tests assert against,
  so those tests are rewritten too.
- A bubbletea program inside a drain is new: the drain already wrestles terminal
  foreground for its subprocesses, and a menu that owns the terminal joins that
  contention. Decision 4 is the constraint, not a description of existing code.
- `alt+a` is the first cross-kind key on the Work dashboard — every other letter
  comes from a kind's `Actions`. It has to be excluded from kind-supplied key
  space so no kind can claim it.
- Decision 7 means an override never removes an agent from a run, only reorders
  it. Someone wanting "only this one, no fallback" has no way to say it.
