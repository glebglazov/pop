# The monitor dashboard configures itself under [monitor.dashboard]

The `[dashboard]` table holds `cursor_position`, `sort_criteria` and
`zoom_on_switch` — every one of them a setting of the **Dashboard**, the monitor
view. Its own schema description reads "Shared dashboard and cursor behavior",
which was never true, and pop has since grown `[work.dashboard]`, `[project]`
and `[worktree]` for the other dashboards. A reader adding a key had no way to
guess which table they wanted.

We renamed the table to `[monitor.dashboard]`, moving all three existing keys,
and kept `[dashboard]` as a deprecated alias in the manner of `[select]` →
`[project]` and `[tasks]` → `[work]`. The new `kill_pane_prompt_enabled` key
(ADR-0205) lands there rather than in `[dashboard]`, and carries a `desc:` tag
like its neighbours so the Config dashboard can edit it on `alt+c` — a
confirmation someone wants switched off is exactly what one reaches for
interactively.

## Considered options

**Add `[monitor.dashboard]` for the new key only**, leaving the other three
behind. Rejected outright: two tables configuring one dashboard, split by
nothing but arrival date, is the confusion this ADR is meant to remove.

**Leave everything in `[dashboard]`.** The honest small answer, and it costs
nothing today; rejected because the name gets more misleading with every
dashboard pop adds, and the deprecation machinery for doing it properly already
exists.

## Consequences

Existing `config.toml` files keep working through the alias, so the rename is
invisible until someone reads the example config. When a file sets the same key
in both tables the new one wins, per the resolution `projectConfig()` already
uses for `[project]`/`[select]`: return the new table when present, else fall
back. The cost is one more deprecated table to carry.
