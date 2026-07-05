---
status: accepted
---

# Auto-drain consent auto-clears when a drain finalizes a set as terminal

When a drain finalizes with the **Task set**'s derived status **DONE** or **AWAITING-APPROVAL** — the two "AFK work complete" states — pop clears that set's **Auto-drain** consent bit (**Auto-drain clearing**). All AFK work is drained, so the daemon has nothing left to do, and the `AD` marker in the **Queue dashboard flags column** was drawing attention to sets that no longer act on it. The clear fires inside the shared drain runner (`RunTaskSetWith`), so foreground `pop tasks implement` and the **Queue daemon** get identical behaviour; it is idempotent, printed into the drain output, and recorded as a durable per-set progress entry.

Consistent with [ADR-0056](0056-drain-outcome-is-the-process-exit-reason.md): the drain **record** still stores only its exit reason. The clear reads the manifest-derived disposition already computed at the runner's terminal switch and acts on separate **Task state** — it does not persist disposition into the Drain.

## Why

The `AD` flag is protected in the flags column and never drops on narrow panes, so it kept demanding attention on sets that had finished draining — the very sets that realistically should be quiet. Clearing the consent bit (rather than only hiding the badge) keeps persisted state honest: what the dashboard shows matches what the daemon will do.

## Considered Options

- **Suppress the `AD` badge only, keep the bit on.** Rejected: leaves the daemon able to auto-re-fire on a later **Open task** / **Remediation task** / **Verification invalidation** while the dashboard implies it won't — display and persisted consent diverge.
- **Clear in the background reconcile reader** (runs before every dashboard build and daemon scan). Rejected: from the user's seat reconcile is invisible plumbing, so a "cleared" notice fired from there is untethered from any action; the notice wants to ride a visible drain-finalization event. (Reconcile would additionally catch the manual-`complete`-to-terminal path; we accept that gap instead — see Consequences.)
- **Clear inside the drain/executor before it records its outcome.** Rejected: that couples the drain to disposition, cutting against ADR-0056; finalization derives disposition via the existing reader instead.

## Consequences

- The clear discards consent, so a subsequent **Open task**, **Remediation task**, or **Verification invalidation** that reopens AFK work will **not** auto-re-fire the daemon — a human must re-mark **Auto-drain**. Accepted deliberately: a finished set staying quiet is the goal.
- **Accepted gap:** a set reaching DONE/AWAITING-APPROVAL via a manual **Complete task** with no drain does not pass through the runner, so its `AD` bit lingers. Rare (you'd auto-drain a set and then hand-complete it) and the human is present to toggle; not worth reintroducing the background reader to close.
- Trigger is exactly {DONE, AWAITING-APPROVAL}. **BLOCKED** (open AFK gated behind a human), **NEEDS-VERIFY** / **VERIFY-FAILED** (the verify→remediation→auto-drain loop is still live), and **FAILED** all keep the bit.
- Independent of the **Manifest auto-drain seed** ([ADR-0047](0047-manifest-auto-drain-seeds-at-registration.md)), which is read once at registration and never re-synced, so a cleared bit is never silently re-seeded on refresh.
