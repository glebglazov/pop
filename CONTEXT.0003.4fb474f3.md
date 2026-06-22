---
fragment: 4fb474f3
generation: 0003
branch: master
---

~ Open task
  Explicitly returning Failed, Skipped, or Done tasks to Open via `pop tasks open`, regardless of task type — the command is named for the target status. It is the inverse of **Complete task**: undoing a premature completion (e.g. a human-in-the-loop task marked Done before its verification was actually finished) is as valid as retrying a Failed task or re-running a Done AFK task. Reopening a Done task flips the derived **Task set status** out of DONE; for a Done AFK task it becomes eligible again, so a later **Implement** — or the **Queue daemon** in an auto-drain set — re-fires an agent on it. It accepts either a Task-set-relative file reference `<task-set>/<file>.md`, which opens exactly that one task with no prompt, or a whole-set form (`<task-set>` or `<task-set>/`), which opens a **Multi-task selection** where Failed, Skipped, and Done tasks are all checkable (no row pre-checked) and an already-Open task is locked at-target. It removes any recorded attempt count, appends a local progress entry, preserves runtime files, and does not commit. Open task batches need no ordering. The status table prints copy-paste open hints only for Failed tasks; Done and Skipped tasks are reopenable but never advertised there.
  avoid: Issue reset, reset, automatic retry, uncomplete
  was: Explicitly returning Failed or Skipped tasks to Open via `pop tasks open` so they may be attempted again — the command is named for the target status. It accepts either a Task-set-relative file reference `<task-set>/<file>.md`, which opens exactly that one task, or a whole-set form (`<task-set>` or `<task-set>/`), which opens a Multi-task selection of the set's Failed and Skipped tasks. It removes any recorded attempt count, appends a local progress entry, preserves runtime files, and does not commit. Open task batches need no ordering — each transition is independent. The status table prints copy-paste open hints in the `<task-set>/<file>.md` form.

# NOTE for grill-consolidate: the task state-machine diagram in the
# CONTEXT.md domain-model overview (the `open ─ failed ─ skipped ─ done`
# ASCII diagram) must gain a `done ──Open task──▶ open` reverse edge — today
# `done` has only inbound edges. Diagram lives in base prose, not a term, so
# it is folded in at consolidation, not via a term delta op.
