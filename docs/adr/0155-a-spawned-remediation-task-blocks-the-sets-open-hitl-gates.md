---
status: accepted
---

# A spawned Remediation task blocks the set's open HITL gates

## Context

`writeRemediationTask` appends a **Remediation task** with `BlockedBy: []`
(`tasks/remediation.go:212`). Nothing wires it to the set's HITL tasks. A real
manifest shows the shape: in `2026-07-29-implement-rebind-policy`, the HITL task
`07` declares `blocked_by` `01`–`06`, while remediations `08` and `09` — spawned
after it was authored — appear nowhere in that list.

During an unattended **Drain** this is invisible: an open remediation is an
eligible AFK task, so `DeriveStatus` returns READY and the drain runs remediation
long before any gate can fire. The hole is the manual path. A human can complete
the approval gate — `pop tasks complete <set>/07-*.md`, or `C` in the **Task set
detail view** — while remediation is still open, because blocked_by validation
sees `01`–`06` all done and passes. The gate's declared scope also lies: it names
the six tasks it was authored against and omits the repair work that followed.

## Decision

On spawn, the new Remediation task's id is appended to the `blocked_by` of **every
open HITL task** in the set. HITL tasks already Done or Skipped are never rewired,
and existing sets are not backfilled.

## Consequences

- No derived **Task set status** changes. An open remediation already made the set
  READY, and once remediation lands the gate's dependencies are satisfied →
  AWAITING-APPROVAL, exactly as before. A reader looking for a behaviour change in
  the status derivation will find none: what this buys is that a manual sign-off is
  refused while agent repair work is pending, and that the gate names its true
  scope.
- Every open HITL task, not just the terminal approval one: there is no reliable
  "terminal gate" test at spawn time, and a mid-flow gate is no more signable than a
  final one while remediation is outstanding.
- No backfill means sets that already carry remediations keep the old, permissive
  shape. Accepted — rewriting stored manifests to add dependency edges a human never
  authored is worse than an inconsistency that ages out.
- Skipping a remediation task still satisfies the new edge, since a Skipped task
  satisfies `blocked_by` by design. The gate stays reachable through deliberate
  deferral, which is the intended escape hatch.

Interacts with [0086-agent-verification-is-a-pre-approval-drain-phase.md](0086-agent-verification-is-a-pre-approval-drain-phase.md)
and [0103-human-verdict-disposition-is-accept-or-remediate.md](0103-human-verdict-disposition-is-accept-or-remediate.md).
