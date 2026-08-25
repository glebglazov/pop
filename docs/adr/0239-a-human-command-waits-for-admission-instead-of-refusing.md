---
status: accepted
---

# A human command waits for admission instead of refusing

## Context

Every contention path in pop was a hard error. `StartDrain` refused with
`runtime execution already in progress (PID … at …)` or `checkout claimed by set
X (running drain)`; an **Assist session** refused with a live-drain message, or —
when a drain was parked at a gate rather than running — with the store's own
`checkout gate hold held by another live owner`. A survey of both flows found
fourteen refusal paths on the assist side alone and not one queue anywhere. The
only softer outcomes were pane *reuse* ([ADR-0158](0158-dashboard-verbs-split-by-whether-they-hand-off-and-say-so-in-the-key-case.md))
and the **Work daemon**'s **Spawn deferral**.

The cost lands on the human. A refusal makes the operator pop's scheduler: read
the error, work out who holds the checkout, poll it, re-run the command. Two of
those four steps are pop's own bookkeeping, and the third is a busy-wait
performed by a person.

## Decision

A human-initiated **Tree-stable operation** never refuses for internal
contention. It registers in the **Admission queue** and performs an **Admission
wait** until an **Admission grant** arrives.

The wait is unbounded. A bound would hand the waiting back to the human at the
least predictable moment, which is the failure this decision exists to remove.
What makes an unbounded wait tolerable is that it is *actionable*: the wait line
names the holder and where to reach it — set, **Claim reason**, PID, controlling
tty, drain pane — because the resolution is almost always to go and answer a
prompt that is still open. **Checkout quiescence** refusals already carry exactly
this information; the wait line reuses it.

Ordering is strict registration FIFO (**Admission queue**), so of two sets
already waiting the one that asked first goes first. **Task set priority** still
decides which Ready set the daemon picks next and is deliberately blind to a
queue that has already formed.

A waiter's registration is itself a **Checkout claim**, exactly as a **Recovery
waiter**'s is. The daemon therefore sees it and defers, so no dispatch can take
the window out from under a queued human, and no priority tier for humans is
needed.

Non-interactive callers are unchanged. The daemon keeps its **Spawn deferral**
(a non-blocking wait, already the right behaviour for a machine) and a
non-interactive invocation keeps a non-zero exit. `--wait` / `--no-wait`
override either way. The rule is that a *human* never re-runs a command, not
that no code path ever reports busy.

## Considered options

- **Wait everywhere, including scripts and the daemon.** Rejected: a CI job that
  blocks forever is worse than one that exits non-zero, and the daemon's
  poll-and-defer already keeps the promise without holding a process open.
- **A bounded wait with a timeout.** Rejected: it restores the re-run, just later
  and less predictably, and it would fire exactly when a colleague is parked at a
  gate — the case where waiting is most correct.
- **Priority-then-FIFO, reusing Recovery turn ordering unchanged.** Rejected: it
  cannot promise that of A, B, C the second to ask is the second to run, which is
  the guarantee the human asked for. Priority keeps its existing job instead.
- **A priority tier that puts humans ahead of the daemon.** Rejected as
  redundant: a waiter's claim already defers dispatch, so the tier would be a
  second mechanism for something the first one delivers.
- **Keeping a "already in progress" error for the same set, pointing at assist.**
  Rejected: it is a refusal, and it is the refusal a human hits most.

## Consequences

- Supersedes the refusal half of ADR-0135's chokepoints: `BeginDrain` still
  enforces the union, but a human caller queues on it rather than exiting.
- A queued command is visible as an **Admission indicator**, not a status value
  (the ADR-0111 separation).
- **Amends [ADR-0161](0161-gate-occupancy-is-set-scoped-except-the-dirty-tree-claim.md)
  by reaffirming it under new pressure.** Holding the checkout on *every* failure
  was considered and rejected: with waiting in place, an unnecessary claim no
  longer produces a loud refusal but a silent stall, so the dirty-tree-only rule
  becomes more important, not less. A clean-tree **Failed gate** still claims
  nothing.
- ADR-0135's unbounded dirty-Failed-gate stall is inherited by every waiter
  behind it. Accepted deliberately, mitigated only by the actionable wait line.
- A drain parked at a gate must produce the polite live-drain wait, not the
  store's `checkout gate hold held by another live owner`.
