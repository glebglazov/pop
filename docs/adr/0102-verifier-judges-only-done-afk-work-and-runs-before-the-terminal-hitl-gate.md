# Verifier judges only done AFK work, and runs before the terminal HITL gate

The **Verifier**'s verdict scope is the set's `done` AFK tasks only. The prompt
excludes open/not-`done` AFK tasks and HITL tasks of any status, because an agent
cannot judge a human sign-off and a not-yet-run task is not an unmet criterion.
This fixes a deadlock: previously `buildVerifierPrompt` emitted every task in the
manifest, so a terminal HITL sign-off task still open at AWAITING-APPROVAL was
read as a failing criterion → NEEDS-HUMAN → the drain returned before the HITL
gate branch, and the human could never reach the gate that would clear it.

Ordering (chosen: verify-then-HITL): on an **Awaiting-approval Task set** the
Verifier runs *first* over the done AFK work; a PASS then opens the HITL sign-off
gate. So cheap agent checking precedes expensive human time, and a genuine AFK
defect still fails here (→ remediate/block) instead of sending a human to test
broken code.

## Considered options

- **HITL-then-verify** — skip verify at AWAITING-APPROVAL, gate the human first,
  verify the full set only after sign-off. Rejected: lets a human sign off on code
  that later fails automated verify — an awkward inversion — and loses the
  check-before-human-time ordering.

Interacts with [0086-agent-verification-is-a-pre-approval-drain-phase.md](0086-agent-verification-is-a-pre-approval-drain-phase.md)
(the pre-approval phase) and ADR-0045 (the HITL lifecycle).
