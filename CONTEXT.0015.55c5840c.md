---
fragment: 55c5840c
generation: 0015
branch: master
---

+ Agent verification
  An independent verifier agent's judgment of a Task set's completed AFK work — acceptance criteria (authoritative) plus task bodies, the accumulated diff, and the optional co-located prd.md — run before any human approval. It is gated by user config and off by default (runs only when [tasks.verify].enabled is true); when enabled it fires as the tail of a Drain once no open AFK work remains, and its verdict is cached in the Drain store keyed by the work SHA, so a change to the work re-triggers it. Distinct from the Completion sentinel, which is the implementing agent self-reporting its own success; Agent verification is a second agent confirming reality.
  avoid: review, QA, human verification, Completion sentinel
  under: Task execution

+ Verifier
  The agent that performs Agent verification, resolved from an ordered fallback list (config [tasks.verify].agents, CLI --verify-agent, or default_agents) at a pinned effort (default heavy) — falling through to the next agent on an Agent quota pause or missing binary, exactly like the implement quota fallback. It runs in a fresh context and is chosen independently of the implementing agents so it does not grade its own work.
  avoid: reviewer, checker, judge agent
  under: Task execution

+ Verify verdict
  The cached result of Agent verification for a Task set at a specific work SHA, held in the Drain store: PASS (proceed to approval or Done), FIXABLE (findings an agent can resolve), or NEEDS-HUMAN (only a human can resolve). A verdict is stale once the work SHA moves, which returns the set to needing verification.
  avoid: verify result, verification status
  under: Task execution

+ Remediation task
  An AFK task the Verifier writes into a set (a new markdown file plus an atomic index.json entry) when Agent verification returns FIXABLE, whose body is the findings to resolve. Drain picks it up like any eligible AFK task; the verify→remediate→re-verify loop is bounded by a per-set remediation depth cap (config max_remediation_depth), after which the set parks at VERIFY-FAILED. Findings live only as a Remediation task's body — never as annotations inside another task's spec.
  avoid: fix task, verification findings file, verify note
  under: Task set

+ Awaiting-approval Task set
  A Task set whose AFK work is Agent-verified (PASS) and whose only remaining open work is a human's terminal approval (a HITL task). It derives status AWAITING-APPROVAL and an `awaiting_approval` Drain outcome — the post-agent, pre-human end of the HITL lifecycle, where the human signs off rather than "verifies" (the agent already did). Replaces the retired Unverified Task set.
  avoid: Unverified Task set, pending verification, review state, Blocked Task set

+ Verify-failed Task set
  A Task set that Agent verification could not clear on its own: the Verifier returned NEEDS-HUMAN, or the remediation depth cap was exhausted. It derives status VERIFY-FAILED and a `verify_failed` Drain outcome, carries the findings, and parks (no eligible AFK work), so the Queue daemon leaves it for a human to reopen, edit, re-verify, or approve anyway.
  avoid: failed verification, blocked, rejected

~ Task set status
  The status derived from a discovered Task set whenever a tasks command runs, from the manifest plus — when Agent verification is enabled — the SHA-gated Verify verdict. A Ready set has an eligible task; a Done set has only done tasks and, when verification is enabled, a PASS verdict at the current SHA; a Failed set has a failed task. When no AFK task is eligible and the set is not Done, Failed, or Deferred: it is Awaiting-approval when only a human approval task is left (Agent-verified if verification is enabled), Verify-failed when verification could not clear it, and Blocked when an open AFK task is still gated behind a human task. With verification enabled, a set with no fresh verdict is pending verification (VERIFYING while the Verifier runs); with verification off, status derives from the manifest alone, as before.
  was: The status derived from a discovered Task set whenever a tasks command runs. A **Ready** Task set has at least one eligible task; a **Done** Task set has only done tasks; a **Failed** Task set has at least one failed task. When no AFK task is eligible and the set is neither Done, Failed, nor Deferred, the disposition splits by whether agent work remains: an **Unverified** Task set has no open AFK task left — only human verification (HITL) stands before Done; a **Blocked** Task set still has an open AFK task gated behind a human-in-the-loop task. Pop does not persist a separate completion flag, so artifact changes naturally affect the next derived status.

~ Drain outcome
  How a Drain's process ended (its exit reason) versus the set's work disposition. Work disposition — done, failed, blocked, awaiting_approval, verify_failed, deferred — is read from the manifest-derived Task set status (now also gated on the Verify verdict), never restated on the Drain. `awaiting_approval` and `verify_failed` are appended to the durable, append-only outcome journal; the legacy `unverified` value is retained on disk and read forward as `awaiting_approval`. Process exit reasons stay finished / quota-paused / interrupted / crashed.
  was: How a **Drain**'s process ended — its exit reason, not the set's work disposition: finished (the drain ran to its own stopping point), quota-paused (an agent preset hit quota), interrupted (deliberate SIGINT teardown), or crashed (the process died unexpectedly, recorded by reconciliation rather than by the drain itself). The set's resulting work disposition — done, failed, blocked, unverified, deferred — is read from the manifest-derived **Task set status**, never restated on the Drain.

~ Task set
  The local `<id>/index.json` manifest and its sibling task markdown files beneath the Task storage `tasks/` directory, optionally alongside a co-located `prd.md` (the set's whole context in one folder; PRD-less sets are normal). A Task set is the schedulable unit. Its directory name is its canonical identifier and display label; there is no separate Task-set title. PRD existence remains irrelevant to task scheduling and execution — prd.md is optional enrichment the Verifier may read, never a required input.
  was: The local `<id>/index.json` manifest and its sibling task markdown files beneath the **Task storage** `tasks/` directory. A Task set is the schedulable unit. Its directory name is its canonical identifier and display label; there is no separate Task-set title. It may be created from a PRD, a grilling session, or another planning workflow; PRD existence is irrelevant to task scheduling and execution.

- Unverified Task set
