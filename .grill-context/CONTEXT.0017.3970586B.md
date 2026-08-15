---
fragment: 3970586B
generation: 0017
branch: master
---

+ Work dashboard search
  The name query opened with `/` on either **Work page**: a committed,
  case-insensitive substring match over a row's project or ID that narrows the
  rows the active **Work view preset** already selected. Stateful — Enter applies
  it and returns the full keymap, so navigation and every action key work on the
  narrowed rows; the term persists across polls, preset changes and page
  switches, and is shown in the page header beside the preset. Reopening `/`
  starts from an empty buffer, so applying an empty query clears the search;
  Esc abandons the edit and restores the term that was in force. Per-page and
  session-only. Distinct from the **Work dashboard filter menu**, which chooses
  *which rows exist*; a search only subtracts from what the preset already built.
  avoid: filter, fuzzy text filter, `/` filter, quick filter, live filter query
  under: Work dashboard

+ Text entry mode
  A TUI mode whose keyboard belongs to a `ui.TextField`: it may reserve **only
  keys that produce no text** — arrows, `tab`, `pgup`/`pgdn`, `ctrl+n`/`ctrl+p`,
  `ctrl+c`, plus its commit and cancel keys — and forwards everything printable
  to the field. Every letter and digit is text, always, so no host may steal one;
  `j` and `k` taken for navigation, which made `jj` unsearchable, is the case
  that named the rule. That is a ceiling, not a quota: a host reserves the least
  it can, so **Work dashboard search** takes only Enter, Esc and `ctrl+c` because
  a committed search has no reason to navigate mid-type, while an incremental
  picker legitimately reserves arrows it cannot function without. Word-level
  editing (`ctrl+w`, `alt+b`/`alt+f`, `alt+d`, `ctrl+k`) belongs to the shared
  field, never to a host.
  avoid: filter mode, input focus, typing state, per-surface reserved keys
  under: TUI

~ Pane pin
  The Work dashboard lifting the rows its launching pane is attributed to out of the
  ordered list and rendering them first, marked `▸` in the prefix column the cursor
  already shares. Applied after the sort resolves rather than as a sort term, so it can
  raise a Map row above the whole task-set block — which no comparator can do, rows
  never being ordered across kinds — while leaving every ordering rule beneath it
  untouched. Re-derived on every rebuild from pane facts read once at launch, so a pin
  may appear, move or vanish mid-session; that is feedback on the human's own act of
  starting a Drain or binding a set, not a target chasing their navigation, because it
  never moves the cursor. Pinned rows are moved, not copied, and scroll away like any
  other row. It is wholly silent: a row a **Work view preset** or a **Work dashboard
  search** hides is not pinned and nothing is printed, because with no cursor placed
  there is no unexplained state left to caption. Carries no opt-out — the first
  keypress is the opt-out.
  avoid: pane-seeded cursor, sticky row, follow mode, cursor sync, reveal, jump-to-row
  was: … It is wholly silent: a row a **Work view preset** or filter query hides is
    not pinned and nothing is printed … (generation 0013; amended only to retire
    "filter query" for the renamed **Work dashboard search**).

~ Work dashboard filter menu
  The modal popup opened with `f` on the **Work dashboard** (page A), holding a
  single-select numbered list of **Work view preset**s. Pressing `1`–`9` or j/k
  plus Enter activates one preset and rebuilds the rows immediately; exactly one
  is active, and its name is always shown in the page header. Session-only —
  resets to the default preset on relaunch. Distinct from **Work dashboard
  search** (`/`), which subtracts by name from the rows this menu's preset
  already selected; the chrome keeps "filter" for this menu and "search" for `/`
  so the two narrowings never share a word. Page B (Routines) does not offer it.
  was: The modal popup opened with `f` on the **Work dashboard** (page A),
    holding a single-select numbered list of **Work view preset**s. Pressing
    `1`–`9` or j/k plus Enter activates one preset and rebuilds the rows
    immediately; exactly one is active, and its name is always shown in the page
    header. Session-only — resets to the default preset on relaunch. Distinct
    from `/`, the fuzzy text filter, which is a transient query over
    already-included rows. Page B (Routines) does not offer it.
