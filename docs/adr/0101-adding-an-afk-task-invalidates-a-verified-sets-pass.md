# Adding an AFK task invalidates a verified set's PASS

A PASS certifies a **Task set** as verified end-to-end *as scoped*. Growing the
set changes what "end-to-end" means, so when a new AFK task is added to a set
that already holds a PASS, we invalidate its cached verdicts (as
`spawnRemediationTask` already does) rather than let the stale PASS immunize the
set via ADR-0096's idempotency. Without this, a task added by a direct manifest
edit (e.g. during a HITL assistance session) drains, the set returns to DONE at a
new work SHA, and `ensureVerifyVerdict` falls through to the prior PASS — so the
newly-added work is never verified. This makes the manual/HITL add-work path
symmetric with the remediation add-work path (add-work ⇒ invalidate).

Refines [0096-pass-verdict-immunizes-terminal-status-against-sha-drift.md](0096-pass-verdict-immunizes-terminal-status-against-sha-drift.md):
idempotency still absorbs incidental SHA drift, but no longer survives a scope increase.
