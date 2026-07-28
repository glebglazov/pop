---
fragment: be50cd9a
generation: 0047
branch: master
---

+ Shipped asset
  A static document or file whose correct content is determined by the pop
  binary, not by the user — it is installed into pop's data dir and refreshed
  from the embedded copy whenever the two differ, so it always describes the
  binary that is installed. The user's config dir holds only hand-authored
  files: pop writes there only from a command whose purpose is editing config
  at the user's request, so a Shipped asset never lands there.
  avoid: seeded doc, machine-global config, static config
  under: Integration

~ Work store doc
  The per-operation document that adapts a planning skill's publish step to one **Work store** — sections such as publishing a spec, publishing tickets, and wayfinding operations, including any store-specific drafting vocabulary (e.g. effort and HITL/AFK for the pop store). Resolution is two-layer: the repo-level doc at `docs/agents/issue-tracker.md` (upstream tracker-doc convention, unchanged) wins when present; otherwise skills read pop's own doc, a **Shipped asset** at `${XDG_DATA_HOME:-~/.local/share}/pop/work-store.md` which Integration refresh rewrites whenever it differs from the embedded copy. There is no machine-global user override: a user who wants different publish behaviour writes the repo doc.
  was: The per-operation document that adapts a planning skill's publish step to one **Work store** — sections such as publishing a spec, publishing tickets, and wayfinding operations, including any store-specific drafting vocabulary (e.g. effort and HITL/AFK for the pop store). Resolution is two-layer: the repo-level doc at `docs/agents/issue-tracker.md` (upstream tracker-doc convention, unchanged) wins when present; otherwise skills read the machine-global pop doc at `${XDG_CONFIG_HOME:-~/.config}/pop/work-store.md`, which Integration refresh seeds create-if-absent and never overwrites — user edits are the global override, and no per-agent memory files or new CLI commands are involved.

~ Trunk worktree
  A repository's single canonical fork base for managed **Worktree set**s. A non-bare repo defaults its trunk to the git main worktree with no config; a bare repo has no implicit trunk and must have one named, either hand-authored as a `trunk = true` per-checkout **Repo override** or recorded by pop into the runtime tier (`config.runtime.toml`) when `--trunk` names one at a managed register — the hand-authored value winning, and the trunk key itself never resolving through the trunk-anchored runtime layer. Managed worktrees fork from the trunk's HEAD; reconciling a completed worktree branch back into trunk is the human's own concern, not something pop does. A bare repo with no trunk from either source has none, so pop cannot provision a managed worktree there — it can only drain in place in whatever checkout the operator is currently sitting in.
  was: A repository's single canonical fork base for managed **Worktree set**s. A non-bare repo defaults its trunk to the git main worktree with no config; a bare repo has no implicit trunk and must declare one explicitly via a `trunk = true` per-checkout **Repo override**. Managed worktrees fork from the trunk's HEAD; reconciling a completed worktree branch back into trunk is the human's own concern, not something pop does. An unconfigured bare repo has no trunk, so pop cannot provision a managed worktree there — it can only drain in place in whatever checkout the operator is currently sitting in.
