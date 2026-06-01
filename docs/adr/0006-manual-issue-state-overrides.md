# Manual issue-state overrides outside the executor

`pop workload` exposes manual commands that move an issue between statuses without the executor: `complete-issue` (Open/Failed/Skipped → Done), `skip-issue` (Open → Skipped), and `reset-issue` extended to accept Skipped (Failed/Skipped → Open). Completion bypasses the Completion sentinel — no agent run, no acceptance-criteria verification, no commit; the human is the verifier and commits their own work. A Skipped issue is never executed yet **satisfies `blocked_by` for its dependents**, and a set whose remaining issues are all Done or Skipped becomes the new terminal status Deferred rather than Done.

## Why

A human-in-the-loop issue can deadlock a set: the HITL issue cannot be verified until its own follow-up issues run, but those follow-ups are `blocked_by` the HITL issue, which the executor will never auto-run. Without an override the only escape is marking the HITL issue Done prematurely — recording an unverified completion to unstick the queue.

`skip-issue` breaks the deadlock by deferring the issue while letting its dependents proceed. This deliberately punches a hole in the dependency invariant: downstream work runs against a prerequisite that was *set aside, not completed*. We accept that hole because skipping is an explicit human act with a clear intent ("I will conclude this once the follow-ups exist"), and Deferred keeps the unfinished issue visible so it is concluded (`complete-issue`) or reopened (`reset-issue`) later rather than forgotten.

`complete-issue` exists for the symmetric case — the human did the work by hand and wants Done without re-running an agent that would only re-verify what a person already verified. It bypasses the sentinel because there is no agent in the loop to emit one.

## Considered Options

- **Keep the deadlock; require premature Done.** Rejected: pollutes the record with unverified completions and conflates "done" with "deferred".
- **A dedicated "verify-later" dependency type instead of skip.** Rejected for this iteration: a new dependency kind is a larger manifest-contract change than a status plus an override command, and the deadlock is the only motivating case so far.
- **Restrict the overrides to HITL issues only.** Rejected: the same manual paths are legitimately useful for AFK and Failed issues; a generic verb avoids a second near-identical command.
- **Skipped does not satisfy `blocked_by`.** Rejected: it would not resolve the deadlock, which is the entire reason skip exists.

## Consequences

`skipped` joins the issue-status contract and `DEFERRED` joins the set-status set; every status switch (eligibility, blocker checks, status derivation, progress, render) must account for both. Tests must lock down that Skipped unblocks dependents, that Deferred is passed over by automatic selection like Done, and that completion via override writes no implementation commit. The dependency hole is intentional — a future reviewer should not "fix" Skipped to block dependents without revisiting this decision.
