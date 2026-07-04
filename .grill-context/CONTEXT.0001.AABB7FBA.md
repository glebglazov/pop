---
fragment: AABB7FBA
generation: 0001
branch: master
---

+ Help overlay
  A modal layer listing every binding active in the current TUI surface; Esc dismisses, other keys are swallowed while it is open. Bindings shown are **contextual** — only what applies in the surface's present mode (main list, action menu, filter, modal, configure phase, etc.), with a header naming the mode.
  avoid: help screen, help mode, F1 screen
  under: Pickers

+ Help binding
  The house chords that open the **Help overlay** on any list-based TUI: `ctrl+shift+/` (displayed `C-?`), with `ctrl+?` as a terminal alias, plus `ctrl+h` (`C-h`) as a layout-neutral fallback. Replaces F1. Bare `?` is intentionally not bound so pickers can accept `?` in filter input. Non-US keyboard layouts are out of scope for now — the `C-?` label is a US-layout mnemonic.
  avoid: F1, help key
  under: Pickers

~ Frame
  The shared screen-chrome module the budgeted list views stand on: from one declaration of which regions are present (update notice, header, input box, warnings, hints) it both computes the body height the caller may fill and renders the header/footer around a caller-supplied body string. The single region declaration feeds budget and render together, so the reserved-line count can no longer drift from the view the way the hand-counted `Height-N` magic numbers did. Warnings are reserved like any other region; the body is floored so it never collapses. Pairs with **List**: List owns the body (rows, cursor, anchor), Frame owns everything around it. The hints region advertises the **Help binding** (`C-? help`) on surfaces that support a **Help overlay**.
  was: The shared screen-chrome module the budgeted list views stand on: from one declaration of which regions are present (update notice, header, input box, warnings, hints) it both computes the body height the caller may fill and renders the header/footer around a caller-supplied body string. The single region declaration feeds budget and render together, so the reserved-line count can no longer drift from the view the way the hand-counted `Height-N` magic numbers did. Warnings are reserved like any other region; the body is floored so it never collapses. Pairs with **List**: List owns the body (rows, cursor, anchor), Frame owns everything around it.
