---
fragment: 3809669e
generation: 0012
branch: master (text-field consolidation)
---

+ Text field
  The house single-line editable input: a Model-shaped embeddable component (rune buffer, block cursor, house prompt glyph `❯ `) hand-rolled on raw bubbletea, `ui.TextField`. It owns an emacs-style editing keymap (arrows, home/end, backspace, clear) as a default callers may preempt by intercepting their own reserved keys first. It is the single house config point for text entry, replacing the retired `newTextInput()` bubbles wrapper. Distinct from the bordered **input box** (`WriteInputBox`), which is chrome that wraps a Text field.
  avoid: text input, input field, line editor
  under: UI
