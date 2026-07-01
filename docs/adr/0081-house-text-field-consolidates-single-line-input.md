---
status: accepted
---

# A hand-rolled house Text field consolidates single-line input

## Context

pop had two idioms for the same job — "editable single line." One hand-rolled on
raw bubbletea (`ui/nameprompt.go`, a rune buffer backing the worktree-name step of
[ADR-0076](0076-worktree-picker-owns-interactive-creation.md)); one via
`charm.land/bubbles/v2/textinput` (`newTextInput()` in `ui/util.go`, used by the
picker filter, the configure picker, and the queue dashboard filter). ADR-0076's
"considered options" had already rejected `bubbles/textinput` and asserted pop
"imports zero bubbles components," so the codebase contradicted its own record.

This is the text-input analogue of the shared list foundation
([ADR-0079](0079-shared-tui-list-foundation-is-for-selectable-lists.md)): one house
idiom instead of two.

## Decision

Single-line text entry is a hand-rolled house component, `ui.TextField`: a
Model-shaped **embeddable** widget (not a standalone Program) holding a rune
buffer, a block cursor, and the house prompt glyph `❯ `. It owns an emacs-style
editing keymap (arrows, `ctrl+b`/`ctrl+f`, home/end incl. `ctrl+a`/`ctrl+e`,
backspace, `ctrl+u` clear) as a **default** — callers intercept their own reserved
keys first and forward only the remainder to the field, which is why the field
owning `ctrl+a`→home does not collide with ADR-0076's `ctrl+a`=create-worktree
binding. It emits no `tea.Cmd` (no blink). It is the single house config point for
text entry, retiring `newTextInput()`.

The four `textinput` sites migrate to `ui.TextField`; `nameprompt` keeps its domain
wrapper (base hint, empty→default fallback, `PromptName` Program) and swaps only its
buffer guts for the shared field. The bordered **input box** (`WriteInputBox`) is
unchanged and continues to wrap the field where callers want a border.

We keep `charm.land/bubbles/v2/key` and therefore `bubbles/v2` stays in go.mod, so
this is a **consistency** decision, not dependency-shedding. `key` is a keymap
helper (`key.Binding` + `key.Matches`) that renders nothing — it is not a component,
so keeping it does not reintroduce the "zero rendered bubbles components" boundary
ADR-0076 asserts. This decision reinforces and extends that boundary rather than
reversing it.

The governing principle: **own the domain-entangled; outsource the
generic-and-self-contained only when a library earns real leverage.** pop's lists
are complex *and* domain-entangled (identity reload, fzf matching, quick-access,
anchoring) → hand-rolled (ADR-0079). A single-line field is generic and
self-contained but small enough that a library earns almost nothing — it clears no
leverage bar — so it is hand-rolled for house consistency. `key` is likewise
generic and self-contained, but hand-rolling it earns no leverage either, so it is
kept.

## Considered options

- **B — bubbles everywhere.** Delete the hand-rolled `nameprompt` editor and
  standardize on `textinput`. Least code to own, and the outsource-generic principle
  superficially favors it. Rejected: it reverses ADR-0076, and it rests a core UI
  idiom on `bubbles/v2 v2.0.0-rc.1`, a release-candidate (unstable) API.
- **Shed rc-`bubbles` entirely.** Also replace `key` with a local keymap helper so
  `bubbles/v2` leaves go.mod. Rejected as out of scope: `key` is generic,
  self-contained, and renders nothing; hand-rolling it earns no leverage, and the rc
  concern does not by itself justify the churn across seven files.

## Consequences

- The glossary gains **Text field** (`ui.TextField`), distinct from the bordered
  **input box**. `text input` / `input field` are on the avoid list.
- The prompt glyph converges on `❯ `; the three filter sites shift `> ` → `❯ `
  (cosmetic; the list cursor uses `█`, so no glyph collision).
- `bubbles/v2` remains a dependency via `key`; a future decision to shed the rc
  entirely would additionally need the `key` replacement.
