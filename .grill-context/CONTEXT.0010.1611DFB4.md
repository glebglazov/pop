---
fragment: 1611DFB4
generation: 0010
branch: master
---

+ grilling
  The interview primitive both grilling skills compose: design tree, frontier, rounds, find-facts-yourself. A verbatim upstream overlay that reads the glossary union and writes nothing at all; every pop addition is a composition concern belonging to the composing skill, so a session loading it has no write instruction to disobey. Kept under its upstream name because the fork's old name credited an experiment upstream has since deleted.
  avoid: batch-grill-me, grilling primitive, base grill

- batch-grill-me

~ Task planning skills
  The embedded, pop-independent skills installed together by the `task-skills` component, in three kinds: Workflow skills (grilling, grill-with-docs, grill-with-map, to-spec, to-tasks, wayfinder — session-shaped, manual-invocation-only; grill-consolidate rides along as the glossary-maintenance pass), Tool skills (prototype, research — model-invoked, verbatim upstream), and the Setup skill (setup-matt-pocock-skills — session-shaped, manual-invocation-only, prepares a repo for the others). Versioned with the pop binary and installed only by explicit opt-in; pop's task scheduling and execution do not depend on them being installed.
  avoid: Workload framework, workload skills bundle, agent integration
  was: The embedded, pop-independent skills installed together by the `task-skills` component, in three kinds: Workflow skills (batch-grill-me, grill-with-docs, grill-with-map, to-spec, to-tasks, wayfinder — session-shaped, manual-invocation-only; grill-consolidate rides along as the glossary-maintenance pass), Tool skills (prototype, research — model-invoked, verbatim upstream), and the Setup skill (setup-matt-pocock-skills — session-shaped, manual-invocation-only, prepares a repo for the others). Versioned with the pop binary and installed only by explicit opt-in; pop's task scheduling and execution do not depend on them being installed.

~ grill-with-docs
  The standalone grilling skill: **grilling** plus the domain-modeling write discipline (glossary fragments, numbered ADRs) and commit-on-close. Composed over the primitive rather than inlining the interview rules, so its verbatim upstream region is the domain-modeling half alone. Never loaded by a wayfinding ticket — its contract mandates repository writes.
  avoid: grill-me, the grilling skill
  was: The standalone grilling skill: **batch-grill-me** plus the domain-modeling write discipline (glossary fragments, numbered ADRs) and commit-on-close. Composed over the primitive rather than inlining the interview rules, so its verbatim upstream region is the domain-modeling half alone. Never loaded by a wayfinding ticket — its contract mandates repository writes.

~ grill-with-map
  The grilling skill a wayfinding ticket loads: **grilling** plus the wayfinding answer discipline (ADR-shaped answers, **ADR draft**s and **Context draft**s, prototypes to the Map's scratch directory). Writes only into the Map — never the repo, never a commit.
  avoid: grill-in-map, wayfinder grilling
  was: The grilling skill a wayfinding ticket loads: **batch-grill-me** plus the wayfinding answer discipline (ADR-shaped answers, **ADR draft**s and **Context draft**s, prototypes to the Map's scratch directory). Writes only into the Map — never the repo, never a commit.

~ Shared skill document
  A pop-owned companion document that more than one embedded skill depends on, held once under `integrate/skills/pop/_shared/` and copied into each consuming skill's installed directory at install time — only the destination differs per skill. `CONTEXT-FORMAT.md` (the glossary union rule and the `+`/`~`/`-` op syntax, read by grilling and written against by grill-with-docs and grill-with-map) and `ADR-FORMAT.md` (the ADR template, used by grill-with-docs for a numbered repo ADR and by grill-with-map for an unnumbered **ADR draft**) are the first two. Distinct from an ordinary companion file, which lives in its one skill's own directory; a shared document that goes missing or drifts from its source is a **Doctor** finding like any other rendered file.
  avoid: shared skill, common file, skill include
  was: A pop-owned companion document that more than one embedded skill depends on, held once under `integrate/skills/pop/_shared/` and copied into each consuming skill's installed directory at install time — only the destination differs per skill. `CONTEXT-FORMAT.md` (the glossary union rule and the `+`/`~`/`-` op syntax, read by batch-grill-me and written against by grill-with-docs and grill-with-map) and `ADR-FORMAT.md` (the ADR template, used by grill-with-docs for a numbered repo ADR and by grill-with-map for an unnumbered **ADR draft**) are the first two. Distinct from an ordinary companion file, which lives in its one skill's own directory; a shared document that goes missing or drifts from its source is a **Doctor** finding like any other rendered file.

~ Workflow skill
  An embedded skill that is a session-shaped workflow someone opens deliberately — grilling, grill-with-docs, grill-with-map, grill-consolidate, to-spec, to-tasks, wayfinder. Session shape says nothing about who opens it: that is the separate **Agent-loaded skill** axis, on which grilling and grill-with-map are agent-loaded and the rest human-opened. The counterpart of a Tool skill; the two kinds together make up the Task planning skills.
  avoid: command skill, manual-only skill
  was: An embedded skill that is a session-shaped workflow someone opens deliberately — batch-grill-me, grill-with-docs, grill-with-map, grill-consolidate, to-spec, to-tasks, wayfinder. Session shape says nothing about who opens it: that is the separate **Agent-loaded skill** axis, on which batch-grill-me and grill-with-map are agent-loaded and the rest human-opened. The counterpart of a Tool skill; the two kinds together make up the Task planning skills.

~ Agent-loaded skill
  An embedded skill another skill's body tells the model to load, so it carries no `disable-model-invocation` — grill-with-map and grilling (loaded by a wayfinding ticket and by the grilling skills that compose the interview primitive), plus the Tool skills prototype and research. Its counterpart is a **human-opened** skill, which keeps the flag because a human decides when the session starts: grill-with-docs, grill-consolidate, setup-matt-pocock-skills, spend-audit, to-spec, to-tasks, wayfinder. The axis is *who loads it*, not whether it is session-shaped: grill-with-map is a whole session and still agent-loaded, because the only thing that ever opens it is a Decision ticket. Classification is a property of the skill, decided once per embedded skill and recorded in its overlay header when it contradicts upstream's frontmatter — never worked around by composing slash-command text in a pop verb.
  avoid: model-invoked skill, auto-triggered skill, manual-only skill
  was: An embedded skill another skill's body tells the model to load, so it carries no `disable-model-invocation` — grill-with-map and batch-grill-me (loaded by a wayfinding ticket and by the grilling skills that compose the interview primitive), plus the Tool skills prototype and research. Its counterpart is a **human-opened** skill, which keeps the flag because a human decides when the session starts: grill-with-docs, grill-consolidate, setup-matt-pocock-skills, spend-audit, to-spec, to-tasks, wayfinder. The axis is *who loads it*, not whether it is session-shaped: grill-with-map is a whole session and still agent-loaded, because the only thing that ever opens it is a Decision ticket. Classification is a property of the skill, decided once per embedded skill and recorded in its overlay header when it contradicts upstream's frontmatter — never worked around by composing slash-command text in a pop verb.
