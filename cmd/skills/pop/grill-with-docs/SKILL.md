---
name: grill-with-docs
description: Grilling session that challenges your plan against the existing domain model, sharpens terminology, and updates documentation (CONTEXT.md, ADRs) inline as decisions crystallise — conflict-free under parallel agents and teams via generation-numbered glossary fragments and sequential-id ADRs. Use when user wants to stress-test a plan against their project's language and documented decisions.
---

<!--
base: mattpocock/skills grilling@391a2701 + domain-modeling@391a2701

This file is a marked overlay. Everything from here down to the "POP OVERLAY"
marker is a verbatim copy of two upstream skills at the pinned refs above,
concatenated: first the body of grilling/SKILL.md (the interview primitive),
then the body of domain-modeling/SKILL.md (the glossary/ADR discipline). Pop
inlines both rather than delegating to `/grilling` + `/domain-modeling`, per
ADR-0009 (skills are embedded in the binary and ship to machines without
Matt's skills installed). Pop's parallel-safety additions — the single-writer
override, grill-consolidate, and the commit-on-close discipline — live below
that marker. To review upstream drift, diff the region between this header and
the marker against grilling@<newref> + domain-modeling@<newref>.
-->

Interview me relentlessly about every aspect of this plan until we reach a shared understanding. Walk down each branch of the design tree, resolving dependencies between decisions one-by-one. For each question, provide your recommended answer.

Ask the questions one at a time, waiting for feedback on each question before continuing. Asking multiple questions at once is bewildering.

If a *fact* can be found by exploring the codebase, look it up rather than asking me. The *decisions*, though, are mine — put each one to me and wait for my answer.

Do not enact the plan until I confirm we have reached a shared understanding.
# Domain Modeling

Actively build and sharpen the project's domain model as you design. This is the *active* discipline — challenging terms, inventing edge-case scenarios, and writing the glossary and decisions down the moment they crystallise. (Merely *reading* `CONTEXT.md` for vocabulary is not this skill — that's a one-line habit any skill can do. This skill is for when you're changing the model, not just consuming it.)

## File structure

Most repos have a single context:

```
/
├── CONTEXT.md
├── docs/
│   └── adr/
│       ├── 0001-event-sourced-orders.md
│       └── 0002-postgres-for-write-model.md
└── src/
```

If a `CONTEXT-MAP.md` exists at the root, the repo has multiple contexts. The map points to where each one lives:

```
/
├── CONTEXT-MAP.md
├── docs/
│   └── adr/                          ← system-wide decisions
├── src/
│   ├── ordering/
│   │   ├── CONTEXT.md
│   │   └── docs/adr/                 ← context-specific decisions
│   └── billing/
│       ├── CONTEXT.md
│       └── docs/adr/
```

Create files lazily — only when you have something to write. If no `CONTEXT.md` exists, create one when the first term is resolved. If no `docs/adr/` exists, create it when the first ADR is needed.

## During the session

### Challenge against the glossary

When the user uses a term that conflicts with the existing language in `CONTEXT.md`, call it out immediately. "Your glossary defines 'cancellation' as X, but you seem to mean Y — which is it?"

### Sharpen fuzzy language

When the user uses vague or overloaded terms, propose a precise canonical term. "You're saying 'account' — do you mean the Customer or the User? Those are different things."

### Discuss concrete scenarios

When domain relationships are being discussed, stress-test them with specific scenarios. Invent scenarios that probe edge cases and force the user to be precise about the boundaries between concepts.

### Cross-reference with code

When the user states how something works, check whether the code agrees. If you find a contradiction, surface it: "Your code cancels entire Orders, but you just said partial cancellation is possible — which is right?"

### Update CONTEXT.md inline

When a term is resolved, update `CONTEXT.md` right there. Don't batch these up — capture them as they happen. Use the format in [CONTEXT-FORMAT.md](./CONTEXT-FORMAT.md).

`CONTEXT.md` should be totally devoid of implementation details. Do not treat `CONTEXT.md` as a spec, a scratch pad, or a repository for implementation decisions. It is a glossary and nothing else.

### Offer ADRs sparingly

Only offer to create an ADR when all three are true:

1. **Hard to reverse** — the cost of changing your mind later is meaningful
2. **Surprising without context** — a future reader will wonder "why did they do it this way?"
3. **The result of a real trade-off** — there were genuine alternatives and you picked one for specific reasons

If any of the three is missing, skip the ADR. Use the format in [ADR-FORMAT.md](./ADR-FORMAT.md).
<!-- ═══════════════════════════════ POP OVERLAY ═══════════════════════════════
Everything below is pop-specific and has no upstream twin. It carries one
behavioural override of the base — negating domain-modeling's "Update
CONTEXT.md inline" single-writer instruction in favour of per-session
fragments — plus the grill-consolidate fold-in path and the commit-on-close
discipline that make grilling safe under parallel agents and teams.
-->

## Single-writer override

**Override (negates the "Update CONTEXT.md inline" section above): never write the base `CONTEXT.md` — write a delta op to your own per-session fragment per [CONTEXT-FORMAT.md](./CONTEXT-FORMAT.md), and treat the glossary you challenge terms against as the union of base + fragments.** Read [CONTEXT-FORMAT.md](./CONTEXT-FORMAT.md) before your first write. This keeps concurrent sessions and teams conflict-free; the "update `CONTEXT.md` right there" wording upstream is the one place pop deviates, and this line is authoritative.

If the user asks you to **consolidate** (fold accumulated fragments into the base), use the `grill-consolidate` skill. Consolidation is a separate single-writer maintenance pass, not part of the grilling session — don't fold fragments in mid-grill.

## Closing the session

Once you've proposed the final glossary updates and any ADRs, and the user signals the design is settled (or asks to wrap up), **commit the artifacts this session produced automatically** — don't ask first. Committing is always desired at the close, so just do it and report what was committed. Do this once, at the natural close — don't commit mid-grill or after every individual fragment.

Why this matters: these artifacts often get carried into downstream work via a fresh git worktree forked from the current branch's HEAD (for example when `to-tasks` later turns the plan into work items). Anything not committed to HEAD is left behind. The session that produced the artifacts is the right place to commit them, so don't defer this to a later skill.

To commit:

1. **Skip if nothing to do.** If the working directory is not a git repository, or this session created/modified no committable repository files, say so and skip.
2. **Identify session paths.** From this conversation's history, list *exactly* the repository files this session created or modified — the base glossary (`CONTEXT.md`, `CONTEXT-MAP.md`), session fragments (`.grill-context/**`, plus any legacy `CONTEXT.*.md` colocated beside a base), ADRs (`docs/adr/**`), and any code or prototype the session touched. Commit CONTEXT fragments **as-is** — do not consolidate them (consolidation is a separate pass). Do **not** include files this session never touched, even if dirty; prior-session artifacts are intentionally out of scope.
3. **Stage exactly those paths** (never `git add -A`) and create a **single commit**. Derive a short `<topic-slug>` from the subject of the grilling session (the term or area discussed). The type follows content:
   - docs-only → `docs(<topic-slug>): <summary> (ADR-NNNN + glossary)` (drop whichever parenthetical part doesn't apply)
   - mixed code + docs → a fitting conventional type (`feat`, `chore`, …), still scoped `(<topic-slug>)`

   Write a short human `<summary>` of what the artifacts cover (e.g. `effort-model-resolution glossary + ADR-0032`).

   Before writing the subject, **sample the repo's house style** — `git log -5 --format='%s%n%b'` — and infer the prevailing convention: conventional-commits `type(scope): subject` or not, subject capitalization, and any trailer (e.g. `Co-Authored-By`). Match that grammar and reproduce the trailer convention. Infer the *grammar*, don't copy the sampled type/scope verbatim — a skewed window (say five `fix(...)` commits) must not relabel a docs pass. The `type` still follows content (docs-only vs. mixed), as above.
4. **Report.** After committing, show the user the exact files staged and the commit subject. Separately, report any dirty files this session did *not* touch as "left alone — not staged" so nothing is silently swept or split.

After the commit, the plan is settled and persisted; the user will typically move on to a separate step (such as `to-tasks`) themselves.
