---
status: accepted
---

# An admission grant is atomic

## Context

[ADR-0239](0239-a-human-command-waits-for-admission-instead-of-refusing.md)
removes the refusal, and with it a guarantee nobody had to think about before:
fail-fast is deadlock-free by construction. A process that gives up cannot be
half-way into a lock-set.

Once commands wait, cycles become reachable. There are two resources —
**Checkout claim** (a **Runtime path**) and **Set claim** (a task set across all
checkouts) — and an operation such as an AFK task attempt needs both. A drain
parked at a gate holding a set-scoped **Checkout gate hold**, and a queued drain
holding the checkout window while waiting on that set, is a cycle that neither
side can break.

## Decision

A waiter holds nothing while it waits. An **Admission grant** takes the entire
lock-set — **Checkout claim** and **Set claim** together — inside one
transaction, or grants nothing and the waiter keeps waiting. Acquisition is
never incremental.

This is deadlock-freedom by structure rather than by discipline: a waiter that
holds no partial lock cannot be part of a cycle, so no call site has to remember
a lock order and no future call site can get it wrong.

`store.TryAcquireRecoveryTurn` ([store/recovery_turn.go](../../store/recovery_turn.go))
is generalised into the coordinator. It already does this shape for **Recovery
turn**s — checking the claim union, the turn table and the ordering inside a
single transaction and returning a typed block reason with no TOCTOU — so the
mechanism is extended rather than invented, and the two queues share one
transaction boundary instead of racing each other on one checkout.

On grant, the operation re-derives its target's status before acting. Work that
completed while it waited — the set drained by a second queued command, signed
off from an assist menu, archived — is a clean zero exit reporting nothing left
to do, not an error and not a gate prompt. The command was asked to happen and
it happened.

## Considered options

- **A fixed global lock order (checkout before set, always).** Rejected: correct,
  but it is an invariant every present and future call site must uphold by hand,
  and a violation shows up as a hang rather than a compile error.
- **Deadlock detection with a victim.** Rejected: it needs a policy for which
  waiter to sacrifice, and sacrificing one returns a refusal to a human — the
  thing ADR-0239 exists to eliminate.
- **A second queue beside the recovery waiters, with a tie-break between them.**
  Rejected: two independent queues on one **Runtime path** each see the other's
  claim and can starve each other, which is the class of bug
  [ADR-0135](0135-admission-to-a-checkout-is-gated-on-the-checkout-claim-union.md)
  was written to remove.
- **Drain on the status the command queued with.** Rejected: it can re-drain a
  set a human signed off seconds earlier.
- **Drop into the gate menu the new status calls for.** Rejected: it converts a
  background wait nobody is watching into a prompt demanding attention.

## Consequences

- One coordinator owns admission for both fresh work and quota recovery, so
  **Recovery turn ordering** (priority-then-FIFO) and the **Admission queue**
  (strict FIFO) are two orderings inside one grant path and must be reconciled
  there rather than in two tables.
- A waiter is a registration, not a **Drain** row — ADR-0135 rejected holding a
  row through a wait, and [ADR-0056](0056-drain-outcome-is-the-process-exit-reason.md)
  keeps the row as execution history.
- "Nothing left to drain" becomes a normal, successful outcome of `pop tasks
  implement`, and callers that treat any non-drain as failure need updating.
