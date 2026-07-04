---
status: accepted
---

# A PASS Verify verdict immunizes terminal status against later commits

Once Agent verification returns **PASS** within a **verification episode**, a **Task set**'s terminal derived status (**DONE** for pure-AFK, **AWAITING-APPROVAL** when a HITL gate remains) no longer regresses to **NEEDS-VERIFY** when runtime HEAD moves past the verified work SHA. The set stays terminal; surfaces show a yellow **`verified @ <shortSHA>`** annotation when HEAD differs — on `pop tasks status` in the Details column, and on the **Queue dashboard** as a STATUS suffix (plus the detail-view header when a set is opened). **NEEDS-VERIFY** is reserved for terminal sets with no PASS in the current episode.

A **verification episode** ends when the set leaves the terminal zone (human **Open task** / batch reopen, or **Remediation task** spawn). That triggers **verification invalidation**: `DELETE` of every cached `verify_verdicts` row for `(repo, set)` in the Drain store (the table is a cache; **Captured verify run**s remain). The next terminal arrival requires fresh verification. Explicit re-verify (`pop tasks verify`, HITL gate Re-verify) still runs and may record a non-PASS verdict at the current HEAD.

This refines [ADR-0086](0086-agent-verification-is-a-pre-approval-drain-phase.md): SHA-exact lookup remains for force runs and for non-PASS verdicts at the current HEAD, but a prior PASS in the episode is an immunizing fact, not a lease that expires on every commit.

## Why

SHA-gating every terminal set on exact HEAD made post-PASS commits — merge commits, typo fixes, trunk catching up — look like "re-verify everything" even when AFK work was already cleared and the human's next act is approval or archive. At completion time the work was verified and working; later commits should not silently revoke that without a new completion episode.

## Considered Options

- **Keep exact-SHA gating (status quo).** Rejected: every HEAD move regresses to NEEDS-VERIFY, including after human-sign-off-ready sets.
- **Soft invalidation (verify epoch on the set).** Rejected in favour of `DELETE`: the verdict table is explicitly a cache, not an audit trail; epoch adds schema and query complexity for little gain over wiping rows at episode end.

## Consequences

- Status derivation gains a second lookup: latest PASS for `(repo, set)` in the current episode when no verdict exists at current HEAD. Non-PASS at current HEAD still wins (**VERIFY-FAILED**).
- Drain auto-verify (`ensureVerifyVerdict`) must not re-invoke the Verifier when an immunizing PASS exists unless the human forces re-verify.
- `tasks.Row` (or equivalent) carries **Verified-at SHA** for renderers; dashboard and status table share the field with different column placement.
- Invalidation hooks on `ResetTask` / `OpenTasks` and `spawnRemediationTask` call a shared `InvalidateVerifyVerdicts(repo, setID)`.
