---
status: superseded
superseded_by: ADR-0087
---

# HITL disposition splits by remaining agent work (UNVERIFIED vs BLOCKED)

> ⛔ **SUPERSEDED BY [ADR-0087](0087-unverified-retires-to-awaiting-approval.md)** (UNVERIFIED retired to AWAITING-APPROVAL) and the **Verifier** subsystem in [ADR-0096](0096-pass-verdict-immunizes-terminal-status-against-sha-drift.md). The `UNVERIFIED` status and its durable `unverified` drain-outcome value no longer exist — grep `unverified` over the code returns nothing; terminal HITL now flows through `StatusAwaitingApproval` / `StatusVerifyFailed` and the `store.VerifyVerdict` cache. The remaining-agent-work split no longer describes any behaviour.

_Original rationale retired to cut LLM-misleading noise; the decision and its context now live in ADR-0087 and ADR-0096. See [ADR-0069](0069-adr-status-is-normalized-frontmatter-and-dead-adrs-are-tombstoned.md) for the tombstone policy._
