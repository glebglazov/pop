---
status: accepted
---

# House `C-h` help overlay is centralized across TUIs

## Context

List-based TUIs in pop exposed bindings inconsistently. Project and worktree pickers
and the monitor dashboard used F1 for a full **Help overlay**; the queue dashboard,
configure picker, name prompt, and multi-select had only one-line footer hints (or
nothing discoverable). F1 is awkward on Mac laptops (Fn+F1), and footer hints on
binding-heavy surfaces like the queue dashboard truncate the long tail.

This follows the same house-foundation pattern as [ADR-0079](0079-shared-tui-list-foundation-is-for-selectable-lists.md)
and [ADR-0081](0081-house-text-field-consolidates-single-line-input.md): one shared
`ui` idiom instead of per-surface drift.

## Decision

Every list-based TUI except the ephemeral **Error screen** gains a contextual
**Help overlay** toggled by a single house **Help binding**: `ctrl+h` (displayed
`C-h`). F1 is retired. A second `C-h` or Esc dismisses; other keys are swallowed
while the overlay is open.

Shared infrastructure lives in `ui`:

- `ui.HelpKeys` — one `key.Binding` for `ctrl+h`
- `ui.RenderHelpOverlay` — aligned columns, input-box chrome, footer (`C-h toggle · Esc close`)
- Each surface supplies a contextual entry table for its present mode (main list,
  queue action menu, filter, modal, configure phase, etc.) with a header naming the
  mode

Surfaces in scope: project picker, worktree picker, monitor dashboard, queue
dashboard (all sub-modes), configure picker, name prompt, multi-select. The error
screen keeps its existing footer hint only.

Callers intercept `ui.HelpKeys` before `TextField` (or other input) handling, so
`C-h` opens help even on surfaces with an always-on filter input.

Non-US keyboard layouts are out of scope for now; `C-h` is chosen as a
layout-neutral mnemonic (Emacs/help tradition) rather than `C-?`, which is
US-layout-centric and would steal `?` from picker filter input if bound bare.

## Considered options

- **Keep F1.** Rejected — Mac Fn friction was the original complaint; no reason to
  keep a second chord.
- **`C-?` / `ctrl+shift+/` (with `ctrl+?` alias).** Rejected — intuitive on US
  QWERTY but layout-coupled elsewhere; bare `?` cannot be bound without blocking
  filter input in pickers even when intercepted first (product expectation: search
  for `?` in paths).
- **Footer hints only on binding-heavy surfaces.** Rejected — queue dashboard
  remains undiscoverable; inconsistent with pickers that already had overlays.
- **One exhaustive binding list per surface regardless of mode.** Rejected in favor
  of contextual lists — action menus and modals expose different letters; a single
  mega-list adds noise.
- **Per-surface `viewHelp()` with only a shared binding constant.** Rejected —
  layout would drift across five+ copies (the problem ADR-0079 solved for lists).

## Consequences

- Glossary: adds **Help overlay** and **Help binding**; extends **Frame** hints to
  advertise `C-h help` on overlay-capable surfaces.
- Implementation replaces duplicated `viewHelp()` in picker and monitor dashboard,
  adds overlay plumbing to queue dashboard / configure picker / name prompt /
  multi-select, and removes F1 bindings and hint text.
- Future i18n for non-US layouts is a separate pass (possibly `BaseCode`-aware
  matching when Kitty protocol is available); not part of this decision.
