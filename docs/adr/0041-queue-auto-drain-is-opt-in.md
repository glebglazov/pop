# Queue auto-drain is opt-in, and the dashboard is the primary launcher

## Status

accepted

## Context

The **Queue daemon** (`pop queue run`) drained every **Ready** Task set across
registered repositories by standing consent: launching the daemon *was* the
unattended-AFK consent, and the only per-set opt-out was **Archive** (see
ADR-0027, the `Queue scope` glossary entry). There was no way to see, ahead of
time, what the daemon would pick up, nor to choose per-set where a drain should
land — the daemon owned both the decision to drain and the worktree-vs-base
placement.

## Decision

Invert the consent model and add a hands-on control surface:

- A new persisted per-set **Auto-drain** bit lives in **Task state** alongside
  `priority` and `archived`. It **defaults off**. The daemon now drains only
  sets a human has marked auto-drainable; an unmarked set is left alone.
  Auto-drain is orthogonal to **Archive** (Archive hides a set entirely;
  Auto-drain governs only daemon pickup of a still-visible set).
- A new **`pop queue dashboard`** (machine-global TUI, sibling to the Project
  and Worktree pickers) becomes the **primary** way to start Queue work. It
  lists every non-archived set with outstanding queue-actionable state across
  all repositories and lets the operator drain (`i`), integrate (`I`), bind or
  create a worktree (`b`), abandon (`U`), inspect (`s`), toggle Auto-drain
  (`a`), and preview the working pane (`p`). Manual `i` drains a set regardless
  of its Auto-drain bit — Auto-drain gates only the daemon's autopilot, never
  the human.
- The dashboard's `i` reuses the Queue's per-set drain-spawn (provision a
  managed worktree for a worktree-ready project, else drain the Execution base in
  place), so manual and automatic drains share one provisioning module and
  identical binding semantics; the only difference is human-trigger vs the
  Auto-drain gate.

## Consequences

- **Behavior change for existing users.** After upgrade, `pop queue run` drains
  nothing until sets are marked auto-drainable. A standing-consent autopilot
  silently becomes inert until opted into. This is the deliberate cost of
  moving from opt-out to opt-in.
- Global cross-project **priority** ordering remains a non-goal (ADR-0027); the
  dashboard sorts by project then set identifier, not by a global rank. Priority
  is out of scope for this change.
- The daemon survives unchanged in shape (fully-parallel per-repo draining); it
  simply filters its candidate sets by the Auto-drain bit.
