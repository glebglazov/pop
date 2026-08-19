---
fragment: 98006D87
generation: 0026
branch: master
---

+ domain-modeling
  The Agent-loaded skill that maintains the repository's domain language and decisions: upstream's active `CONTEXT.md` and ADR discipline, with Pop's fragment-based parallel-write rules as its overlay. Owns the installed `CONTEXT-FORMAT.md` and `ADR-FORMAT.md` companions; other skills may compose it.
  avoid: domain model skill, glossary skill
  under: Agent integrations

+ Domain docs
  The repository instructions that give every agent passive domain awareness: `AGENTS.md`/`CLAUDE.md` points to `docs/agents/domain.md`, which tells readers where the base glossary and ADRs live and, in Pop-configured repositories, that the effective glossary includes matching `.grill-context` fragments. The active write algorithm belongs to `domain-modeling`, not these consumer instructions.
  avoid: context instructions, domain-modeling instructions
  under: Agent integrations

~ grilling
  The Agent-loaded interview primitive the two grilling skills compose: a pinned, byte-identical copy of upstream `grilling` containing the design tree, frontier, rounds and fact-finding rules. It owns no Pop glossary or write discipline.
  avoid: batch-grill-me, grilling primitive, base grill
  was: The interview primitive both grilling skills compose: design tree, frontier, rounds, find-facts-yourself. A verbatim upstream overlay that reads the glossary union and writes nothing at all; every pop addition is a composition concern belonging to the composing skill, so a session loading it has no write instruction to disobey. Kept under its upstream name because the fork's old name credited an experiment upstream has since deleted.

~ grill-with-docs
  The human-opened standalone grilling workflow: composes the Agent-loaded `grilling` and `domain-modeling` skills, then applies Pop's once-per-round glossary timing and commit-on-close rule. Never loaded by a wayfinding ticket because its contract writes and commits repository artifacts.
  avoid: grill-me, the grilling skill
  was: The standalone grilling skill: **grilling** plus the domain-modeling write discipline (glossary fragments, numbered ADRs) and commit-on-close. Composed over the primitive rather than inlining the interview rules, so its verbatim upstream region is the domain-modeling half alone. Never loaded by a wayfinding ticket — its contract mandates repository writes.

~ Shared skill document
  A Pop-owned companion document that more than one embedded skill depends on. Its canonical source lives with the skill that owns its meaning, and Integration copies it into each other consuming skill at install time. `domain-modeling` owns `CONTEXT-FORMAT.md` and `ADR-FORMAT.md`; `grill-with-map` receives installed copies because it writes compatible Map drafts. Distinct from an ordinary companion file, which has only one consumer.
  avoid: shared skill, common file, skill include
  was: A pop-owned companion document that more than one embedded skill depends on, held once under `integrate/skills/pop/_shared/` and copied into each consuming skill's installed directory at install time — only the destination differs per skill. `CONTEXT-FORMAT.md` (the glossary union rule and the `+`/`~`/`-` op syntax, read by grilling and written against by grill-with-docs and grill-with-map) and `ADR-FORMAT.md` (the ADR template, used by grill-with-docs for a numbered repo ADR and by grill-with-map for an unnumbered **ADR draft**) are the first two. Distinct from an ordinary companion file, which lives in its one skill's own directory; a shared document that goes missing or drifts from its source is a **Doctor** finding like any other rendered file.

~ Agent-loaded skill
  An embedded skill that another skill may load and that can also trigger when a conversation fits, so it carries no `disable-model-invocation`: grilling, domain-modeling, grill-with-map, prototype and research. Its counterpart is a **human-opened** skill, which keeps the flag because a human decides when its session starts. The axis is who may load it, not whether it is session-shaped.
  avoid: model-invoked skill, auto-triggered skill, manual-only skill
  was: An embedded skill another skill's body tells the model to load, so it carries no `disable-model-invocation` — grill-with-map and grilling (loaded by a wayfinding ticket and by the grilling skills that compose the interview primitive), plus the Tool skills prototype and research. Its counterpart is a **human-opened** skill, which keeps the flag because a human decides when the session starts: grill-with-docs, grill-consolidate, setup-matt-pocock-skills, spend-audit, to-spec, to-tasks, wayfinder. The axis is *who loads it*, not whether it is session-shaped: grill-with-map is a whole session and still agent-loaded, because the only thing that ever opens it is a Decision ticket. Classification is a property of the skill, decided once per embedded skill and recorded in its overlay header when it contradicts upstream's frontmatter — never worked around by composing slash-command text in a pop verb.

~ Task planning skills
  The embedded, Pop-independent skills installed together by the `task-skills` component, in three kinds: Workflow skills (grilling, grill-with-docs, grill-with-map, grill-consolidate, to-spec, to-tasks, wayfinder, spend-audit), Tool skills (domain-modeling, prototype, research), and the Setup skill (`setup-matt-pocock-skills`). Versioned with the Pop binary and installed only by explicit opt-in; Pop's task scheduling and execution do not depend on them being installed.
  avoid: Workload framework, workload skills bundle, agent integration
  was: The embedded, pop-independent skills installed together by the `task-skills` component, in three kinds: Workflow skills (grilling, grill-with-docs, grill-with-map, to-spec, to-tasks, wayfinder — session-shaped, manual-invocation-only; grill-consolidate rides along as the glossary-maintenance pass), Tool skills (prototype, research — model-invoked, verbatim upstream), and the Setup skill (setup-matt-pocock-skills — session-shaped, manual-invocation-only, prepares a repo for the others). Versioned with the pop binary and installed only by explicit opt-in; pop's task scheduling and execution do not depend on them being installed.

~ Tool skill
  An embedded, Agent-loaded, general-purpose capability rather than a session-shaped workflow. `domain-modeling` is a marked upstream overlay that maintains glossary fragments and ADRs; `prototype` and `research` are adopted verbatim from upstream. A Workflow skill may compose a Tool skill while keeping destination-specific packaging rules in the caller.
  avoid: helper skill, sub-skill, wayfinder component
  was: An embedded skill that is a general-purpose instrument rather than a session workflow — prototype and research, adopted verbatim from upstream. Both are **Agent-loaded skills**, so they auto-trigger when the conversation shape matches, but the instrument/workflow distinction is about shape, not invocability. Callers such as the wayfinder skill compose tool skills by naming them; caller-side packaging rules (where the output lands — e.g. a Decision ticket's `## Answer`) live in the caller, never in the tool itself.
