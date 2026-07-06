---
fragment: FA1LRETR
generation: 0007
branch: failed-task-implement-gate
---

+ Failed drain gate
  A set-wide hard stop during Implement: while any task in the set is Failed, no other AFK task runs — even open tasks with no blocked_by dependency on the failure. Re-entering Implement on that set must land on the Failed gate prompt for the first failed task (manifest order), not advance to the next eligible open task.
  avoid: per-task failure skip, continue past failure

~ Exhausted task
  A task that remains unsuccessful after its maximum attempts. The task executor marks it Failed locally, preserves any partial implementation changes for inspection, does not commit them, and stops draining the whole set until the failure is cleared.
  was: A task that remains unsuccessful after its maximum attempts. The task executor marks it Failed locally, preserves any partial implementation changes for inspection, does not commit them, and stops draining.

~ Failed gate prompt
  An interactive choice shown when a drain reaches or re-enters a Failed task during a foreground Implement. It defaults to re-running the task while still offering agent assistance, finishing by hand, opening a Runtime shell in the checkout, or exit without changing task state — the Failed-task counterpart of HITL gate prompt. Exit is bound to the fixed key 0 (rendered last so its number never shifts as options are added). It stays interactive in a drain pane with a TTY; --yes skips it for fully unattended runs. Queue-initiated drains never show this menu; **Retriable failure** (quota) is healed by Implement recovery wait, not queue reopen.
  was: An interactive choice shown when a drain reaches or re-enters a Failed task during a foreground Implement. It defaults to re-running the task while still offering agent assistance, finishing by hand, opening a Runtime shell in the checkout, or exit without changing task state — the Failed-task counterpart of HITL gate prompt. Exit is bound to the fixed key 0 (rendered last so its number never shifts as options are added). It stays interactive in a drain pane with a TTY; --yes skips it for fully unattended runs. Queue-initiated drains never show this menu; they auto-reopen only Retriable failures (see Queue failed recovery).

+ Failure reason
  The structured why recorded on the latest Captured run footer for a task attempt — the durable source read by LatestFailureReason and the Failed assistance prompt, distinct from the human-facing progress.txt line. It is not persisted on the task manifest (only failed_after is). Harness contract verdicts (missing Completion sentinel, empty summary, unchecked acceptance) and agent-emitted TASK_FAILED text are both failure reasons; quota exhaustion is not — it produces an Agent quota pause while the task stays Open.
  avoid: failed_after, progress record line

~ Retriable failure
  A stop Implement heals unattended via **Agent quota recovery wait** without a human Failed gate decision — **Agent quota pause** on a task attempt or **Verifier** invocation. Not an **Exhausted task**; those require human disposition via the **Failed gate prompt**.
  was: A stop the Queue may heal unattended without a human Failed gate decision — today only Agent quota pause (task stays Open; queue waits for cooldown). Not an Exhausted task whose agent could not finish the work; those require human disposition via the Failed gate prompt.

+ Queue failed recovery
  The queue-initiated branch of failed-drain handling: never auto-reopen an Exhausted task. Queue spawn policy stays Ready-only (Failed sets are not re-spawn candidates); quota healing stays on the Agent quota pause path. When a queue-spawned drain hits an Exhausted task, Implement's set-wide hard stop applies and the Failed gate runs under the same interactive/static rules as any other implement — queue adds no separate reopen logic.
  avoid: auto-retry, queue reopen
