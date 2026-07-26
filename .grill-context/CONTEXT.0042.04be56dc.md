---
fragment: 04be56dc
generation: 0042
branch: master
---

+ Setup skill
  The embedded `setup-matt-pocock-skills` Workflow skill, kept under its upstream name to credit the flow's origin: a manual-only session that prepares a repository for the Task planning skills. It authors `docs/agents/issue-tracker.md` (skipped by default when the Work store choice is pop — an absent repo doc defers to each machine's seeded `work-store.md`; a committed one pins the choice for the whole team), sets up the domain-docs layout and `docs/agents/domain.md`, and adds an Agent-skills block to CLAUDE.md/AGENTS.md so repo-resident agents discover these files. It never scaffolds `.pop/`, and its triage-labels section is negated (pop ships no triage skill).
  under: Agent integrations

~ Task planning skills
  The embedded, pop-independent skills installed together by the `task-skills` component, in three kinds: Workflow skills (grill-with-docs, to-spec, to-tasks, wayfinder — session-shaped, manual-invocation-only; grill-consolidate rides along as the glossary-maintenance pass), Tool skills (prototype, research — model-invoked, verbatim upstream), and the Setup skill (setup-matt-pocock-skills — session-shaped, manual-invocation-only, prepares a repo for the others). Versioned with the pop binary and installed only by explicit opt-in; pop's task scheduling and execution do not depend on them being installed.
  was: The embedded, pop-independent skills installed together by the `task-skills` component, in two kinds: Workflow skills (grill-with-docs, to-prd, to-tasks, wayfinder — session-shaped, manual-invocation-only; grill-consolidate rides along as the glossary-maintenance pass) and Tool skills (prototype, research — model-invoked, verbatim upstream). Versioned with the pop binary and installed only by explicit opt-in; pop's task scheduling and execution do not depend on them being installed.
