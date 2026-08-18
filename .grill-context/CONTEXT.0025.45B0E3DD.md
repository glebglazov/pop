---
fragment: 45B0E3DD
generation: 0025
branch: master
---

+ Scroll edge
  The count a boundary line carries for the rows hidden past it — `↑ 4` where rows have
  scrolled off above, `↓ 3` below — owned by **List** so every surface standing on it says
  it the same way. It rides chrome that already exists wherever there is any (the Work
  dashboard's table rule, the **Dashboard**'s header row, the rule closing the **Selection
  area**, which carries one count at each end for the two lists it divides) and spends a
  line of its own only where there is none. It exists because a reserved region and a
  bottom-anchored **Action menu** both take lines out of the scrolling area, so rows now
  leave the viewport for reasons the human did not initiate; a list that scrolls silently
  cannot tell them apart from rows that are not there.
  avoid: scroll indicator, more-rows marker, overflow line

~ Selection area
  The reserved region at the **foot** of a dashboard holding every selected row, parked at
  the bottom of the viewport whatever the list's length, below the ordinary rows and
  divided from them by a dim `N selected` rule. It is what makes a **Selection** exempt
  from hiding: a selected row stays on screen whatever the **Work view preset** or the
  **Work dashboard search** says, so no selected row is ever an invisible target and no
  count of hidden members needs reporting. Rows sit in the list's own sort and kind
  precedence, not in marking order, and are moved rather than copied. The cursor never
  lands there by default or after a rebuild, though `j`/`k` walk into it freely. Capped at
  a third of the viewport, the members the cap leaves out counted by a **Scroll edge** at
  whichever boundary hides them. It sits at the foot because a region at the head displaces
  the list the eye is working in every time a row is marked; at the foot it grows into
  chrome instead, and the ordinary rows do not move.
  was: The reserved region at the top of a dashboard holding every selected row, above the
    **Pane pin** block and separated from it by a dim `N selected` line. It is what makes a
    **Selection** exempt from hiding: a selected row stays on screen whatever the **Work
    view preset** or the **Work dashboard search** says, so no selected row is ever an
    invisible target and no count of hidden members needs reporting. Rows sit in the list's
    own sort and kind precedence, not in marking order, and are moved rather than copied.
    The cursor never lands there by default or after a rebuild, though `j`/`k` walk into it
    freely. Capped at a third of the viewport, overflowing into a `… +N more selected` line
    that shortens the rendering without narrowing what a verb targets.

+ Action menu
  The dashboard's list of the verbs its current target offers, rendered as bottom chrome
  through **Frame**'s reserved block — the same mechanism the **Work view preset** menu
  already uses — rather than spliced into the table beside a row. Its top rule names what it
  acts on (`actions · orders-api`, `actions · 6 selected`), which is what lets it sit
  nowhere near its target; opening it shrinks the list body by exactly its height, so rows
  leave from the top and scroll back when it closes. One position for every menu on the
  surface, the detail view's item menus included: where the actions are is a fact about the
  dashboard, not about which row or how many are marked.
  avoid: verb menu, action overlay, command palette

- Pinned action menu

~ Pane pin
  The Work dashboard lifting the rows its launching pane is attributed to out of the
  ordered list and rendering them at the head of it, marked `▸` in the prefix column the
  cursor already shares. It is uncontested there — the **Selection area** sits at the foot —
  so the head of the list answers "which of these is me?" and nothing else. Applied after
  the sort resolves rather than as a sort term, so it can raise a Map row above the whole
  task-set block — which no comparator can do, rows never being ordered across kinds — while
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

~ Navigation
  The dashboard keys that move or reframe the view rather than changing anything —
  cursor movement, `gg`/`G`, search, presets, the page toggle, help. Always live, in every
  mode, which is what keeps **Selection mode** a retarget of the verbs rather than a
  freeze of the surface. Its jump keys are region-aware once a **Selection area** exists,
  mirrored to the region's place at the foot: from the ordinary rows `G` reaches the last
  of them, just above the divider, and only a second press crosses into the area; `gg` from
  inside the area reaches the area's first row before the list's top. One press per
  crossing, so the divider is never jumped by accident.
  was: The dashboard keys that move or reframe the view rather than changing anything —
    cursor movement, `gg`/`G`, search, presets, the page toggle, help. Always live, in every
    mode, which is what keeps **Selection mode** a retarget of the verbs rather than a
    freeze of the surface. Its jump keys are region-aware once a **Selection area** exists:
    `G` reaches the bottom of the region the cursor is in and only then the bottom of the
    whole list, `gg` the same in reverse.
