---
status: accepted
---

# Workbench apply reconciles by pane identity

## Context

[ADR-0073](0073-session-templates-are-explicit-weighted-split-trees.md) made apply
**additive + skip-by-name at the window level** and explicitly **descoped healing /
re-apply**: reshaping an already-built window was deemed impossible to do
surgically, because a tmux window's split tree is creation-order-bound and not
reconstructable.

But a real workflow wants exactly that: grow a live session from one Workbench into
a richer one — e.g. a two-row `minimal` (vim, claude) into a three-row `gs-dev` that
keeps those two panes and appends a third row of dev servers — **without killing the
running processes**. ADR-0073's window-skip makes that impossible: the second
Workbench's window matches by name and is skipped wholesale, so nothing is added (we
confirmed this empirically — `apply gs-dev` over a live `minimal` printed
`window "dev" already exists ... skipping` and added no panes).

## Decision

Apply is a **recursive, name-keyed, append-only merge**, not a window-level skip.

- **Identity lives in pop-owned tmux user options, never in clobberable fields.** On
  realize, each leaf is stamped `@pop_pane <spec-name>` (`set-option -p`, the
  ADR-0058 pattern) and each window `@pop_wb_window <name>` with `automatic-rename
  off`. Matching reads `#{@pop_pane}` / `#{@pop_wb_window}` — `pane_title` and
  `window_name` are display-only and get rewritten by vim, shells, and tmux's
  auto-rename, so they cannot carry identity. Titles are still set for humans.
- **Merge walk.** A matched window is recursed into (not skipped): each target leaf
  whose name matches a live pane is left untouched (process intact); each missing
  leaf is created by splitting relative to its nearest live sibling (`-h` for a
  missing column, `-v` for a missing row); a wholly-missing container is built fresh
  off its parent anchor. Containers are matched implicitly through their named leaves
  (internal split nodes are unnamed per ADR-0073).
- **Append-only, never destructive.** Reapply never removes, reorders, or kills.
  Survivors may be **resized** to honor the target's weights (unavoidable — a new
  pane must take cells), so `vim`'s `weight 1 → 3` reproportions the live pane; its
  process is untouched. The result may be a *superset* of the target if extra panes
  were opened manually.
- **Unnamed leaves are anonymous (B1).** Without a name there is no identity, so an
  unnamed leaf is always (re)created. Reapply-safety is therefore an opt-in property
  of *naming your panes*. A duplicate pane name within one window is a non-fatal load
  finding (ADR-0054) that flags the Workbench reapply-unsafe rather than aborting.
- **Two entry points, deliberately asymmetric.**
  - The **`pop workbench apply` command** (into an existing session) keeps ADR-0073's
    spirit: purely additive merge, never touches what it didn't create.
  - The **picker create-path** (a session *born* from a Workbench, gated by the
    opt-in `[workbench] pick_on_create`) makes the session *exactly* the Workbench:
    it removes the stray shell window `tmux new-session` always births, so no junk
    window survives.
- **`before_apply`.** A Workbench-scoped command array of one-time side effects
  (repo setup: pull, decrypt, mkdir), run with cwd = the session directory, **on
  every apply** of the Workbench being applied (the caller owns idempotency). Not
  shell-env propagation — that would evaporate across panes; it is on-disk / service
  side effects only.

Amends ADR-0073 (reverses "healing is descoped" and window-level skip-by-name).

## Considered options

- **General reshape** (allow arbitrary 1→2: reorder, drop, reweight-with-move). The
  only tmux-honest realization is kill-the-window-and-rebuild, which destroys running
  work — ADR-0073 already called this hostile. Rejected; append-only is the safe
  subset that covers the real workflow.
- **Identity by `pane_title` / `window_name`.** Rejected — both are rewritten by
  running programs and tmux auto-rename, so matching would silently break the moment
  vim or a shell prompt touched the title.
- **Positional identity for unnamed leaves.** Rejected — inserting one pane shifts
  every downstream index, causing wrong matches or duplicates. Names are the identity;
  unnamed means disposable.
