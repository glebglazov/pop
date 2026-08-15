---
fragment: F3BEE533
generation: 0013
branch: master
---

+ Pane pin
  The Work dashboard lifting the rows its launching pane is attributed to out of the
  ordered list and rendering them first, marked `▸` in the prefix column the cursor
  already shares. Applied after the sort resolves rather than as a sort term, so it can
  raise a Map row above the whole task-set block — which no comparator can do, rows
  never being ordered across kinds — while leaving every ordering rule beneath it
  untouched. Re-derived on every rebuild from pane facts read once at launch, so a pin
  may appear, move or vanish mid-session; that is feedback on the human's own act of
  starting a Drain or binding a set, not a target chasing their navigation, because it
  never moves the cursor. Pinned rows are moved, not copied, and scroll away like any
  other row. It is wholly silent: a row a **Work view preset** or filter query hides is
  not pinned and nothing is printed, because with no cursor placed there is no
  unexplained state left to caption. Carries no opt-out — the first keypress is the
  opt-out.
  avoid: pane-seeded cursor, sticky row, follow mode, cursor sync, reveal, jump-to-row
  under: Language

- Pane-seeded cursor

- Bound-checkout attribution tiebreak

+ Bound-checkout pin order
  Which of several Task sets bound to one checkout leads the **Pane pin** when a bare
  shell in that checkout attributes to all of them: the **Checkout claim** holder while
  something is live there, then the set drained most recently, then the topmost bound
  row under the active sort. Since every candidate pins, this only decides which one
  leads — being wrong costs second place, not a wrong answer — so the sub-ladder no
  longer has to be defended in prose.
  under: Language

~ Pane work attribution
  Which Work container the pane a read surface was launched in belongs to — the question
  every other derivation asks backwards, from a known container to its pane. It is a
  first-hit ladder over what the pane can show, strongest rung first: the pane's own
  `@pop_*` tag naming a Task set, a **Decision ticket** (which resolves to its Map) or a
  Routine, then the **Work session** stamp naming a Map. The top rungs mean "this pane
  *is* one pop opened for that work" and are unambiguous. Below them the ladder falls
  back to where the pane is merely *standing*: the live **Drain** whose checkout contains
  the pane's directory, which names its set outright, and last the checkout itself plus
  the Task sets bound to it — the rung that fires for the ordinary editor shell the human
  opened themselves, which is where they are when they want this. Directories are matched
  canonically and by containment, deepest checkout first. Routines stop at the tag rung:
  being project-scoped rather than checkout-scoped, a neighbourhood rung would answer with
  a project's whole routine list for any shell in the repo. One checkout can hold several
  bound sets and pop records no per-set recency, so that last rung attributes the pane to
  all of them and lets **Bound-checkout pin order** decide which leads. The ladder is
  answered kind-side behind the **Work seam**, obtained by type assertion the way an
  advanceable kind is, and resolved while the snapshot is being built — where each kind
  already holds the rows it needs, the hidden ones included. The pane's facts (pane id,
  session, directory, every tag, the session stamp) are read once at launch in one
  `display-message` round-trip and carried, never re-read. Its one consumer today is the
  **Pane pin** on both Work dashboard pages, and it never announces itself: attributed
  means pinned, and a pane attributed to nothing — or to a row the active view excludes —
  says nothing at all and widens nothing (ADR-0201, ADR-0209).
  avoid: pane ownership, current work detection, pane-to-set lookup, cursor memory
  was: Which Work container the pane a read surface was launched in belongs to … Its one
    consumer today is the **Work dashboard**'s cursor: opening from an attributed pane
    lands on that container's row, once, at first render … Whenever there was more than
    one candidate the choice is named in the status line — which set, out of how many,
    and why … an attributed container whose row a **Work view preset** or a live filter
    query excludes is named in the status line with the reason, and the view is never
    widened to reveal it (ADR-0201).
