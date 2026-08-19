<!--
base: mattpocock/skills skills/engineering/setup-matt-pocock-skills/domain.md@8b78b53

This file is a marked overlay. Everything from here down to the "POP OVERLAY"
marker is a byte-verbatim copy of upstream
skills/engineering/setup-matt-pocock-skills/domain.md at the pinned ref
mattpocock/skills@8b78b53. Pop inlines the
seed templates rather than delegating to Matt's skills (ADR-0009). The pop
overlay in SKILL.md keeps upstream's `docs/agents/domain.md` write, so this
seed is load-bearing on every run. To review upstream drift, diff the region
between this header and the marker against
skills/engineering/setup-matt-pocock-skills/domain.md@<newref>.

The pin was re-reviewed for ADR-0225 decision 7 and is unchanged: 8b78b53 is
the reviewed current revision for every upstream-backed file in this work. This
distribution is embedded and offline, so a re-pin is a deliberate edit here, not
a fetch at run time.
-->

# Domain Docs

How the engineering skills should consume this repo's domain documentation when exploring the codebase.

## Before exploring, read these

- **`CONTEXT.md`** at the repo root, or
- **`CONTEXT-MAP.md`** at the repo root if it exists — it points at one `CONTEXT.md` per context. Read each one relevant to the topic.
- **`docs/adr/`** — read ADRs that touch the area you're about to work in. In multi-context repos, also check `src/<context>/docs/adr/` for context-scoped decisions.

If any of these files don't exist, **proceed silently**. Don't flag their absence; don't suggest creating them upfront. The `/domain-modeling` skill (reached via `/grill-with-docs` and `/improve-codebase-architecture`) creates them lazily when terms or decisions actually get resolved.

## File structure

Single-context repo (most repos):

```
/
├── CONTEXT.md
├── docs/adr/
│   ├── 0001-event-sourced-orders.md
│   └── 0002-postgres-for-write-model.md
└── src/
```

Multi-context repo (presence of `CONTEXT-MAP.md` at the root):

```
/
├── CONTEXT-MAP.md
├── docs/adr/                          ← system-wide decisions
└── src/
    ├── ordering/
    │   ├── CONTEXT.md
    │   └── docs/adr/                  ← context-specific decisions
    └── billing/
        ├── CONTEXT.md
        └── docs/adr/
```

## Use the glossary's vocabulary

When your output names a domain concept (in an issue title, a refactor proposal, a hypothesis, a test name), use the term as defined in `CONTEXT.md`. Don't drift to synonyms the glossary explicitly avoids.

If the concept you need isn't in the glossary yet, that's a signal — either you're inventing language the project doesn't use (reconsider) or there's a real gap (note it for `/domain-modeling`).

## Flag ADR conflicts

If your output contradicts an existing ADR, surface it explicitly rather than silently overriding:

> _Contradicts ADR-0007 (event-sourced orders) — but worth reopening because…_
<!-- ═══════════════════════════════ POP OVERLAY ═══════════════════════════════
Pop's one delta to this seed: the base file is not the whole glossary. A pop
grilling session never writes `CONTEXT.md` in place — it appends a delta
fragment, so parallel agents and teammates cannot conflict over one shared file
(ADR-0089, ADR-0225) — and an agent that reads only the base therefore reads a
glossary that is behind the repo. The rule below is the *consumer* half of that
scheme and deliberately nothing more. Creating a fragment, numbering its
generation, resolving collisions and folding fragments back in belong to the
`domain-modeling` skill and its `CONTEXT-FORMAT.md`, which a session loads when
it is actually changing the model. Turn-one repository instructions are read by
every agent in the repo, most of which only read the glossary; a write algorithm
here would cost all of them context and would give the scheme two owners.
-->

## The effective glossary is the base plus its fragments

`CONTEXT.md` holds only the settled part of a context's language. The
**effective glossary** is that base file overlaid, at read time, with every
glossary fragment that deltas it:

- `.grill-context/<slug>.<counter>.<uuid>.md` at the repo root — where fragments
  live now. `<slug>` is the context's link text from `CONTEXT-MAP.md`,
  lowercased with each run of non-alphanumeric characters collapsed to `-`, or
  the literal `CONTEXT` in a single-context repo. Select one context's fragments
  by that prefix.
- legacy `<dir>/CONTEXT.<counter>.<uuid>.md` beside a base `CONTEXT.md` — where
  older sessions wrote them. Pop still reads these, so read them too until they
  are gone.

Glob both locations at session start, and include hidden paths — `.grill-context/`
is a dotdir, so plain `rg` and `ls` skip it (`rg --hidden`, `ls -a`).

A fragment is a list of delta ops against the base, not a glossary of its own:
`+ Term` adds, `~ Term` redefines, `- Term` retires. Read them over the base:
a fragment op beats the base, a higher `<counter>` generation beats a lower one
for the same term, and two fragments of the *same* generation touching one term
are contested — treat both readings as live and do not pick a winner. A term you
find only in a fragment is still this repo's word for the thing; use it, and
honour its `avoid:` list, exactly as if it were in the base.

Reading is all this file asks of you. Do not edit `CONTEXT.md` and do not write
a fragment from these instructions: a session that settles a term loads the
`domain-modeling` skill, which owns the fragment format and its write rules, and
`grill-consolidate` is the only thing that folds fragments into the base.
