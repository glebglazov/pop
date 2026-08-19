---
status: accepted
---

# Domain modeling owns the fragment discipline

Pop will match Matt Pocock's skill composition: `grill-with-docs` loads separate `grilling` and `domain-modeling` skills. Pop keeps its parallel-safe glossary fragments, Map-only drafts and commit-on-close behavior, but each rule moves to the skill that owns its destination. This amends the skill boundaries in [ADR-0112](0112-grill-skills-are-a-marked-overlay-on-pinned-domain-modeling.md) and [ADR-0171](0171-wayfinding-decisions-mint-through-implementing-slices.md); their self-contained embedding and Map write-boundary decisions remain in force.

## Decision

1. **`grilling` is the pinned upstream skill without a Pop behavioral overlay.** It owns only the interview: design tree, frontier, rounds and fact-finding. It has no `CONTEXT-FORMAT.md` companion and no Pop glossary rule.

2. **`domain-modeling` becomes an Agent-loaded Tool skill.** Its upstream region owns active glossary and ADR maintenance. Its Pop overlay replaces direct `CONTEXT.md` writes with `.grill-context` fragments and retains Pop's union, operation, generation, collision, consolidation and clash-tolerant ADR rules. Used directly, it writes a fragment when a term settles.

3. **`domain-modeling` owns the companion documents.** The canonical `CONTEXT-FORMAT.md` and `ADR-FORMAT.md` sources move from `_shared/` beside `domain-modeling`, matching upstream ownership. Each remains a marked upstream base plus Pop's parallel-safety overlay. Integration copies both into `grill-with-map`, the other skill that writes compatible glossary and ADR drafts.

4. **`grill-with-docs` becomes a human-opened composer.** It loads `grilling` and `domain-modeling`, groups settled glossary operations into one fragment update at each grilling round's close, retains the unified fact-finding rule, and commits only the session's artifacts when the session closes. It owns no copy of either format document.

5. **`grill-with-map` keeps its Map-only write discipline.** It composes `grilling`, not repository-writing `domain-modeling`; writes stay under the Map and never enter or commit the repository. Its installed format documents come from the `domain-modeling` source so Map drafts remain compatible with later minting.

6. **Domain docs own passive discovery.** Pop's overlay on the setup skill's `docs/agents/domain.md` template tells ordinary agents that the effective glossary is the base `CONTEXT.md` plus matching `.grill-context` fragments. The `AGENTS.md`/`CLAUDE.md` block remains the pointer to those consumer rules. Active Pop workflows do not depend on setup: `grill-with-docs` loads `domain-modeling`, and `grill-with-map` carries its own rule. Bare `grilling` without repository domain instructions has upstream's interview-only behavior.

7. **The restructure includes an upstream refresh.** Each upstream-backed file is re-pinned to the reviewed current revision and remains embedded with Pop. Runtime installation stays offline and deterministic; later upstream changes still require a deliberate re-pin.

## Considered Options

- **Keep the current `grilling` overlay and inline domain modeling in `grill-with-docs`.** Rejected because it gives the upstream interview primitive a Pop glossary responsibility, duplicates glossary reading in both composing skills, and hides an upstream Tool skill inside a Workflow skill.
- **Put all context behavior in `grill-with-docs`.** Rejected because direct `domain-modeling` would not exist, other workflows could not compose it, and companion-file ownership would still differ from upstream.
- **Rely only on setup-generated repository instructions.** Rejected because users can install the Task planning skills without running setup. Skills that actively read or write the domain model must carry their own complete rules.
- **Create a fourth Discipline-skill kind.** Rejected because one skill does not justify a new taxonomy. `domain-modeling` fits Tool skill: it is an Agent-loaded reusable capability rather than a session-shaped workflow.
- **Follow upstream `main` at runtime.** Rejected by ADR-0009's embedded distribution contract. Structural alignment does not remove Pop's deliberate pin.

## Consequences

- Pop's skill graph and companion-file ownership match upstream, while Pop-specific storage and closing behavior remain explicit overlays.
- Direct `domain-modeling` can create glossary fragments or ADRs outside a grilling session. It does not commit them; commit-on-close belongs only to `grill-with-docs`.
- Direct bare `grilling` no longer guarantees glossary-union reading. Configured repositories provide passive awareness through their Domain docs; workflows that need the glossary load or carry the relevant discipline.
- The catalog, rendering map, drift fixtures, Doctor evidence and installation tests must add `domain-modeling`, remove `grilling`'s companion, and copy both format documents from their new canonical owner into `grill-with-map`.
