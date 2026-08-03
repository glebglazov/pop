---
status: accepted
---

# Human completion and verification are two axes, and the human's outranks the verdict

Whether a **Task set**'s work is finished and whether a **Verifier** has judged it are two
independent facts. Pop stops competing them for one status slot:

- A set that reached terminal on its own — an agent drained it, nobody intervened —
  behaves exactly as [ADR-0086](0086-agent-verification-is-a-pre-approval-drain-phase.md)
  and [ADR-0096](0096-pass-verdict-immunizes-terminal-status-against-sha-drift.md)
  specified. No verdict is **NEEDS-VERIFY**; a non-PASS verdict at HEAD is
  **VERIFY-FAILED**. Unchanged.
- A set a human's own `pop tasks complete` carried terminal reads **DONE** (or
  **AWAITING-APPROVAL**), and the verification outcome rides beside it as a
  **Verification mark**: `unverified`, `verified` at a SHA, or `verify-failed`. The
  verdict never demotes the status. **VERIFY-FAILED** in particular becomes a mark on
  such a set rather than the status.

**Human completion** is a recorded bit, `human_completed` in the set's `index.json`. It is
written at the **Task transition** chokepoint when a human `→done` edge is what leaves the
set terminal, and cleared by the manifest writer whenever the derived status leaves the
terminal zone. It does *not* clear on later commits.

The **Verification mark** is resolved by **Verified status resolution** together with the
status — one call answering two fields — so every surface (`pop tasks status`, the **Work
dashboard**, `pop work status`, the daemon scan, the pre-approval **Drain** phase) reads
both from the same place and none re-derives either.

## Why

This set was 34/34 done and read `NEEDS-VERIFY`. Manually completing its last task
reported success and changed nothing visible: the read-side resolution re-derived every
terminal row, and a terminal set with no PASS at HEAD regressed straight back to
`NEEDS-VERIFY`. The human was told the completion applied and then shown a status saying
it had not.

The bug was not in the gate's mechanics but in its shape. One slot was carrying two
answers, so the newer, weaker fact (nobody has run a Verifier yet) was overwriting the
older, stronger one (a person looked at this and said it is done). Verification must stay
*visible* — "done and nobody checked" is a different situation from "done and checked" —
but it must not be able to contradict the assertion.

## Considered Options

- **Make the human's `complete` record a PASS verdict.** Rejected: it lies about who
  judged the work. `pop tasks verify --accept` already exists for "I reviewed the findings
  and they are non-blocking" (**Accepted verdict**) and is deliberately a different act
  from "this work is finished". Folding the two would erase the distinction and would make
  the assertion expire on SHA drift, which is exactly wrong.
- **Store the bit as a store row keyed like a verdict.** Rejected: a **Verify verdict** is
  keyed by `(repo, set, work SHA)` because a Verifier's PASS is a claim about a *tree* and
  expires when the branch moves (ADR-0096). "I am okay with this" is a claim about the
  *set's work*, needs no SHA, and must survive later commits. Keying it by SHA would
  reintroduce the demotion through the back door on the first unrelated commit.
- **Add a `DONE-UNVERIFIED` status.** Rejected: it multiplies the status vocabulary by the
  verification vocabulary. Every consumer that switches on status — ordering, styling, the
  fold gate, dispatch — would grow a parallel arm, and the next independent fact would
  double it again.
- **Suppress verification entirely for human-completed sets.** Rejected: it turns an
  ergonomic fix into a hole in the gate. The **Verify verb**'s eligibility and the drain's
  verify phase both still run for such a set; verification is deferred to a mark, never
  skipped.

## Consequences

- The `Kind` seam's `Container` and the Task-set `Row` each carry a `VerifyMark` cell
  beside `RawStatus`. The **Verified-at SHA** badge became four-state (`verify-failed`
  joins) and is now a display projection of the mark rather than a second reading of the
  status.
- Both **Verify verb** eligibility tests (Work dashboard menu, Task-set kind actions) key
  on the mark instead of on `NEEDS-VERIFY` / `VERIFY-FAILED`, so a human-completed set that
  still owes a verdict is offered verification.
- The drain's pre-approval verify phase still runs the Verifier and still records and
  prints the verdict on a human-completed set, but a non-PASS neither parks it nor spawns a
  **Remediation task** — spawning fix work would reopen work the human closed and erase the
  bit.
- The fold gate opens: it refuses `NEEDS-VERIFY`, and a human-completed set reads `DONE`,
  so it reaches the fold rather than sitting behind a verdict.
- A human *skip* does not set the bit. The bit records completion, not disposal; a skip's
  meaning ("this task is not needed") is not an assertion that the set's work is done.
- An unreadable `human_completed` value reads as absent rather than MALFORMED — the key is
  hand-editable, and the fail-safe direction is off, where verification still gates the
  status.
