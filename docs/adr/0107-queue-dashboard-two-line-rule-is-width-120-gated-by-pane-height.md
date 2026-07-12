---
status: accepted
---

# Queue dashboard two-line rule is width<120 gated by pane height

Supersedes [ADR-0097](0097-queue-dashboard-two-line-rows-below-width-threshold.md).

The **Queue dashboard** task-set table renders every row as **two lines** when a trigger fires and the pane is tall enough to afford it; otherwise every row is a single line. The decision is global and uniform — all rows share the same height so scroll and cursor math stay uniform ([ADR-0079](0079-shared-tui-list-foundation-is-for-selectable-lists.md)).

Two triggers, not equal in kind:

- **Fit — terminal width < 120 columns** (raised from ADR-0097's 80). The columns genuinely do not fit on one line at modern content width — colored **Task set status** tokens plus their suffixes (`verified @ <sha>`, `orphaned`) need room. A pane under 120 cols is narrow *for the content*, so two-line is forced.
- **Readability — any visible Task set identifier exceeds 36 characters.** Discretionary: two-line shows the full set id instead of ellipsizing it. Global, so one long id would otherwise double every row's height.

Both triggers are then gated by pane height: two-line engages only when the terminal is **roomy** (height at or above a low floor, ~16 rows). Below the floor — a small **tmux-popup** — the dashboard stays single-line even when narrow or carrying a long id, trading id completeness and column breathing room for visible-row density. Any real laptop terminal (40–70 rows) clears the floor, so the collapse only ever bites genuinely tiny panes.

```
roomy       := termHeight >= HEIGHT_MIN         // ~16; tiny popups fall below
needTwoLine := termWidth < 120 || anyLongSetID(>36)
twoLine     := needTwoLine && roomy
```

Row layout in two-line mode (correcting ADR-0097's now-stale text, which the status-suffix commits reworked): **line 1** carries PROJECT / full TASK SET id / WORKTREE / DRAIN; **line 2** carries STATUS, indented to align under the TASK SET column. There is no longer a FLAGS column — Auto-drain rides the STATUS cell as a suffix ([ADR-0108](0108-auto-drain-marker-silenced-on-picked-up-rows.md)).

## Why

ADR-0097 keyed only on width (`< 80`) and set-id length, and never looked at height. In a short **tmux-popup** — the common way this dashboard is launched over `~/.local/share/chezmoi` — a single long set id flips every row to two lines and halves the visible-row count, which is the wrong trade when vertical space is the scarce resource. The width floor of 80 was also set before the STATUS cell gained color and suffixes; 120 reflects the wider content. `m.height` was already captured from `WindowSizeMsg` (it arrives correctly inside a tmux popup) — the old rule simply never consulted it.

## Considered Options

- **Height vetoes only the readability trigger, never the width<120 fit trigger.** Rejected: in a short *and* narrow popup you would still be forced to two lines, halving density in exactly the case where density matters most. The user chose density there, accepting truncated ids and cramped columns.
- **Per-row variable height** (expand only the cursored or long-id rows). Rejected as in ADR-0097: inconsistent row heights break scanning and the List's uniform-height budget.
- **A height floor high enough to catch laptops.** Rejected: the floor is a safety net for pathological tiny panes, not a density policy for normal terminals; set low (~16) so it never fires on a real 15-inch display.

## Consequences

- On a short, narrow popup the table is single-line with ellipsized long ids and tight columns — deliberate; density wins when height is scarce.
- `HEIGHT_MIN` is a guard, not a tuning knob for everyday use; users with normal terminals never observe it.
- The width bump to 120 makes medium panes (80–119 cols) go two-line where they previously stayed single-line — accepted, since colored+suffixed status needs the room.
