---
status: accepted
---

# Verification publishes a Verify report

## Context

Refine publishes a **Refine report** and verification publishes nothing
readable. The Verifier does produce prose — `verifier.tmpl.md` asks for
`SUMMARY:` and `FINDINGS:` — but it lands only in the `verify_verdicts` row, on
stdout at verify time, in two agent prompts, and in a spawned Remediation task's
body. Two consequences make "verification failed, why?" hard to answer:

- Every human-facing surface truncates the reasons to their first line
  (`firstFindingsLine`, `tasks/render.go`), so nine lines out of ten are
  unreadable anywhere.
- The prompt says `leave empty for PASS`, so the verdict a reader most wants to
  interrogate records nothing at all.
- **Verification invalidation** is a hard `DELETE` of the set's verdict rows and
  fires *on remediation spawn* — the moment findings become actionable, the full
  text is on a countdown. The only survivor is a **Captured verify run**, a
  gzipped agent stream: an audit trail, not something a human reads.

The asymmetry was principled rather than accidental. Refine has no verdict, so
its prose *is* its output; verification's output is an enum pop gates on, and
its prose was designed as justification feeding the remediation loop, not as a
standing record. That reasoning no longer covers the human who wants to know why.

## Decision

Agent verification publishes a **Verify report**, on the mechanism Refine
already proved and under terms of its own:

1. **The Verifier authors it.** The prose remainder of its reply becomes the
   report; the `VERDICT:` line stays first and is lifted off the front, the way
   `splitRefinerReply` lifts `COMMIT-SUBJECT:`. The agent commits to the enum
   before it starts justifying it. Rendering a document from the cached row
   instead was rejected: it can only restate what `FINDINGS:` already carries,
   which is the thing that is missing.
2. **Written on every verdict, PASS included**, and the PASS contract changes
   from "leave empty" to stating what was checked and why each criterion is met.
   This is the one place the extra output tokens are worth buying.
3. **One document per invocation**, timestamped under the set's directory,
   latest by timestamp — so a remediation lap leaves the whole lap-by-lap trail.
4. **It outlives the cache.** `verify_verdicts` stays a cache and stays deleted
   on invalidation; the reports are the durable audit trail that cache never
   was. This divergence is the point, not an oversight.
5. **The Verifier never reads its own prior reports.** See below.
6. **An Accepted verdict gets a pop-rendered report** — human-authored, carrying
   the ADR-0103 `Note` and the verdict it overrode. No agent runs on that path,
   so there is no reply to split, and it is the case a future reader is most
   likely to find puzzling.
7. **Surfaced where humans read**, not in agent prompts: the HITL gate preamble,
   the paging entry, the detail view, and `Artifacts()` at a tier above refine.
   An agent told to fix something already has the findings in its task body; a
   second copy invites it to treat the report as the spec.
8. **No new toggle.** It is part of what verification is, and `[work.verify]`
   already gates it.

## Considered options

**Feeding the Verifier its previous report**, as the Refiner is fed its own, was
the closest call. The prior art points both ways: the Refiner reads its previous
report and carries forward what is still true, while ADR-0103 feeds a *human's*
override note into later Verifier prompts only "without suppressing a fresh
judgment" — pop was already careful there.

Rejected, because the two roles differ in what their output does. Refine gates
nothing, so carrying prose forward is document maintenance. The verdict gates a
drain, and the Verifier's independence — "chosen independently of the
implementing agents so it does not grade its own work" — is load-bearing exactly
there. A prior FIXABLE handed to the next Verifier anchors the enum, and the
verdict-line-first ordering does not help, since the agent reads the whole
prompt before answering. The forward-carry already happens through the right
channel: the Remediation task body carries findings to the *implementer*, and
the re-verify judges the fixed tree fresh.

**One unified "pass report" concept** covering both Refine and Verify was also
rejected. The document mechanics are near-identical and should be factored into
one shared shape — header stamp, timestamped-directory scan, pointer, artifact
tier — but the roles are not the same, and one asymmetry proves it: verification
already owns the four-state **Verified-at SHA** badge, which the glossary calls
"a display projection of the **Verification mark**, never a second derivation of
it." A Verify report carrying its own staleness verdict would be that second
derivation. The report answers *why*; whether the judgment still holds at HEAD
stays the badge's question.

## Consequences

Reports accumulate on disk per invocation with no pruning, as refine's already
do. Every verification, including passes, costs more output tokens. The
`verify_verdicts` cache and the report history can now disagree about how many
verdicts a set has had — deliberately, since one is a cache keyed by work SHA and
the other is a log.
