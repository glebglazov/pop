# Open Questions

## Keybindings: shared vs. separate for sessions picker and dashboard

Currently both "sessions" mode (folder picker) and "dashboard" mode (attention panes) share the same `keyMap` struct. Some keys only make sense in one mode:
- `x` / `f` / `F` / `r` — dashboard-only actions
- `ctrl+r` / `ctrl+x` / `ctrl+d` — sessions-picker-only actions

Should we split these into separate keybinding structs? The modes are quite distinct and likely to diverge further.
