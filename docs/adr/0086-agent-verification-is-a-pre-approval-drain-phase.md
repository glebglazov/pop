---
status: accepted
---

# Agent verification is a pre-approval drain phase that produces remediation tasks

Once a Drain exhausts a Task set's open AFK work, an independent **Verifier** agent judges the whole set — acceptance criteria (authoritative), task bodies, the accumulated diff, and the optional `prd.md` (enrichment; PRD-less sets verify fully) — before any human is asked to approve. This makes "all AFK done" derive **NEEDS-VERIFY**, not immediately Done: a set reaches Done (pure-AFK) or AWAITING-APPROVAL (has a terminal HITL task) only with a **PASS** verdict at the current work SHA. Verification is **on by default** for any set with AFK work (`[tasks.verify].enabled`, opt out per set with `"verify": false`).

The verdict is three-way. **PASS** advances the set. **FIXABLE** makes the Verifier a *task producer*: it writes a new AFK **Remediation task** (markdown + atomic `index.json` entry) whose body is the findings, which Drain then picks up normally; the verify→remediate→re-verify loop is bounded by a per-set **remediation depth cap** (`max_remediation_depth`), after which the set parks. **NEEDS-HUMAN** (or an exhausted cap) parks the set at VERIFY-FAILED with the findings.

## Why

The Completion sentinel is the implementing agent grading its own homework — a zero exit plus self-checked boxes. That is not an independent signal. A separate Verifier, run in a fresh context and chosen independently of the implementers, catches drift before it wastes human attention, and — because findings become a real AFK task rather than annotations on a spec — remediation rides the existing Drain loop with no new execution path. Findings are attempt-scoped ("the work at SHA X fails criterion Y"), so they belong in a Remediation task's body, never inside the task specs (which are stable, task-scoped intent); mixing them would stack stale complaints and blur what is authoritative.

Verifier selection reuses the implement machinery: an ordered fallback list (`[tasks.verify].agents`, CLI `--verify-agent`, else `default_agents`) at a pinned effort (default `heavy`, CLI `--verify-effort`), falling through on an Agent quota pause or missing binary — so any configured agent can verify at heavy effort when the primary is unavailable. A standalone `pop tasks verify <set>` (force, ignoring the SHA cache) and a **Re-verify** option in the HITL gate prompt (ADR-0012) let a human re-check after inline changes without kicking off a fresh drain.

## Considered Options

- **Advisory-only verification (never blocks).** Rejected: it annotates but doesn't save human attention — the human still checks everything.
- **Reopen the offending tasks and inject findings into the re-attempt prompt.** Rejected in favour of producing a Remediation task: reopen can't express cross-cutting findings that map to no single task, needs new findings-store plumbing, and re-runs the original vague spec instead of pointing at the actual produced code.
- **Verify per task on completion.** Rejected as the primary model: a single task's slice is too narrow for whole-feature/integration judgment. Whole-set verification, cached by SHA, is the gate.
- **Reuse the implementing agent (fresh context) as the verifier.** Rejected as the default: the same model family shares blind spots and may bless its own mistakes. Independence is the point; a distinct fallback list is cheap.
- **Unbounded remediation.** Rejected: a verifier that never fully passes would drain agent quota forever with no human off-ramp. Hence the depth cap → park.

## Consequences

- Task set status derivation gains a new input — the **Verify verdict**, persisted in the Drain store and SHA-gated. It is a cache, not a completion flag: when the work SHA moves the verdict is stale and the set returns to needing verification, so "artifacts drive status" still holds in spirit.
- The Verifier **mutates the manifest mid-drain** (creates Remediation tasks). This is a new authoring power beyond to-tasks; it requires the atomic manifest write and next-number allocation.
- VERIFY-FAILED is a new parked disposition for the Queue daemon (no eligible AFK work), alongside blocked and awaiting_approval.
- New durable Drain outcomes ship — see ADR-0087.
