---
fragment: 1B2C1CB5
generation: 0006
branch: master
---

+ Runtime shell
  An attended interactive subshell (`$SHELL`, fallback `/bin/sh`) rooted at the **Runtime path**, offered as a menu option at the **HITL gate prompt** and **Failed gate prompt** and as the `O` action in the **queue dashboard**. It is a pure side-trip for running commands by hand in the checkout — typically an install or build (e.g. `make install-dev`) before sign-off. It never changes task state: on exit, control returns to the gate menu (or the dashboard) unchanged, with no Task-set refresh. In the dashboard it suspends the TUI for the subshell and resumes on exit; a row with no resolved checkout (empty Runtime path) makes the action a no-op with a status-line hint rather than opening a shell.
  avoid: assistance session, subshell escape, terminal
  under: Tasks

~ HITL gate prompt
  An interactive choice shown when implement reaches or selects a Human-blocked Task set. It defaults to getting agent assistance while still letting the human complete the task, defer it, open a **Runtime shell** in the checkout, or exit without changing task state; choosing complete or defer is the explicit manual decision and does not ask for a second yes/no confirmation. Exit is bound to the fixed key `0` (rendered last so its number never shifts as options are added); the other actions take ascending numbers. After complete or defer clears the blocking HITL task, implement refreshes the same Task set and continues from any newly eligible AFK task. When shown because a no-argument implement found no Ready Task set, it is framed as "No runnable AFK work" rather than as a dead end. It stays interactive in a drain pane with a TTY; `--yes` skips it for fully unattended runs.
  was: An interactive choice shown when implement reaches or selects a Human-blocked Task set. It defaults to getting agent assistance while still letting the human complete the task, defer it, or exit without changing task state; choosing complete or defer is the explicit manual decision and does not ask for a second yes/no confirmation. After complete or defer clears the blocking HITL task, implement refreshes the same Task set and continues from any newly eligible AFK task. When shown because a no-argument implement found no Ready Task set, it is framed as "No runnable AFK work" rather than as a dead end. It stays interactive in a drain pane with a TTY; `--yes` skips it for fully unattended runs.

~ Failed gate prompt
  An interactive choice shown when a drain reaches a Failed task. It defaults to re-running the task while still offering agent assistance, finishing by hand, opening a **Runtime shell** in the checkout, or exit without changing task state — the Failed-task counterpart of **HITL gate prompt**. Exit is bound to the fixed key `0` (rendered last so its number never shifts as options are added). It stays interactive in a drain pane with a TTY; `--yes` skips it for fully unattended runs.
  was: An interactive choice shown when a drain reaches a Failed task. It defaults to re-running the task while still offering agent assistance, finishing by hand, or exit without changing task state — the Failed-task counterpart of **HITL gate prompt**. It stays interactive in a drain pane with a TTY; `--yes` skips it for fully unattended runs.
