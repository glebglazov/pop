---
fragment: a8f3c2e1
generation: 0015
branch: master
---

+ Integrate outcome line
  One stdout line per successful or skipped integrate action, naming what changed. File-based **Integration components** emit one line per resolved installed skill (not one line per component bundle); **Status wiring** stays one line per agent with no skill name. Labels (`added`, `updated`, `skipped (conflict at …)`, `skipped (opted out)`, `removed (opted out)`, etc.) attach to that named unit — same per-skill granularity for skips and removals as for adds and updates. The named skill is the **resolved install name** — what appears at the agent's skill location after **Skills prefix** is applied — not the **Integration skill alias** or embed base alone.
  avoid: component outcome, integrate row
  under: Agent integrations

+ Stale skill removal line
  An **Integrate outcome line** emitted when **Stale agent entry cleanup** deletes a pop-owned skill whose resolved install name is no longer expected — e.g. after a **Skills prefix** change (`pop-tmux-pane` → `tmux-pane`) or **Integration component id** rename. Label: `removed (stale)`. Distinct from `removed (opted out)`.
  avoid: pruned line, stale prune report
  under: Agent integrations

~ Integration component id
  The stable slug naming one **Integration component** in pop's machine-facing contract: `status-wiring`, `pane-skills`, `task-skills`. Used for CLI flags (`--no-pane-skills` only — the old `--no-pane-skill` flag is not accepted), render-tree directory names under `$XDG_DATA_HOME/pop/integrations/<agent>/`, Doctor evidence keys, and catalog lookup — not for individual installed skill names (`tmux-pane`, `grill-with-docs`, …). Skill-bundle components use plural ids; **Status wiring** stays singular because it is hooks/plumbing, not a skill set.
  was: (see fragment above — plural rename; alias flag not yet decided)

~ Pane skill
  The embedded skill that teaches an agent to drive `pop pane`. Installed via the **Integration component id** `pane-skills` (one resolved skill, typically `tmux-pane` when **Skills prefix** is empty). Still selected in config via the **Integration skill alias** `pane`.
  was: (see CONTEXT.0014 — embed base tmux-pane; alias pane; component framing only)

~ Doctor rendering
  The terminal presentation of **Doctor**. Integrate-family sub-checks for file-based components name the **resolved install name** (same as **Integrate outcome line**), not the **Integration component id**; **Status wiring** checks stay at component level (`<agent> status-wiring`). Otherwise unchanged: scannable ANSI, one row per command family, terse checks beneath.
  was: (see CONTEXT.md — general presentation rules only; no integrate naming convention)

+ Integrate outcome ordering
  **Integrate outcome line**s group by agent (existing configured agent order). Within an agent: **Status wiring** first, then file-based skills in embed **catalog source order** (`tmux-pane`; then `grill-with-docs`, `grill-consolidate`, `to-prd`, `to-tasks`). For each embed base, emit any **Stale skill removal line** for superseded resolved names immediately before that base's current line — so `pop-grill-consolidate  removed (stale)` sits next to `grill-consolidate  updated`, not in a separate trailing block.
  avoid: alphabetical integrate output, sort by label
  under: Agent integrations
