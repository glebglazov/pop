---
fragment: E80A25F2
generation: 0008
branch: master
---

~ Setup skill
  The embedded `setup-matt-pocock-skills` Workflow skill, kept under its upstream name to credit the flow's origin: a manual-only session that prepares a repository for the Task planning skills. It authors `docs/agents/issue-tracker.md` (skipped by default when the Work store choice is pop — an absent repo doc defers to the machine-level `~/.agents/docs/issue-tracker.md`; a committed one pins the choice for the whole team), sets up the domain-docs layout and `docs/agents/domain.md`, and adds an Agent-skills block to CLAUDE.md/AGENTS.md so repo-resident agents discover these files. It never scaffolds `.pop/`, and its triage-labels section is negated (pop ships no triage skill).
  was: The embedded `setup-matt-pocock-skills` Workflow skill, kept under its upstream name to credit the flow's origin: a manual-only session that prepares a repository for the Task planning skills. It authors `docs/agents/issue-tracker.md` (skipped by default when the Work store choice is pop — an absent repo doc defers to each machine's seeded `work-store.md`; a committed one pins the choice for the whole team), sets up the domain-docs layout and `docs/agents/domain.md`, and adds an Agent-skills block to CLAUDE.md/AGENTS.md so repo-resident agents discover these files. It never scaffolds `.pop/`, and its triage-labels section is negated (pop ships no triage skill).
