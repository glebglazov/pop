---
fragment: a3f91b2c
generation: 0032
branch: master (wayfinder skill-family grilling)
---

+ Tool skill
  An embedded skill that is a general-purpose instrument, not a session workflow: model-invoked (no `disable-model-invocation`), so it auto-triggers when the conversation shape matches — prototype and research, adopted verbatim from upstream. Callers such as the wayfinder skill compose tool skills by naming them; caller-side packaging rules (where the output lands — e.g. a Decision ticket's `## Answer`) live in the caller, never in the tool itself.
  avoid: helper skill, sub-skill, wayfinder component
  under: Agent integrations

+ Workflow skill
  An embedded skill that is a session-shaped workflow the user opens deliberately: manual-invocation-only via `disable-model-invocation` — grill-with-docs, grill-consolidate, to-prd, to-tasks, wayfinder. The counterpart of a Tool skill; the two kinds together make up the Task planning skills.
  avoid: command skill, manual-only skill
  under: Agent integrations

~ Task planning skills
  The embedded, pop-independent skills installed together by the `task-skills` component, in two kinds: Workflow skills (grill-with-docs, to-prd, to-tasks, wayfinder — session-shaped, manual-invocation-only; grill-consolidate rides along as the glossary-maintenance pass) and Tool skills (prototype, research — model-invoked, verbatim upstream). Versioned with the pop binary and installed only by explicit opt-in; pop's task scheduling and execution do not depend on them being installed.
  was: The embedded, pop-independent skills (grill-with-docs, to-prd, to-tasks) whose output feeds Task sets. Versioned with the pop binary and installed only by explicit opt-in; pop's task scheduling and execution do not depend on them being installed. grill-consolidate also ships embedded, but is a glossary-maintenance pass that folds CONTEXT fragments into the base — not a Task-set producer.

~ Skills prefix
  The configurable string prepended to an embedded skill's base name to form its installed name (`<prefix><base>`). Set via `skills_prefix` in `[integrations]`, default `pop-`; an empty value installs skills under their bare base name. The prefix reaches skill *bodies* too: render rewrites cross-skill references (the known embedded base names) to their resolved installed names, so a rendered skill never tells an agent to run a skill under a name that isn't in its listing. Embedded sources stay byte-intact — the rewrite happens only at render, keeping upstream-drift diffs clean.
  was: The configurable string prepended to an embedded skill's base name to form its installed name (`<prefix><base>`). Set via `skills_prefix` in `[integrations]`, default `pop-`; an empty value installs skills under their bare base name.
