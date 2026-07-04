---
fragment: 8ebfb6ff
generation: 0016
branch: fix-worktree-branch-picker-cursor
---

~ Interrupted task
  A task whose active agent process was terminated by user interruption (graceful SIGINT teardown) or process termination. The task executor forwards termination to the agent process group, preserves partial implementation changes, persists the interrupted attempt's Captured attempt stream (so a later resume can carry its in-flight narrative forward), appends no Progress record, and exits without committing. An interrupted task is not Failed. A hard kill of pop itself writes no stream — that is a crashed Drain, and the resume then has only the checkout diff to build on.
  was: A task whose active agent process was terminated by user interruption or process termination. The task executor forwards termination to the agent process group, preserves partial implementation changes, leaves task artifacts unchanged, and exits without committing. An interrupted task is not Failed.

~ Agent quota pause
  The clean stop produced by Agent quota detection. It leaves the current task Open and preserves its partial runtime changes, and persists the paused attempt's Captured attempt stream, so a later implement invocation may resume work after allowance returns — the resuming agent inherits the paused attempt's in-flight context the same way a resumed Interrupted task does.
  was: The clean stop produced by Agent quota detection. It leaves the current task Open and preserves its partial runtime changes, so a later implement invocation may resume work after allowance returns.
