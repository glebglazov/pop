---
fragment: af293fd3
generation: 0001
branch: master
---

~ Queue dashboard
  The interactive `pop queue dashboard` TUI — the primary hands-on surface for starting and managing **Queue** work, sibling to the **Project picker** and **Worktree picker**. It is machine-global like `pop queue status`, scanning every registered repository's **Task storage** and rendering one row per non-archived **Task set** that still has outstanding queue-actionable state (grouped by project, sets newest-first by identifier), excluding only a concluded **Done Task set**. Each row shows the derived **Task set status** (folding **Mergeability** for **Integration backlog** rows), the set's branch — prefixed with a `↳` glyph when it runs in a non-trunk **Worktree binding**, unmarked when it runs in the **Representative checkout** — a live **Picked-up** drain indicator, and an **Auto-drain** badge; keys drain (`i`), integrate (`I`), bind or create a worktree (`b`), abandon (`U`), inspect (`s`), toggle **Auto-drain** (`a`), and preview the working pane (`p`). The full on-disk checkout path is not shown inline (it lives in the `s` inspect modal); an overflowing cell is ellipsis-truncated. It is a manual launcher running parallel to the **Queue daemon**'s **Auto-drain**-gated autopilot, not a replacement for it.
  was: ... the associated checkout as `<dir> (<branch>)` (bound worktree, else the resolved **Queue base**), a live **Picked-up** drain indicator, and an **Auto-drain** badge ... [full path shown inline; no overflow handling]
