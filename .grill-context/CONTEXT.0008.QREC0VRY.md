---
fragment: QREC0VRY
generation: 0008
branch: quota-recovery-coordinator
---

+ Agent quota recovery coordinator
  The machine-global `tasks/` primitive in `pop.db` that coordinates post-quota resume. **Agent preset** cooldowns stay machine-global; **Recovery turn** scope is per **Runtime path** so unrelated worktrees may resume in parallel but only one waiter per checkout re-enters drainage at a time — preventing parked waiters from releasing the **Runtime execution lock** while another set starts committing on the same tree. **Implement** owns the wait loop; **Queue** reads the coordinator to avoid duplicate spawns but does not own turn logic (consistent with ADR-0043).
  avoid: queue quota scheduler, global drain mutex, bob

+ Recovery waiter
  A quota-paused drain registered with the **Agent quota recovery coordinator** while its owning process polls for a **Recovery turn**. It names the task set, exhausted preset, reset instant, and the **Runtime path** from the set's **Worktree binding** at park time. Registration claims the set and checkout against duplicate queue spawns for the duration of the wait; deregistration happens on turn taken, interruption, or process exit.
  avoid: quota backoff, agent cooldown entry, parked set

~ Agent quota recovery wait
  The poll loop an implement process enters after **Agent quota pause**: park the drain (`quota_paused` terminal, **Runtime execution lock** released per ADR-0067), register a **Recovery waiter**, poll the **Agent quota recovery coordinator** until a **Recovery turn** is granted, then `BeginDrain` and resume the same open task. Applies to foreground and unattended drains alike — the pane shows the wait rather than exiting for human re-run.
  was: The poll loop an implement process enters after **Agent quota pause**: park the drain (`quota_paused` terminal, **Runtime execution lock** released per ADR-0067), register a **Recovery waiter**, poll the **Agent quota recovery coordinator** until a **Recovery turn** is granted, then `BeginDrain` and resume the same open task. Applies to foreground and unattended drains alike — the pane shows the wait rather than exiting for human re-run.
  avoid: in-process sleep, quota retry loop, blocking wait, --yes-only wait

+ Quota recovery resume point
  Where **Agent quota recovery wait** re-enters work after a **Recovery turn**: the same open task for a mid-drain task-attempt pause, or the **Verifier** for a post-drain verify pause — never a completed task re-run. Any **Agent preset** invocation during implement (task attempt or verify) may trigger recovery wait; all share the same checkout-scoped **Recovery turn**.
  avoid: verify-only wait, task-only wait, full drain restart

~ Retriable failure
  A stop Implement heals unattended via **Agent quota recovery wait** without a human Failed gate decision — **Agent quota pause** on a task attempt or **Verifier** invocation (task stays Open or verify re-runs after turn). Not an **Exhausted task** whose agent could not finish the work; those require human disposition via the **Failed gate prompt**.
  was: A stop the Queue may heal unattended without a human Failed gate decision — today only Agent quota pause (task stays Open; queue waits for cooldown). Not an Exhausted task whose agent could not finish the work; those require human disposition via the Failed gate prompt.
  avoid: retrieval failure, auto-retry, transient failure

+ Recovery turn
  One granted slot to resume agent work on a given **Runtime path** after the waiter's exhausted **Agent preset** cooldown clears globally. A waiter acquires a turn only when no other drain is actively executing on that checkout and the waiter is first under **Recovery turn ordering** for that path. The turn is preset-agnostic — at most one recovery resume per checkout at a time regardless of which **Agent preset** each waiter exhausted. Parallel worktrees resume independently; the guard is against multiple sets mutating the same checkout.
  avoid: next shot, quota lease, recovery gate, per-preset queue

+ Recovery turn ordering
  Among **Recovery waiters** on the same **Runtime path**, turns go to the highest **Task set priority** first; equal priority breaks FIFO by registration time. **Worktree binding** supplies the path at park time; ordering does not compare across checkouts.
  avoid: round-robin, jitter lottery, global FIFO, per-preset queue

~ Agent quota pause
  The clean stop produced by **Agent quota detection**. It leaves the current task Open and preserves its partial runtime changes, and persists the paused attempt's **Captured run**. The drain then enters **Agent quota recovery wait** rather than exiting. The resuming agent inherits the paused attempt's in-flight context the same way a resumed **Interrupted task** does.
  was: The clean stop produced by **Agent quota detection**. It leaves the current task Open and preserves its partial runtime changes, and persists the paused attempt's **Captured run**. An unattended drain then enters **Agent quota recovery wait** rather than exiting; a foreground drain without `--yes` may still exit for human re-run. The resuming agent inherits the paused attempt's in-flight context the same way a resumed **Interrupted task** does.
  avoid: Exhausted task, Interrupted task, Failed task
