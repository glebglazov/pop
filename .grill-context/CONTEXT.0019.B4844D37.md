---
fragment: B4844D37
generation: 0019
branch: master
---

+ Selection
  The run-scoped set of rows a dashboard's next **Action** applies to, held in memory for
  one command invocation and never persisted. Each surface identifies its members by the
  row key it already has — a **Pane**'s pane id on the **Dashboard**, a Work container's
  cursor key on the **Work dashboard** — so a Selection survives the wholesale row rebuild
  every poll performs. One concept, worn by both dashboards; the members keep their own
  domain nouns rather than being flattened into a shared "unit".
  avoid: unit, marked set, checked rows, multiselect (that is the modal widget), batch

+ Selection mode
  The state a dashboard is in whenever its **Selection** is non-empty. Derived, never
  stored: the first `tab` enters it and clearing the last row leaves it, so there is no
  second piece of state to disagree with the first. In it every **Action** is plural and
  targets the Selection rather than the cursored row, announced by `-- SELECT --` at the
  left of the hint line in the accent colour the **Selection area** and the bulk
  confirmation share.
  avoid: bulk mode, multi mode, visual mode

+ Selection area
  The reserved region at the top of a dashboard holding every selected row, above the
  **Pane pin** block and separated from it by a dim `N selected` line. It is what makes a
  **Selection** exempt from hiding: a selected row stays on screen whatever the **Work
  view preset** or the **Work dashboard search** says, so no selected row is ever an
  invisible target and no count of hidden members needs reporting. Rows sit in the list's
  own sort and kind precedence, not in marking order, and are moved rather than copied.
  The cursor never lands there by default or after a rebuild, though `j`/`k` walk into it
  freely. Capped at a third of the viewport, overflowing into a `… +N more selected` line
  that shortens the rendering without narrowing what a verb targets.
  avoid: pinned block, selection pane, staging area, tray

+ Action capability
  The modes an **Action** declares itself to work in — singular, plural, or both —
  defaulting to singular so a verb becomes bulk-applicable only because someone wrote it
  down. It replaces the older habit of expressing such limits case by case on the surface:
  a hand-off verb is singular because it says so, not because the dashboard remembers to
  check. Declared as a field on `work.Action` for kind-owned verbs and as a mirrored table
  on the **Dashboard**'s keymap, which has no kind seam to hang it on. A verb the current
  mode disallows refuses out loud rather than going inert; **Navigation** is never gated
  by it.
  avoid: bulk flag, plural verb, batch capability

+ Navigation
  The dashboard keys that move or reframe the view rather than changing anything —
  cursor movement, `gg`/`G`, search, presets, the page toggle, help. Always live, in every
  mode, which is what keeps **Selection mode** a retarget of the verbs rather than a
  freeze of the surface. Its jump keys are region-aware once a **Selection area** exists:
  `G` reaches the bottom of the region the cursor is in and only then the bottom of the
  whole list, `gg` the same in reverse.
  avoid: movement keys, chrome keys

~ Pane pin
  The Work dashboard lifting the rows its launching pane is attributed to out of the
  ordered list and rendering them above it, below only the **Selection area** when one is
  open, marked `▸` in the prefix column the cursor already shares. Applied after the sort
  resolves rather than as a sort term, so it can raise a Map row above the whole task-set
  block — which no comparator can do, rows never being ordered across kinds — while
  leaving every ordering rule beneath it untouched. Re-derived on every rebuild from pane
  facts read once at launch, so a pin may appear, move or vanish mid-session; that is
  feedback on the human's own act of starting a Drain or binding a set, not a target
  chasing their navigation, because it never moves the cursor. Pinned rows are moved, not
  copied, and scroll away like any other row. It is wholly silent: a row a **Work view
  preset** or a **Work dashboard search** hides is not pinned and nothing is printed,
  because with no cursor placed there is no unexplained state left to caption — a rule
  that binds pop's own inference only, which is why a human's **Selection** may widen the
  same preset. Carries no opt-out — the first keypress is the opt-out.
  was: The Work dashboard lifting the rows its launching pane is attributed to out of the
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
