---
status: accepted
---

# Admission to a checkout is gated on the Checkout claim union

[ADR-0067](0067-runtime-lock-is-held-only-during-active-execution.md) released the **Runtime execution lock** at every human wait and accepted a consequence: with the lock gone, the queue could spawn a second set into an inline-trunk checkout while the first was parked. [ADR-0100](0100-quota-recovery-is-checkout-scoped-and-owned-by-implement.md) intended the **Recovery waiter** to claim "the set and checkout," but the implemented queue gate was set-scoped and `StartDrain` consulted only live running drains. Observed 2026-07-22 on one trunk: a second set admitted 11 seconds after a quota park, and again 17 seconds after a gate hold — while a third set, quota-recovered, sat blocked behind that gate hold for hours.

We now gate admission on a **Checkout claim** union, derived at read time (no new table): a live running **Drain** (execution), a live **Recovery waiter** (quota recovery — an automatic process that *will* resume, unlike a human wait), or a **Checkout gate hold** parked at a **Failed gate prompt** over uncommitted work (dirtiness snapshotted at park time, untracked files included, not re-evaluated live). The union is enforced at three chokepoints: `BeginDrain` (transactionally), queue dispatch (as spawn deferral with a reason), and **Recovery turn** acquisition (claims minus the acquiring waiter, then existing turn ordering). Every claim source carries owner PID + start token and is swept by the opportunistic reconcile — `recovery_waiters` gains owner identity for this. Gate-hold registration never replaces a different live owner's hold, and release deletes only the holder's own row.

Human waits deliberately claim nothing: the HITL gate, the verify-fail gate, approval tasks, and a *clean-tree* Failed gate neither stall queue dispatch nor block a waiter's resume. **Checkout quiescence** ([ADR-0104](0104-out-of-band-mutators-require-checkout-quiescence.md)) becomes "no Checkout claim and no Checkout gate hold," so `verify --accept` refuses during a quota wait (closing the race where a resume re-runs verify over a human-authored PASS); a quota-waiter refusal also reports whether that waiter is next under **Recovery turn ordering**.

## Considered options

- **Hold the Drain row through the quota wait.** Rejected: conflates occupancy with the exit-reason history the row records ([ADR-0056](0056-drain-outcome-is-the-process-exit-reason.md)) and re-couples the lock to process liveness, which ADR-0067 deliberately undid.
- **A single claims table replacing waiters and gate holds.** Rejected: drains double as outcome history and waiters carry reset/priority metadata turns need — a merged table means dual-write drift.
- **Dispatch-only enforcement.** Rejected: a manual `implement` slips past the queue; `BeginDrain` is the chokepoint both paths share.
- **All gate holds claim the checkout** (status quo for recovery turns, extended to admission). Rejected: a human reading a menu must not stall the queue — queue liveness was chosen explicitly over gate-time safety, with per-task commits and worktree isolation ([ADR-0046](0046-implement-defaults-to-worktree-execution.md)) as the mitigations.

## Consequences

- Amends ADR-0067's dropped-signal consequence: the anti-double-spawn signal is restored for execution, quota waits, and dirty Failed gates. Concurrent mutation during other gate sessions remains an accepted footgun mitigated by worktree isolation.
- Amends ADR-0100: the queue's waiter gate becomes checkout-scoped, and `RecoveryBlockGateHold` is retired — a recovered waiter resumes while a human sits at a clean gate menu, so manual verification steps run against a tree another set may be rewriting (accepted; checkout-mutating actions launched from a gate, e.g. reverify, re-acquire the claim like any drain).
- A set parked at a dirty Failed gate stalls that checkout's queue until a human clears the gate — unbounded, and unchanged by cleaning the tree mid-gate (the snapshot stands until the gate session ends).
