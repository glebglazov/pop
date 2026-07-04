---
status: accepted
---

# UNVERIFIED retires to AWAITING-APPROVAL; verify_failed is added

With an independent Verifier now running before any human sees a set (ADR-0086), "verify" becomes the agent's job and the human's terminal act is **approval / sign-off**. The status and Drain outcome formerly called `unverified` — agents done, human check remains — is therefore renamed **AWAITING-APPROVAL** / `awaiting_approval`: the agent already verified, so calling the state "unverified" would be a lie. A new `verify_failed` outcome records a set the Verifier could not clear (NEEDS-HUMAN, or the remediation cap exhausted).

## Why

This refines [ADR-0045](0045-hitl-disposition-splits-by-remaining-agent-work.md), which split the terminal-HITL disposition out of `blocked` precisely so "awaiting your check" reads honestly. Inserting an agent gate in front of that check shifts the meaning of the check itself from "verify" to "approve," and the vocabulary has to follow or it re-introduces the ambiguity ADR-0045 fixed.

## Considered Options

- **Repurpose `UNVERIFIED` to mean "agent has not verified yet."** Rejected: it shifts the meaning of a durable value — old records meant "awaiting human," new ones would mean "awaiting agent" — the exact semantic drift ADR-0045's append-only-journal consequence warns against.
- **Keep `UNVERIFIED` as the human state.** Rejected: after the agent verifies, "unverified" contradicts reality and reopens the naming collision.

## Consequences

- The outcome journal is durable and append-only. `awaiting_approval` and `verify_failed` are appended; the legacy `unverified` value is **not** rewritten on disk — every reader maps a stored `unverified` forward to `awaiting_approval`. The vocabulary cannot be cleanly un-shipped, which is why this is recorded.
- Surfaces that named UNVERIFIED (`pop queue status`, dashboard, journal, the to-tasks planning skill's "verification at the end" role) re-label to approval/sign-off.
