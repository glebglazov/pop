---
status: superseded by ADR-0107
---

# Queue dashboard switches all rows to two lines below a width threshold

When the **Queue dashboard** task-set table would be cramped on a narrow pane, every row renders as **two lines** — not per-row variable height. Two-line mode activates when **either** the terminal width is below **80 columns** **or** any visible row's **Task set identifier** exceeds **36 characters**. The trigger is global: if one set id is long, all rows use the same two-line layout so scroll and cursor math stay uniform.

**Line 1** carries the ops scan: PROJECT, FLAGS (`AD`/`OR`), STATUS, WORKTREE, DRAIN. **Line 2** is the full Task set identifier, indented to align under the TASK SET column — set names get the full row width instead of ellipsizing or pushing other columns off-screen.

This deliberately extends the [ADR-0079](0079-shared-tui-list-foundation-is-for-selectable-lists.md) boundary: `List[T]` remains one *logical* item per row, but the dashboard Cell renderer emits two physical terminal lines when two-line mode is on, and the List body height budget counts each item as two lines. Rejected for now: cursor-row-only expansion (inconsistent row heights while scanning) and always-wrap set ids (breaks List without a mode switch).

Follows the compact FLAGS column and width-fitting pass; two-line mode is the next step when single-line fitting is not enough for long set ids on small panes.
