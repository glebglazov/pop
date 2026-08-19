---
name: domain-modeling
description: Actively maintain the project's domain model — challenge terms against the glossary, sharpen fuzzy language, and record what settles as glossary deltas and ADRs. Conflict-free under parallel agents and teams via generation-numbered glossary fragments and clash-tolerant ADR ids. Use when a session is changing the project's language or its recorded decisions, on its own or loaded by a workflow that grills a plan.
---

<!--
base: mattpocock/skills domain-modeling@8b78b53

This file is a marked overlay. Everything from here down to the "POP OVERLAY"
marker is a verbatim copy of domain-modeling/SKILL.md at the pinned ref above —
the active glossary and ADR discipline on its own. Pop inlines it rather than
delegating to `/domain-modeling`, per ADR-0009 (skills are embedded in the
binary and ship to machines without Matt's skills installed).

Upstream's frontmatter is replaced by pop's; the classification is upstream's
own: **agent-loaded**, so no `disable-model-invocation`. A workflow that records
what a session settles loads this skill instead of restating the discipline, and
a conversation that is changing the model can reach it directly. The manual gate
belongs to the sessions that own a destination and a commit, not to the
reusable discipline underneath them.

Pop's parallel-safety additions live below the marker: a session writes a
`.grill-context` fragment rather than the base `CONTEXT.md`, reads the glossary
as base + fragments, and takes ADR ids clash-tolerantly. To review upstream
drift, diff the region between this header and the marker against
domain-modeling@<newref>.

The two format documents beside this file are this skill's own companions, as
they are upstream. Integration copies them into every other installed skill
that reads or writes compatible glossary and ADR documents, so no consumer has
to know where the canonical copy lives; see `sharedSkillDocs` in
integrate/catalog.go.
-->

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
Everything below is pop-specific and has no upstream twin. It carries the one
behavioural override of the base — replacing the "Update CONTEXT.md inline"
single-writer instruction with a per-session fragment and a union read — plus
the ADR id rule, the grill-consolidate fold-in path, and the write boundary
that keeps this skill safe to load from any workflow. Where a line below
contradicts the verbatim upstream region, the line below wins; the upstream
text is kept byte-intact only so drift stays diffable.
-->

## Single-writer override

**Override (negates the "Update CONTEXT.md inline" section above): never write
the base `CONTEXT.md` — write a delta op to your own per-session fragment per
[CONTEXT-FORMAT.md](./CONTEXT-FORMAT.md), and treat the glossary you challenge
terms against as the union of base + fragments.** Read
[CONTEXT-FORMAT.md](./CONTEXT-FORMAT.md) before your first write: it has where
fragments live, how a generation is picked, the `+`/`~`/`-` op syntax, and how
a same-generation collision is rendered. This keeps concurrent sessions and
teams conflict-free; the "update `CONTEXT.md` right there" wording upstream is
the one place pop deviates, and this line is authoritative.

The upstream timing survives the override: when a term settles, write its op to
your fragment then, not at the end. A workflow that loads this skill may set its
own beat — one that runs the conversation in rounds groups a round's ops into a
single fragment update — and its timing wins for that session. Nothing else
about the fragment changes.

If the user asks you to **consolidate** (fold accumulated fragments into the
base), use the `grill-consolidate` skill. Consolidation is the only pass that
mutates `CONTEXT.md`: a deliberate single-writer maintenance step, never
something to do mid-session.

## ADR ids are clash-tolerant

Take the next ADR number naively — highest existing `NNNN` in the target
`docs/adr/`, plus one — and don't lock or hunt for gaps.
[ADR-FORMAT.md](./ADR-FORMAT.md) has the full rule, including what happens when
two parallel sessions land on the same number and how to cross-reference an ADR
so the link survives a renumber.

## This skill writes, it does not commit

The fragments and ADRs are yours to write. Committing them is not: a session
opened directly on this skill leaves the artifacts in the working tree and says
what it wrote. The workflow that composed this skill owns the commit — the
`grill-with-docs` session commits its artifacts when the design settles, and a
wayfinding session commits nothing to the repository at all. Don't commit here,
and don't ask to.
