# Human verdict disposition is Accept or Remediate

When a human confronts a non-PASS **Verify verdict**'s findings, the response
vocabulary is two actions, keyed on whether they agree the finding is a real
defect:

- **Accept** — disagree / non-blocking. Recorded as an **Accepted verdict**: a
  human-authored PASS row (flagged human-authored, carrying a note). It reuses
  PASS idempotency and scope-growth invalidation (ADR-0101), so `ResolveVerifiedStatus`
  needs no change. The note feeds forward as *context* into later Verifier prompts
  (suppressing re-flagging of a known non-issue) but never gags a fresh judgment.
- **Remediate** — agree it's a defect. Spawns a **Remediation task** carrying the
  human's note, forceable from a NEEDS-HUMAN verdict or over the auto-cap (the
  auto path only fires for FIXABLE under cap).

Surfaced interactively via the **Verify-fail gate prompt** and headless via
`pop tasks verify <set> --accept/--remediate "<note>"`.

## Considered options

- **A separate override table** for accepts. Rejected: an Accept *is* a PASS
  authored by a human, so modelling it as a PASS row inherits idempotency,
  invalidation, and status derivation for free.
- **Keeping "re-verify with a note" as a third disposition.** Rejected as
  redundant: if you think the Verifier is wrong, you override (Accept) rather than
  ask it to re-run and hope it flips; plain re-running already exists as a separate
  force action (HITL-gate re-verify / `pop tasks verify`) and fires automatically
  when the work SHA moves.
