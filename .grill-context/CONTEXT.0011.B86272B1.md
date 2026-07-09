---
fragment: B86272B1
generation: 0011
branch: master
---

~ Agent verification
  An independent **Verifier** agent's judgment of a **Task set**'s completed AFK
  work. Its verdict scope is only the set's `done` AFK tasks — the prompt carries
  their bodies and acceptance criteria, the accumulated diff, and the optional
  co-located `prd.md`; open/not-`done` AFK tasks and HITL tasks (any status) are
  excluded so the Verifier never fails a set on work it isn't equipped to judge
  (a not-yet-run HITL sign-off is not an unmet criterion). Gated by user config,
  off by default. When enabled it fires as the tail of a **Drain**: on a DONE set,
  and on an **Awaiting-approval Task set** it runs *before* the terminal HITL
  sign-off gate — a PASS then opens that gate, so cheap agent checking precedes
  expensive human time.
  was: An independent Verifier agent's judgment of a Task set's completed AFK work — acceptance criteria (authoritative) plus task bodies, the accumulated diff, and the optional co-located prd.md — run before any human approval. It fires as the tail of a Drain once no open AFK work remains, judging every task in the set.

~ Verify verdict
  The cached result of **Agent verification** for a **Task set** at a specific
  work SHA, held in the **Drain** store: PASS, FIXABLE, or NEEDS-HUMAN. A verdict
  lapses two ways: the work SHA moves (stale), or the set's scope grows — adding
  a new AFK task to an already-verified set invalidates its cached verdicts, so
  the enlarged set is re-verified rather than coasting on a PASS earned before the
  work existed. A PASS may be Verifier-authored or human-authored (an **Accepted
  verdict**); both are the same PASS row and obey the same lapse rules.
  was: The cached result of Agent verification for a Task set at a specific work SHA, held in the Drain store: PASS, FIXABLE, or NEEDS-HUMAN. A verdict is stale once the work SHA moves, which returns the set to needing verification.

+ Accepted verdict
  A human-authored PASS: a person reviewed a non-PASS **Verify verdict**'s
  findings, judged them non-blocking, and overrode the set to verified. Stored as
  an ordinary PASS row (flagged human-authored, carrying the human's note), so it
  reuses PASS idempotency and the scope-growth invalidation with no change to
  **Verified status resolution**. The note feeds forward as *context* into later
  **Verifier** prompts — informing the Verifier of a known non-issue so it isn't
  re-flagged — but never suppresses a fresh judgment, so a later real regression
  at that spot can still fail.
  avoid: override table, verdict override, dismiss, waiver
  under: (verify)

~ Remediation task
  An AFK task written into a set whose body is verify findings to resolve; **Drain**
  picks it up like any eligible AFK task, bounded by a per-set remediation depth
  cap. Spawned two ways: automatically when **Agent verification** returns FIXABLE
  under cap, or by a human — who may spawn one carrying their own note from a
  NEEDS-HUMAN verdict, or when the auto-cap is exhausted, to fix a defect the auto
  path won't. Spawning invalidates the set's cached verdicts. Findings live only as
  a Remediation task's body — never as annotations inside another task's spec.
  was: An AFK task the Verifier writes into a set when Agent verification returns FIXABLE, whose body is the findings to resolve. Drain picks it up like any eligible AFK task; the loop is bounded by a per-set remediation depth cap, after which the set parks at VERIFY-FAILED.

~ Verify-failed Task set
  A **Task set** that **Agent verification** could not clear on its own: the
  **Verifier** returned NEEDS-HUMAN, or the **Remediation task** depth cap was
  exhausted. It derives status VERIFY-FAILED, carries the findings, and parks. A
  human dispositions it two ways: **Accept** (record an **Accepted verdict** — the
  set stands verified) or **Remediate** (spawn a **Remediation task** with a note).
  Reopen/edit/re-verify remain available.
  was: A Task set that Agent verification could not clear on its own: the Verifier returned NEEDS-HUMAN, or the Remediation task depth cap was exhausted. It derives status VERIFY-FAILED, carries the findings, and parks, so the Queue daemon leaves it for a human to reopen, edit, re-verify, or approve anyway.

+ Verify-fail gate prompt
  The interactive choice shown when a **Drain** reaches a **Verify-failed Task
  set** on a TTY — the verify counterpart of the **HITL gate prompt** and **Failed
  gate prompt**. It offers Accept (record an **Accepted verdict** with a note),
  Remediate (spawn a **Remediation task** with a note), open a **Runtime shell**,
  or exit; `0` is exit. Headless runs use `pop tasks verify <set> --accept` /
  `--remediate "<note>"` instead. Re-verify is not offered here — re-running the
  Verifier is a separate force action, not a response to findings.
  avoid: verdict prompt, review prompt
  under: (verify)

~ HITL gate prompt
  An interactive choice shown when implement reaches a **Human-blocked Task set**,
  an **Awaiting-approval Task set**, or when a ready HITL task is targeted directly
  (`pop tasks implement <task-set>/<hitl>.md` routes to that task's gate rather
  than rejecting it as non-AFK). It defaults to agent assistance while letting the
  human complete the task, defer it, open a **Runtime shell**, re-verify, or exit;
  `0` is exit. After complete or defer clears the blocking HITL task, implement
  refreshes the set and continues from any newly eligible AFK task. Stays
  interactive in a drain pane with a TTY; `--yes` skips it.
  was: An interactive choice shown when implement reaches or selects a Human-blocked Task set. It defaults to getting agent assistance while still letting the human complete the task, defer it, open a Runtime shell, or exit; after complete or defer clears the blocking HITL task, implement continues. Reached only when the drain selects the set, not by directly targeting a HITL task.
