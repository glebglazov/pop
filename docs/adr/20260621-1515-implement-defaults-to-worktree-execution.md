# Implement defaults whole-set drains to worktree execution

Status: accepted

`pop tasks implement` no longer treats foreground execution as current-checkout-only for whole Task-set drains. In a Worktree-ready project, an unbound whole-set Implement defaults to a managed worktree forked from the repository's execution base; `--inline` is the explicit escape hatch for draining in the current checkout. Single task-file runs stay current-checkout by default because they are surgical foreground operations rather than schedulable set drains.

This amends ADR-0036, which rejected Implement auto-provisioning. The earlier decision avoided nested-worktree and zero-setup concerns by making Implement only adopt where it ran; the new default accepts those Queue-style constraints for whole-set drains so manual and Queue-triggered set execution share the same integration path. Existing Worktree bindings still win, explicit Runtime-path overrides still win, and `--inline` does not bypass a binding because one Task set should not land in two histories.

The checkout formerly named `queue_base` is renamed to **Execution base** and the config key hard-renamed to `execution_base = true`. The old name was accurate when only the Queue provisioned managed worktrees; once foreground Implement uses the same base, keeping a Queue-only name would hide the shared execution contract. Existing user configs must be migrated rather than accepted through a deprecated alias; seeing `queue_base` is a hard config error with a targeted rename message, not a warning-and-ignore path that could silently reroute execution.
