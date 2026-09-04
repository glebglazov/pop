---
status: accepted
relates: "widens the trigger [ADR-0086](0086-agent-verification-is-a-pre-approval-drain-phase.md) set and [ADR-0252](0252-refine-fixes-in-place-before-the-verify-phase.md) inherited; leaves [ADR-0096](0096-pass-verdict-immunizes-terminal-status-against-sha-drift.md) and [ADR-0179](0179-human-completion-and-verification-are-two-axes.md) untouched"
---

# Refine and verification fire at every AFK standstill, not only at a terminal one

## Context

A real set went unjudged from end to end. `2026-09-02-vanilla-core-fold-in` holds
14 tasks with three HITL gates interleaved among them. Eight AFK tasks are done —
two of them, `13-shadow-application-for-qa-comparison` and
`14-honest-converge-rerun-semantics`, appended after planning. Its only artifact
is a `progress` record: no **Verify report**, no **Refine report**. The set had
never been verified and never been refined, and a human was being asked to sign
off at its first gate on eight tasks no **Verifier** and no **Refiner** had read.

Nothing was broken in the episode machinery, which was the first suspicion. New
work does re-arm both steps: a **Task transition** into `done` on an AFK task
invalidates the set's cached verdicts, `refineComposition` re-arms the **Refine
episode** on the same edge, and the scope-growth check catches a task appended by
a hand edit that never reached the chokepoint. All of that fired correctly.

The cause is the trigger both steps share. `refinePhase` and `verifyPhase` decline
unless the set reads DONE or AWAITING-APPROVAL, and `DeriveStatus` returns
**BLOCKED** for this set, because open AFK task `08-reward-relabel-service` sits
behind the gate. The comment in `status.go` states the assumption that broke:
"only the human approval gate remains (the agent has already verified — the human
signs off)." That is true of one trailing gate. On an interleaved ladder it is
false at every gate but the last, and the set reaches a genuine judging point once
per gate.

Two things make this cheap to fix. The drain already calls both phases on a
BLOCKED set — the loop runs them whenever no task is selectable, before the
terminal switch that handles BLOCKED itself — so each phase is invoked and
declines on its own status test. And both prompts are already scoped to `done` AFK
tasks alone, so neither would fault a set for work it cannot yet judge.

## Decision

**Both steps fire at an AFK standstill: a set whose AFK work has stopped, whether
because it is finished or because a gate holds it.**

`AFKStandstill(m)` joins `TerminalStatus(status)` in `status.go` as a *second*
named predicate rather than a widening of the first. `TerminalStatus` documents
itself as the one definition of its zone, keying verdict gating, mark resolution
and the **Human completion** bit's lifetime; widening it would move all three
silently. The two predicates sitting side by side put the split in the code.

- **Trigger.** A standstill is the terminal zone, or BLOCKED with a blocking HITL
  task. DEFERRED is excluded by construction — a deferral is the human saying "not
  now", and paying two agent passes for a set nobody is looking at is the opposite
  of the point. FAILED is excluded for the same reason it always was.
- **Scope.** Unchanged, because it was already right: the **Verifier** and the
  **Refiner** read the set's `done` AFK tasks and nothing else.
- **Marks.** The **Verification mark** resolves at every standstill, so a BLOCKED
  set verified at a mid-ladder gate carries its badge. The *status* stays keyed to
  the terminal zone: BLOCKED is never rewritten to VERIFY-FAILED.
- **Disposition.** Unchanged. FIXABLE spawns a **Remediation task**, which wires
  itself into the open HITL task's `blocked_by`, so the gate correctly waits and
  the drain has work again. A non-PASS never refuses the sign-off.
- **Cost.** Up to one Verifier and one Refiner per gate. The existing episode keys
  give the right cadence with no new mechanism: a re-drain at the same gate re-arms
  neither.

**Separately, "quiescence" leaves the glossary.** One hard word carried two
unrelated meanings — a drain having no runnable work, and nobody holding the
checkout. The drain sense becomes an **AFK standstill**; the lock sense becomes
**Checkout vacancy**, the word its own entry's prose already implied by naming the
*occupant* a refusal reports.

## Considered Options

- **Widen `TerminalStatus` instead of adding a predicate.** Rejected: it carries
  three consumers that must not widen with it.
- **Make a gated set read AWAITING-APPROVAL.** Rejected: BLOCKED and
  AWAITING-APPROVAL are a real distinction other surfaces need — one set has agent
  work left, the other does not.
- **Offer verify and refine as verbs at the HITL gate instead.** Rejected: it
  leaves the guarantee resting on a human noticing, which is what failed here.
- **Gate on "no runnable AFK task and one done AFK task", with no status test.**
  Rejected: it fires on a FAILED set, where a verdict on half-finished work is
  noise.
- **Refuse the HITL sign-off while the mark reads verify-failed.** Rejected:
  ADR-0179 settled that a human's assertion outranks a verdict, and a refusal
  inverts that. It also adds a verdict precondition to a verb that has none —
  the coupling ADR-0255 removed from `--remediate`.
- **Name the drain sense "AFK exhaustion", the code's existing phrase.** Rejected
  on the case being added: mid-ladder the gated AFK tasks still exist and will
  resume, so nothing is exhausted. "Standstill" carries the stop that resumes.

## Consequences

- Both phases test the new predicate; neither gains any other logic. The prompts,
  the episode keys and the invalidation hooks are untouched.
- The mark resolution takes a standstill test where it takes a terminal test today,
  so a BLOCKED row can render the **Verified-at SHA** badge. The badge needs no new
  state — its four states already cover it.
- A ladder pays per gate. This is intended: each pass judges an increment, against
  one pass at the end of a ladder that may take days.
- A remediation spawn at a mid-ladder gate makes the set drainable again, so the
  drain resumes instead of parking — a better outcome than at a terminal gate.
- The rename touches `Checkout quiescence` and its two referring entries, plus
  `store/quiescence.go` and `mutateWithCheckoutQuiescence` and the **Out-of-band
  mutation** refusal text.
