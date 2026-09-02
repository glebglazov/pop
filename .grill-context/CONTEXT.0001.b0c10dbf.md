---
fragment: b0c10dbf
generation: 0001
branch: master
---

+ grill-with-docs-fast
  The human-opened fast-pass sibling of **grill-with-docs**: the same contract and the
  same close, conducted in as few rounds as possible by deciding the non-critical calls
  itself and reporting them as **Fast-pass decision**s.
  avoid: fp-grill-with-docs, fast grill, quick grill
  under: Language

+ Fast-pass decision
  A call **grill-with-docs-fast** makes instead of asking, because the answer is
  findable or does not reshape the rest of the design tree. Reported to the human in the
  round's ledger — the call, the rejected alternative, one clause of why — and persisted
  in the session's closing commit body, never in the glossary or an ADR.
  avoid: auto-decision, assumed answer, silent default
  under: Language

~ grill-with-docs
  The human-opened standalone grilling workflow: composes the Agent-loaded `grilling`
  and `domain-modeling` skills, and owns the **Shared skill document** `GRILL-SESSION.md`
  that carries Pop's once-per-round glossary timing, unified fact-finding and
  commit-on-close rules for itself and **grill-with-docs-fast** alike. Never loaded by a
  wayfinding ticket because its contract writes and commits repository artifacts.
  avoid: grill-me, the grilling skill
  was: The human-opened standalone grilling workflow: composes the Agent-loaded `grilling` and `domain-modeling` skills, then applies Pop's once-per-round glossary timing and commit-on-close rule. Never loaded by a wayfinding ticket because its contract writes and commits repository artifacts.

~ Task planning skills
  The embedded, Pop-independent skills installed together by the `task-skills`
  component, in three kinds: Workflow skills (grilling, grill-with-docs,
  grill-with-docs-fast, grill-with-map, grill-consolidate, to-spec, to-tasks, wayfinder,
  spend-audit), Tool skills (domain-modeling, prototype, research), and the Setup skill
  (`setup-matt-pocock-skills`). Versioned with the Pop binary and installed only by
  explicit opt-in; Pop's task scheduling and execution do not depend on them being
  installed.
  avoid: Workload framework, workload skills bundle, agent integration
  was: The embedded, Pop-independent skills installed together by the `task-skills` component, in three kinds: Workflow skills (grilling, grill-with-docs, grill-with-map, grill-consolidate, to-spec, to-tasks, wayfinder, spend-audit), Tool skills (domain-modeling, prototype, research), and the Setup skill (`setup-matt-pocock-skills`). Versioned with the Pop binary and installed only by explicit opt-in; Pop's task scheduling and execution do not depend on them being installed.

~ Workflow skill
  An embedded skill that is a session-shaped workflow someone opens deliberately —
  grilling, grill-with-docs, grill-with-docs-fast, grill-with-map, grill-consolidate,
  to-spec, to-tasks, wayfinder. Session shape says nothing about who opens it: that is
  the separate **Agent-loaded skill** axis, on which grilling and grill-with-map are
  agent-loaded and the rest human-opened. The counterpart of a Tool skill; the two kinds
  together make up the Task planning skills.
  avoid: command skill, manual-only skill
  was: An embedded skill that is a session-shaped workflow someone opens deliberately — grilling, grill-with-docs, grill-with-map, grill-consolidate, to-spec, to-tasks, wayfinder. Session shape says nothing about who opens it: that is the separate **Agent-loaded skill** axis, on which grilling and grill-with-map are agent-loaded and the rest human-opened. The counterpart of a Tool skill; the two kinds together make up the Task planning skills.

~ Shared skill document
  A Pop-owned companion document that more than one embedded skill depends on. Its
  canonical source lives with the skill that owns its meaning — ownership is per
  document, not one owner for all of them — and Integration copies it into each other
  consuming skill at install time. `domain-modeling` owns `CONTEXT-FORMAT.md` and
  `ADR-FORMAT.md`, which `grill-with-map` receives because it writes compatible Map
  drafts; `grill-with-docs` owns `GRILL-SESSION.md`, which `grill-with-docs-fast`
  receives because the two composers share one close. Distinct from an ordinary
  companion file, which has only one consumer.
  avoid: shared skill, common file, skill include
  was: A Pop-owned companion document that more than one embedded skill depends on. Its canonical source lives with the skill that owns its meaning, and Integration copies it into each other consuming skill at install time. `domain-modeling` owns `CONTEXT-FORMAT.md` and `ADR-FORMAT.md`; `grill-with-map` receives installed copies because it writes compatible Map drafts. Distinct from an ordinary companion file, which has only one consumer.
