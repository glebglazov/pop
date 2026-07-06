---
status: accepted
---

# Quota recovery is checkout-scoped and owned by implement

When **Agent quota detection** stops a drain, implement parks the segment (`quota_paused` terminal, **Runtime execution lock** released per [0067](0067-runtime-lock-is-held-only-during-active-execution.md)), registers a **Recovery waiter** in `pop.db`, and polls an **Agent quota recovery coordinator** until it may resume — instead of exiting for the **Queue** to re-spawn. Resume applies to mid-drain task attempts and post-drain **Verifier** invocations alike; the **Quota recovery resume point** re-enters the open task or re-runs verify, never a completed task.

**Agent preset** cooldowns stay machine-global ([0034](0034-reset-aware-agent-cooldown.md), [0043](0043-agent-fallback-owned-by-implement.md)). **Recovery turns** are scoped per **Runtime path** (from **Worktree binding** at park time), preset-agnostic, and ordered by task-set priority then FIFO registration — so parallel worktrees resume independently but only one set re-enters drainage per checkout at a time. That guard exists because park releases the runtime lock: without a checkout claim, queue could spawn another set onto the same tree and commits would collide.

The queue reads recovery waiters (and live drains) to skip duplicate spawns; pinned-agent **SetBackoff** for quota is retired. SIGINT during the wait deregisters the waiter and exits as an interrupted drain; the task stays Open.

## Considered options

- **Exit on quota; queue re-spawns after cooldown.** Rejected for raw implement and for the checkout race: multiple parked processes or a queue tick could resume overlapping work on one checkout.
- **Machine-global recovery turn per agent preset.** Rejected — subscription stampede was not the primary risk; checkout contamination was. Per-checkout turns match the runtime lock's unit.
- **Per-(checkout, preset) turns.** Rejected — two agents on one checkout still share one working tree; one recovery resume at a time regardless of preset.
- **Foreground implement exits; only `--yes`/queue waits.** Rejected — foreground panes should show the wait and heal without manual re-run.
- **Keep pinned SetBackoff alongside the coordinator.** Rejected — one spawn-skip source of truth in `pop.db`.

## Consequences

- Implement grows a recovery poll loop and store tables for waiters/turns; queue's `recordPinnedQuotaCooldowns` and quota **SetBackoff** display paths shrink.
- A weekly quota wait can hold a pane for days with no runtime lock; recovery registration is the occupancy signal instead.
- Cross-worktree resume on the same preset may hit quota again quickly; accepted in favour of checkout isolation.
- **Failed gate prompt** and **HITL gate prompt** park registers a **Checkout gate hold** on the runtime path so **Recovery turn** acquisition waits until the gate session ends — ADR-0067 lock release stays, but checkout occupancy is tracked in the coordinator.
