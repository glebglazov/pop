---
fragment: 7B3C9E41
generation: 0042
branch: master
---

+ Verify report
  The document **Agent verification** publishes for a **Task set**: what the
  **Verifier** checked, and why each acceptance criterion is met or unmet
  (ADR-0245). The Verifier authors it as the prose remainder of its reply — the
  machine-read `VERDICT:` line comes first and is lifted off the front, so the
  agent commits to the enum before it begins justifying it. One document per
  Verifier invocation, timestamped under the set's directory, latest by
  timestamp; a remediation lap therefore leaves the whole lap-by-lap trail
  rather than one rewritten answer. Written on *every* verdict, PASS included —
  a PASS that records what it checked is the case the old empty-findings
  contract served worst. Unlike the **Verify verdict**, it survives
  **Verification invalidation**: the cache is deleted on remediation spawn while
  the report is the durable audit trail that cache never was. An **Accepted
  verdict** gets one too, rendered by pop rather than an agent, carrying the
  human's note and the verdict it overrode. It answers *why*, never *whether* —
  staleness stays the **Verified-at SHA** badge's question, and the report must
  not offer a second answer to it. Its mechanism is the **Refine report**'s,
  factored; its role is not, which is why the two stay separate terms.
  avoid: verify artifact, verdict document, findings file, verification log
  under: Verification

+ Verify pointer
  The **Verify report** as every surface other than a reader carries it: its
  path and the commit it was written against, never a line of what it says —
  the **Refine pointer**'s shape, sharing its resolution. It renders only where
  a human reads: the **HITL gate prompt** preamble, the paging entry, the **Task
  set detail view**, and the set's **Task artifact** list, ranked above the
  Refine report because a verdict outranks a polish note. Deliberately absent
  from the agent-facing prompt views the Refine pointer rides: an agent sent to
  fix something already has the findings in its task body, and a second copy
  invites it to treat the report as the spec.
  avoid: verify link, latest verification, verdict pointer
  under: Verification

~ Verify verdict
  The cached result of **Agent verification** for a **Task set**, held in the
  **Drain** store: PASS (proceed to approval or Done), FIXABLE (findings an
  agent can resolve), or NEEDS-HUMAN (only a human can resolve). Rows are keyed
  by `(repo, set, work_sha)`; a PASS in the current **verification episode**
  immunizes terminal status against later commits — HEAD moving past the
  verified SHA does not regress DONE or AWAITING-APPROVAL, only surfaces
  **Verified-at SHA**. Leaving the terminal zone (**Verification
  invalidation**) clears the cache so a new episode needs fresh verification. It
  is a cache and nothing more: the reasons behind it live in the **Verify
  report**, which outlives invalidation, and the raw stream in the **Captured
  verify run**. The row's `findings` therefore carry the remediation loop, not
  the human — every status surface truncates them to their first line.
  avoid: verify result, verification status
  was: The cached result of **Agent verification** for a **Task set**, held in the **Drain** store: PASS (proceed to approval or Done), FIXABLE (findings an agent can resolve), or NEEDS-HUMAN (only a human can resolve). Rows are keyed by `(repo, set, work_sha)`; a PASS in the current **verification episode** immunizes terminal status against later commits — HEAD moving past the verified SHA does not regress DONE or AWAITING-APPROVAL, only surfaces **Verified-at SHA**. Leaving the terminal zone (**Verification invalidation**) clears the cache so a new episode needs fresh verification. Distinct from the **Captured verify run** audit trail.
