---
fragment: AABB7FBA
generation: 0001
branch: master
---

+ Help overlay
  A modal layer listing every binding active in the current TUI surface; `C-h` toggles it (a second press closes as well as opens) and Esc also dismisses. Other keys are swallowed while it is open. Bindings shown are **contextual** — only what applies in the surface's present mode (main list, action menu, filter, modal, configure phase, etc.), with a header naming the mode. Layout and the **Help binding** live in shared `ui` infrastructure (`ui.HelpKeys`, `ui.RenderHelpOverlay`); each surface supplies only its contextual entry table so binding and render cannot drift apart.
  avoid: help screen, help mode, F1 screen
  under: Pickers

+ Help binding
  The house chord that opens the **Help overlay** on any list-based TUI: `ctrl+h` (displayed `C-h`). Replaces F1. Non-US keyboard layouts are out of scope for now. The **Error screen** skips the overlay — its footer hint already lists every binding.
  avoid: F1, C-?, help key
  under: Pickers

~ Frame
  The shared screen-chrome module the budgeted list views stand on: from one declaration of which regions are present (update notice, header, input box, warnings, hints) it both computes the body height the caller may fill and renders the header/footer around a caller-supplied body string. The single region declaration feeds budget and render together, so the reserved-line count can no longer drift from the view the way the hand-counted `Height-N` magic numbers did. Warnings are reserved like any other region; the body is floored so it never collapses. Pairs with **List**: List owns the body (rows, cursor, anchor), Frame owns everything around it. The hints region advertises the **Help binding** (`C-h help`) on surfaces that support a **Help overlay**.
  was: The shared screen-chrome module the budgeted list views stand on: from one declaration of which regions are present (update notice, header, input box, warnings, hints) it both computes the body height the caller may fill and renders the header/footer around a caller-supplied body string. The single region declaration feeds budget and render together, so the reserved-line count can no longer drift from the view the way the hand-counted `Height-N` magic numbers did. Warnings are reserved like any other region; the body is floored so it never collapses. Pairs with **List**: List owns the body (rows, cursor, anchor), Frame owns everything around it.
