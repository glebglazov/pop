---
fragment: 4E71C0D3
generation: 0058
branch: master
---

~ Attended session choice
  The whole Agent entry a human selects with Tab for one interactive run. It takes precedence over the attended agent flag, ends when that run ends, and changes neither saved config nor another session. A Work dashboard is one such run: a choice made in its Run menu is held by the dashboard, rides out as the attended spec on every attended launch it makes, and ends when the dashboard is quit (ADR-0269).
  was: The whole Agent entry a human selects with Tab for one interactive run. It takes precedence over the attended agent flag, ends when that run ends, and changes neither saved config nor another session.
  under: Task execution

~ Attended entry render
  The agent and model shown before an attended launch, resolved from its Attended session choice, attended agent flag, or configured default in that order. A row that names the entry also offers Tab to change it, wherever at least two valid entries can be chosen: a gate menu's assist row, and a Work dashboard's attended action rows inside the Run menu, singular and plural alike (ADR-0269). The dashboard's `alt+a` chord is retired, and the Work dashboard's persistent subheader still names the Config dashboard's key rather than a chooser of its own, being the one surface that renders no launch.
  was: The agent and model shown before an attended launch, resolved from its Attended session choice, attended agent flag, or configured default in that order. Gate rows offer Tab where at least two valid entries can be chosen; dashboard rows display the entry but offer no agent chooser.

~ Agent override
  A persisted replacement of one Work agent group's ordered agents list in the Config override layer, edited through the Config dashboard, which is its only writer (ADR-0269 withdraws the narrow chooser-writer ADR-0264 admitted). An explicit agent flag takes precedence for its invocation; for attended launches, an Attended session choice takes precedence over both. Removing an override restores the source list.
  was: A persisted replacement of one Work agent group's ordered agents list in the Config override layer, edited through the Config dashboard. An explicit agent flag takes precedence for its invocation; for attended launches, an Attended session choice takes precedence over both. Removing an override restores the source list.
