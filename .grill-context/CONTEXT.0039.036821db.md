---
fragment: 036821db
generation: 0039
branch: master
---

+ Work store
  The destination where planning skills publish their artifacts — task sets, specs, wayfinder maps and tickets, and future artifact kinds such as prototype data — together with that destination's vocabulary for expressing blocking edges and grabbing work. A repository resolves to exactly one Work store; pop's own **Task storage** backs the built-in default, and real trackers (GitHub, GitLab, local markdown, freeform) are alternative Work stores a repository may configure. Distinct from **Agent adapter** (the bridge to an agent CLI) and narrower than it sounds from "tracker": a Work store need not track anything, only hold published work.
  avoid: tracker, issue tracker (as the abstraction's name), task store, task storage adapter

+ Work store doc
  The per-operation document that adapts a planning skill's publish step to one **Work store** — sections such as publishing a spec, publishing tickets, and wayfinding operations, including any store-specific drafting vocabulary (e.g. effort and HITL/AFK for the pop store). Resolution is two-layer: the repo-level doc at `docs/agents/issue-tracker.md` (upstream tracker-doc convention, unchanged) wins when present; otherwise skills read the machine-global pop doc at `${XDG_CONFIG_HOME:-~/.config}/pop/work-store.md`, which Integration refresh seeds create-if-absent and never overwrites — user edits are the global override, and no per-agent memory files or new CLI commands are involved.
  avoid: adapter doc, tracker doc (for pop's own)

~ Task set
  The local `<id>/index.json` manifest and its sibling task markdown files beneath the **Task storage** `tasks/` directory, optionally alongside a co-located `spec.md` (the set's whole context in one folder; spec-less sets are normal). A Task set is the schedulable unit. Its directory name is its canonical identifier and display label; there is no separate Task-set title. Spec existence remains irrelevant to task scheduling and execution — `spec.md` is optional enrichment the **Verifier** may read, never a required input.
  avoid: Issue set, PRD, workload, prd.md (legacy filename; no backward-compat read)
  was: The local `<id>/index.json` manifest and its sibling task markdown files beneath the **Task storage** `tasks/` directory, optionally alongside a co-located `prd.md` (the set's whole context in one folder; PRD-less sets are normal). A Task set is the schedulable unit. Its directory name is its canonical identifier and display label; there is no separate Task-set title. PRD existence remains irrelevant to task scheduling and execution — `prd.md` is optional enrichment the **Verifier** may read, never a required input.

~ Agent verification
  An independent **Verifier** agent's judgment of a **Task set**'s completed AFK work. Its verdict scope is only the set's `done` AFK tasks — the prompt carries their bodies and acceptance criteria, the accumulated diff, and the optional co-located `spec.md`; open/not-`done` AFK tasks and HITL tasks (any status) are excluded so the Verifier never fails a set on work it isn't equipped to judge (a not-yet-run HITL sign-off is not an unmet criterion). Gated by user config, off by default. When enabled it fires as the tail of a **Drain**: on a DONE set, and on an **Awaiting-approval Task set** it runs *before* the terminal HITL sign-off gate — a PASS then opens that gate, so cheap agent checking precedes expensive human time.
  was: (identical except the optional co-located artifact was `prd.md`)
