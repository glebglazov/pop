---
fragment: D0AC6EB9
generation: 0062
branch: master
---

~ Worktree picker
  The fuzzy-search picker in `pop worktree dashboard` for choosing, creating, folding, or deleting git worktrees in the current repository. It lists every checkout git reports and marks each one managed-bound, managed-unbound, or ordinary. Interactive creation is in scope (`ctrl+a`, ADR-0076): pick a **Base branch**, name the new branch/worktree, then `git worktree add`; `ctrl+t` instead provisions a managed worktree ahead of any set (ADR-0152). The Fold action lands a row's branch on trunk via `pop worktree fold` (ADR-0229, ADR-0251), offered on every row — a bound row answers with the **Bound-checkout fold confirmation** in the fold's own pane, not with a refusal in the picker. The **Work daemon**'s worktree parallelism remains the separate path where pop owns `git worktree add` for **managed** **Worktree set**s forked from the **Trunk worktree**. User-defined creation commands may still hand a new path back via **Switch**. Deleting a worktree queues a **Checkout removal** **Errand**, which also removes the checkout's **History** entry; its tmux session is left alone. A row whose checkout is the subject of an unfinished **Errand** carries the **Errand marker** and the picker holds a **Picker live rebuild** for as long as one is showing, so the delete the human just asked for is visible while it happens rather than only in its absence afterwards (ADR-0275).
  was: The fuzzy-search picker in `pop worktree dashboard` for choosing, creating, folding, or deleting git worktrees in the current repository. It lists every checkout git reports and marks each one managed-bound, managed-unbound, or ordinary. Interactive creation is in scope (`ctrl+a`, ADR-0076): pick a **Base branch**, name the new branch/worktree, then `git worktree add`; `ctrl+t` instead provisions a managed worktree ahead of any set (ADR-0152). The Fold action lands a row's branch on trunk via `pop worktree fold` (ADR-0229, ADR-0251), offered on every row — a bound row answers with the **Bound-checkout fold confirmation** in the fold's own pane, not with a refusal in the picker. The **Work daemon**'s worktree parallelism remains the separate path where pop owns `git worktree add` for **managed** **Worktree set**s forked from the **Trunk worktree**. User-defined creation commands may still hand a new path back via **Switch**. Deleting a worktree also removes its **History** entry; its tmux session is left alone.

+ Errand marker
  How an unfinished **Errand** shows on the surface that asked for it: in the
  **Worktree picker**'s marker column, the shared working spinner while the
  errand is queued or running, and a static glyph once it is an **Errand
  failure**. It outranks the **Half-removed checkout** mark on the same row,
  a removal in flight being the repair of that state. It carries no verb — the
  **Errand failure**'s retry and dismiss stay on the **Work dashboard** —
  because delete pressed again on a failed row already re-queues it.
  avoid: progress icon, spinner column, status badge
  under: Language

+ Picker live rebuild
  A picker re-reading its own items on a timer instead of building them once,
  held only while something on screen is still moving — today, an **Errand
  marker** — and dropped the moment nothing is. It does not extend to config: a
  picker still holds none, so the **Config dashboard host** rule that only the
  **Work dashboard** hot-reloads config is unaffected.
  avoid: picker refresh, polling picker, auto-reload
  under: Language

~ Errand
  A single act pop performs for the human in the background, running no coding
  agent — a **Checkout removal** is one. It records its subject and not a plan,
  so the **Pop daemon** works out what to do when it runs rather than when it
  was queued. It is invisible to the **Work seam** until it fails, but never
  invisible to the surface that asked for it: while unfinished it shows there as
  an **Errand marker**.
  was: A single act pop performs for the human in the background, running no coding agent — a **Checkout removal** is one. It records its subject and not a plan, so the **Pop daemon** works out what to do when it runs rather than when it was queued.
