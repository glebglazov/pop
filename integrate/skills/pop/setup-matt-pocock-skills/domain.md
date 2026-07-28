<!--
base: mattpocock/skills skills/engineering/setup-matt-pocock-skills/domain.md@ed37663

This file is a marked overlay. Everything from here down to the "POP OVERLAY"
marker is a byte-verbatim copy of upstream
skills/engineering/setup-matt-pocock-skills/domain.md at the pinned ref
mattpocock/skills@ed37663cc5fbef691ddfecd080dff42f7e7e350d. Pop inlines the
seed templates rather than delegating to Matt's skills (ADR-0009). The pop
overlay in SKILL.md keeps upstream's `docs/agents/domain.md` write, so this
seed is load-bearing on every run. To review upstream drift, diff the region
between this header and the marker against
skills/engineering/setup-matt-pocock-skills/domain.md@<newref>.
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
Pop carries no delta to this seed — upstream's domain-doc consumer rules
apply verbatim. The region is kept present (header + marker) only so drift
stays diffable; there is no pop-specific content below.
-->
