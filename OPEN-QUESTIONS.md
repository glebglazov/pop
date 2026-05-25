# Open Questions

## Keybindings: shared vs. separate for sessions picker and dashboard

Currently both "sessions" mode (folder picker) and "dashboard" mode (attention panes) share the same `keyMap` struct. Some keys only make sense in one mode:
- `x` / `f` / `F` / `r` — dashboard-only actions
- `ctrl+r` / `ctrl+x` / `ctrl+d` — sessions-picker-only actions

Should we split these into separate keybinding structs? The modes are quite distinct and likely to diverge further.

## Naming: "normal" mode is actually sessions/folders picker

In the code the primary picker is often referred to as "normal" mode or `viewNormal`. This is misleading — it's specifically a sessions/projects/worktrees picker. We should rename it to something like `viewSessions`, `viewProjects`, or `viewPicker` to make the intent clear.
