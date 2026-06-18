---
fragment: c25a1cd7
generation: 0002
branch: master
---

~ Queue
  The scheduling concern over Task-set draining across repositories, surfaced by two drivers: the **Queue daemon** (`pop queue run`, automatic, polls and fans out unattended) and the **Queue dashboard** (`pop queue dashboard`, manual, the primary way a human starts drains). Both schedule onto the same substrate — **Repository identity** as the unit, **Worktree binding** as the per-set drain router, the **Integration backlog** for reconciliation — and both collapse a repo's worktrees to one scheduling unit. Global cross-project priority ordering remains a non-goal.
  was: A daemon that supervises per-repository Task-set draining, fanning `pop tasks implement <set>` runs out concurrently across registered repositories into tmux.

+ Queue dashboard
  The interactive cross-repository cockpit (`pop queue dashboard`) for starting and reconciling drains by hand — the primary Queue driver. It lists every live Task set across registered repositories grouped by **Repository identity** (drainable Ready/Failed/Done-awaiting-integration rows bright, **Blocked** rows shown dim), one row per set, showing project, set name, **Task set status**, **Task set priority**, the set's **Worktree binding** rendered `<worktree>(<branch>)` — styled three ways: bound/in-progress, will-reuse-trunk, or will-provision-new managed worktree — and an extra-info cell carrying per-row qualifiers (**Mergeability** clean/conflict for integrateable rows, blocked reason, failed task). Row actions, each an in-place keybinding: `i` drain (detached spawn of plain `pop tasks implement <set>` into the project's **Queue window**, row flips to picked-up; confirms first only when it will provision a new managed worktree), `I` integrate (in-place wizard, when in the **Integration backlog**), `U` **Unbind worktree**, `b` rebind to a different worktree of the repo (every git worktree of the repo plus a provision-new option), `s` show per-set task detail overlay, `Enter` jump to the running drain pane. A read-only single-repository slice of the same shape is folded into `pop tasks status`. It does not replace the **Queue daemon**; the two coexist.
  under: Tasks
