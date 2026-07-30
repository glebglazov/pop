---
status: accepted
---

# Remediation self-reports are set-wide context for later attempts and the next Verifier

## Context

A **Remediation task** ends like any AFK attempt: a **Completion sentinel** with a
`SUMMARY_START`/`SUMMARY_END` block, recorded as a `DONE` record in the set's
`progress.txt`. Today that summary reaches exactly one consumer —
`completedAFKProgress` folds it into the **HITL assistance prompt** for the
assisting agent (`tasks/prompt.go:362`). Three surfaces that need it do not get it:

- The **next remediation attempt.** A second remediation's prompt carries only its
  own task file plus, on retry, the task-scoped **prior-attempt digest**
  ([ADR-0040](0040-afk-retries-carry-a-failure-typed-prior-attempt-digest.md)),
  which excludes completed attempts outright. So remediation cycle 2 re-treads
  cycle 1 blind — the cheapest way to burn the **Remediation depth** cap.
- The **next Verifier run.** `buildVerifierPrompt` passes done AFK task bodies and
  the accumulated diff, no progress summaries, framed "confirm reality, not to
  trust its self-report"
  ([ADR-0102](0102-verifier-judges-only-done-afk-work-and-runs-before-the-terminal-hitl-gate.md)).
- The **human at a gate.** The on-screen **HITL gate prompt** and **Verify-fail
  gate prompt** print the gate task body and a menu, nothing about what remediation
  did. The assisting agent saw more than the person signing off did.

## Decision

- A **Remediation history block** — every done Remediation task in the set, title
  plus its capped sentinel summary — is injected into **every later AFK task
  attempt in that set**, not just remediation attempts. Findings are set-wide
  quality signals against criteria that will be judged again, and a reopened task
  should know where the **Verifier** already caught the set.
- The same block goes into the **Verifier prompt**, framed exactly like the
  existing `## Prior human note (context only — a real regression here still
  fails)` section: always present when remediations exist, labelled as the
  implementer's unverified claims, with an explicit "the diff remains
  authoritative; do not accept a claim you cannot see in the diff". Scope is every
  remediation in the set, not just the current verify episode.
- The human's counterpart is the **Remediation review block**: the same content
  printed on-screen at the HITL gate and the Verify-fail gate, scoped to
  remediation work alone so it never buries the gate task body.
- Each injected summary is capped (~10 lines / ~600 chars); the full narrative
  stays reachable through `pop tasks stream`.

## Considered Options

- **Keep self-reports out of the Verifier** (the ADR-0102 reading) — rejected. The
  Verifier run after a remediation is a separate process judging a new work SHA,
  not the implementer grading itself, and the diff stays authoritative. The
  residual risk is priming: naming a claim makes a shallow confirmation likelier.
  The claims framing is the mitigation, and it is the same device already trusted
  for the prior human note.
- **Episode-scoped history for the Verifier** (only remediations since the last
  PASS) — rejected: a smaller prompt, but it hides the set's repair history from
  the judge with no decision attached to the cut.
- **Remediation-only injection** (later ordinary AFK tasks see nothing) — rejected.
  It preserves task self-containment, but the cost of a task reintroducing a defect
  the Verifier already flagged outweighs the context spend, given the cap.
- **Reuse the prior-attempt digest machinery** — rejected: that digest is
  deliberately one task's own failed attempts. This is other tasks' *successful*
  ones. Same shape, opposite scope; fusing them would blur ADR-0040's rule.

## Consequences

- This is pop's first cross-task prompt channel. Every other history an attempt
  sees is scoped to that attempt's own task, so the block must read as history,
  never as instructions, or an unrelated task will "fix" a marker bug it was never
  asked to touch.
- ADR-0102's self-report stance is narrowed, not reversed: the Verifier's *verdict
  scope* is still done AFK work judged against the diff. Only its context widens.
- Prompt size grows with remediation count in long-lived sets; the per-summary cap
  bounds each entry but not the total, so a set that remediates many times pays for
  it in every later attempt.
