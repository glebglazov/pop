---
status: accepted
---

# Resuming a task carries the prior in-flight attempt's context forward

> **Relates:** extends ADR-0040 from the intra-drain failure retry to the cross-drain resume of a non-terminal attempt

ADR-0040 taught the unattended retry loop to carry a stream-derived, failure-typed digest of prior attempts forward — but scoped deliberately to *failures within one drain*: the digest fires only on `attempt > 1` (the in-memory loop counter) and its filter keeps only `Failed`/`TimedOut` outcomes, because "only failures teach a retry." That scope leaves a real hole. When a drain is interrupted (a deliberate SIGINT — e.g. to stop spending metered tokens and wait for a subscription window to regenerate) or hits an **Agent quota pause**, the task stays Open with its partial changes preserved, but the *next* drain that resumes it spins up a fresh retry loop starting at `attempt := 1`. The loop counter has no memory across drains, so the resuming agent takes the base-prompt branch, and the interrupted/paused attempt's in-flight narrative — already persisted as a **Captured attempt stream** — is never carried. The agent re-discovers the half-done work in its checkout with no orientation.

Decision: the carry-forward becomes driven by *content on disk*, not the loop counter, and its outcome filter admits non-terminal attempts.
- **Fire on content, not counter.** Both feeds (sibling briefs and the prior-attempt digest) are injected whenever the harness-built text is non-empty, replacing the `attempt > 1` guard. This is self-correcting via the existing since-last-reset cut: a brand-new task has no streams (empty → base prompt, unchanged), a just-reopened task has its priors dropped by the RESET cut (empty → base prompt, unchanged), and only a resumed interrupted/quota-paused task — an in-scope stream with no RESET written — newly gets the digest at attempt 1.
- **Admit interrupted and quota-paused outcomes.** The digest filter grows from `{Failed, TimedOut}` to `{Failed, TimedOut, Interrupted, QuotaPaused}`; `Completed` stays excluded. Interrupt and quota-pause are indistinguishable to the resuming agent (in-flight work, deliberately stopped, changes in the tree), so they share one new "resume" lesson: *this attempt was cut off mid-flight, not a failure — your checkout already holds its partial changes, so continue from where it stopped.* The lesson explicitly points the agent at its own dirty working tree as the source of truth; the tail-12 narrative is orientation, the diff is authoritative.

## Considered options

- **A separate "resume brief" feed, distinct from the digest.** Rejected. It would duplicate the stream-parsing, narrative rendering, and since-last-reset scoping the digest already owns for a marginal semantic distinction the per-attempt lesson line already carries.
- **Keep the loop-counter guard and add a resume flag.** Rejected. The since-last-reset cut already distinguishes fresh from resumed; a separate "am I resuming?" flag is redundant state that can drift from the streams on disk, which are the real record.
- **Rely on the checkout diff alone (no narrative for resumes).** Rejected as insufficient but respected as the fallback: the diff tells the agent *what got done*, the tail narrative stops it misreading half-finished code as someone else's mess. When no stream exists (a crashed Drain, not a graceful interrupt), the diff-only path is exactly what happens, and that degradation is acceptable.

## Consequences

- ADR-0040's "carry-forward fires only on a real retry, never on attempt 1" clause is superseded for the resume case; its failure-lesson reasoning stands unchanged for the intra-drain retry it was written for.
- The persisted interrupted/quota-paused **Captured attempt stream** becomes load-bearing, not just telemetry. The **Interrupted task** and **Agent quota pause** glossary entries are corrected to state that the attempt stream is persisted and that a resume inherits it, so the interrupt/pause persist path is not later "simplified" away.
- ADR-0020's invariant holds: the resuming agent still receives only a harness-built digest, never a pointer to a raw stream.
- pop's automatic quota pause gains the same resume-context benefit as a manual interrupt for free, since both flow through the same filter and lesson.
