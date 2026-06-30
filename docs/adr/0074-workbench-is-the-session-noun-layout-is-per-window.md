---
status: accepted
---

# Workbench is the whole-session noun; Layout is the per-window tier

## Context

[ADR-0073](0073-session-templates-are-explicit-weighted-split-trees.md) named the
whole-session blueprint a *Session template* and deliberately reserved the word
**layout** for tmux's per-window pane geometry (`main-vertical`, `tiled`), listing
"Layout" under *avoid* for the whole-session concept. Two later refactors drifted
from that: the command became `pop layout apply` and a window's pane-tree field
became `window.layout`. The result was "layout" living at two scales — per-window
(the field, tmux-aligned) and whole-session (the command) — the exact cross-meaning
ADR-0073 tried to prevent, just relocated into the CLI surface.

## Decision

The whole-session blueprint is a **Workbench**. The per-window pane geometry is a
**Layout** — now a first-class glossary term pinned to tmux's own scope (the
arrangement and sizing of panes *within one window*; a window's `layout` field).
The two tiers are: a Workbench has named windows, each window has a Layout.

- **Why "layout" can't mean the whole session.** tmux defines "layout" strictly
  per-window (the five preset layouts; the serialized `#{window_layout}` string).
  There is no multi-window "layout" in tmux — that scope is a *session*. So the
  whole-session noun must be something else, and `window.layout` (our weighted split
  tree for one window) is *correctly* aligned with tmux and stays.
- **Why "Workbench."** A workbench lays tools out at fixed stations — a clean
  metaphor for a fixed multi-window/multi-pane arrangement. Command: `pop workbench`
  (alias `wb`); the array-of-tables is `[[workbenches]]`. A `[workbench]` table holds
  workbench-scoped options (e.g. `pick_on_create`); the collection is the plural
  `[[workbenches]]` array, resolvable in all three homes.
- **Back-compat.** `[[session_templates]]` is retained as a deprecated alias for the
  `[[workbenches]]` array (a load finding per ADR-0054), so existing configs keep
  loading.

Amends ADR-0073's naming; its weighted-split-tree geometry model is unchanged.

## Considered options

- **Keep "Session template."** Semantically fine but a mouthful in the command and
  picker, and "template" alone is generic. Rejected for ergonomics.
- **Promote "layout" to the whole-session scope** (follow the drift). Rejected — it
  re-crosses tmux's per-window meaning, the very thing ADR-0073 guarded against.
- **A metaphor like "Desk"/"Rig" or a literal "Workspace"/"Shape."** Workbench won as
  the metaphor that best fits "tools at fixed stations"; "workspace" reads as
  project/repo to many, the others were blander or more made-up.
