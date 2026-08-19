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

## The effective glossary is the base plus its fragments

`CONTEXT.md` holds only the settled part of this repo's language. The
**effective glossary** is that base file overlaid, at read time, with every
glossary fragment that deltas it:

- `.grill-context/CONTEXT.<counter>.<uuid>.md` at the repo root — where
  fragments live now. (In a multi-context repo the `CONTEXT` position is the
  context's slug from `CONTEXT-MAP.md`; this repo is single-context.)
- legacy `CONTEXT.<counter>.<uuid>.md` beside the base file — where older
  sessions wrote them. Still read, until they are consolidated away.

`.grill-context/` is a dotdir, so plain `rg` and `ls` skip it — glob it with
`rg --hidden` or `ls -a`.

A fragment is a list of delta ops, not a glossary of its own: `+ Term` adds,
`~ Term` redefines, `- Term` retires. A fragment op beats the base, a higher
`<counter>` generation beats a lower one for the same term, and two fragments of
the same generation touching one term are contested — treat both readings as
live rather than picking a winner. A term you find only in a fragment is still
this repo's word for the thing.

Reading is all this file asks of you. Don't edit `CONTEXT.md` and don't write a
fragment from these instructions: a session that settles a term loads the
`domain-modeling` skill, which owns the fragment format, its generation counter
and collision handling; `grill-consolidate` is the only thing that folds
fragments into the base.

## Use the glossary's vocabulary

When your output names a domain concept (in an issue title, a refactor proposal, a hypothesis, a test name), use the term as defined in `CONTEXT.md`. Don't drift to synonyms the glossary explicitly avoids.

If the concept you need isn't in the glossary yet, that's a signal — either you're inventing language the project doesn't use (reconsider) or there's a real gap (note it for `/domain-modeling`).

## Flag ADR conflicts

If your output contradicts an existing ADR, surface it explicitly rather than silently overriding:

> _Contradicts ADR-0007 (event-sourced orders) — but worth reopening because…_
