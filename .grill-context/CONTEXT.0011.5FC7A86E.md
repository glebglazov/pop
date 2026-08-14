---
fragment: 5FC7A86E
generation: 0011
branch: master
---

+ Flash message
  A transient one-line message a TUI shows after an action, occupying the
  **Frame**'s hints region for three seconds before the hints return. Every
  Frame-based view shares one expiry rule and one timer type, so a message's
  lifetime never depends on the view's own poll interval.
  avoid: status message, toast, notice, status line
  under: Pickers

~ Frame
  The shared screen-chrome module the budgeted list views stand on: from one declaration of which regions are present (update notice, header, input box, warnings, footnote, block, hints) it both computes the body height the caller may fill and renders the header/footer around a caller-supplied body string. The single region declaration feeds budget and render together, so the reserved-line count can no longer drift from the view the way the hand-counted `Height-N` magic numbers did. Render is bottom-anchored: the body is padded to its full budget, so trailing regions sit at the terminal bottom even when the body is short. Warnings are reserved like any other region; the body is floored so it never collapses. Transient action feedback is not a region of its own — a **Flash message** takes over the hints region for three seconds and then yields it back. Pairs with **List**: List owns the body (rows, cursor, anchor), Frame owns everything around it. The hints region advertises the **Help binding** (`C-h help`) on surfaces that support a **Help overlay**.
  was: The shared screen-chrome module the budgeted list views stand on: from one declaration of which regions are present (update notice, header, input box, warnings, status, hints) it both computes the body height the caller may fill and renders the header/footer around a caller-supplied body string. The single region declaration feeds budget and render together, so the reserved-line count can no longer drift from the view the way the hand-counted `Height-N` magic numbers did. Render is bottom-anchored: the body is padded to its full budget, so trailing regions (warnings, status, hints) sit at the terminal bottom even when the body is short — an empty-list hint no longer pulls the status line up under the header. Warnings are reserved like any other region; the body is floored so it never collapses. Pairs with **List**: List owns the body (rows, cursor, anchor), Frame owns everything around it. The hints region advertises the **Help binding** (`C-h help`) on surfaces that support a **Help overlay**.

~ Dashboard
  The presentation of the monitored set of panes — a browsable view of registered panes, their status, and visit times. `pop monitor dashboard` opens this view; `pop dashboard` is only a hidden compatibility alias. It is also where a monitored pane is destroyed: the monitored set is the whole target set, so what the view lists is what `ctrl+x` may kill. Configured by the `[monitor.dashboard]` table.
  was: The presentation of the monitored set of panes — a browsable view of registered panes, their status, and visit times. `pop monitor dashboard` opens this view; `pop dashboard` is only a hidden compatibility alias.
