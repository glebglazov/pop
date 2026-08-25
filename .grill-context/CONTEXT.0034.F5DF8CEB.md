---
fragment: F5DF8CEB
generation: 0034
branch: master
---

~ Fold
  The act of replaying a checkout's branch onto the **Trunk worktree**'s branch:
  a plain `git rebase` of a **Fold scratch branch** inside the folding checkout,
  then trunk advanced by fast-forward only. Trunk is never left mid-operation,
  never gains a merge commit, and is never unwound. Its subject is a **checkout**
  — `pop worktree fold [<name>]`, or the Fold action on any row in the
  **Worktree picker** — and **Task-set fold** is the specialization that
  addresses one by set instead. Every checkout is eligible: a live **Worktree
  binding** raises the **Bound-checkout fold confirmation** rather than a
  refusal, because a binding is pop's own bookkeeping and never a reason git
  cannot proceed. Pop computes no mergeability verdict and keeps no backlog —
  the attempt itself is the answer, discovered in the foreground; a conflict
  opens the **Fold conflict prompt**. What it *does* refuse is governed by
  **Unrequested-act refusal**: a detached checkout, the **Trunk worktree**
  itself, a dirty checkout or trunk, a live **Runtime execution lock** on
  either, a branch already contained in trunk, and an unclassifiable scratch
  branch. Fold never pushes and never fetches: landing trunk anywhere else, and
  refreshing it, are the human's own concern. On success the checkout is left
  clean at the landed tip on its own branch — fold deletes nothing (ADR-0229,
  ADR-0233).
  avoid: integrate, merge, land, ship, reconcile
  was: The act of replaying a checkout's branch onto the **Trunk worktree**'s branch: a plain `git rebase` of a **Fold scratch branch** inside the folding checkout, then trunk advanced by fast-forward only. Trunk is never left mid-operation, never gains a merge commit, and is never unwound. Its subject is a **checkout** — `pop worktree fold [<name>]`, or the Fold action on an eligible row in the **Worktree picker** — and **Task-set fold** is the specialization that addresses one by set instead. Eligible rows are those with no live non-archived **Worktree binding**; a bound row answers through its set, so the picker is never a route around a set's status gate. Pop computes no mergeability verdict and keeps no backlog — the attempt itself is the answer, discovered in the foreground; a conflict opens the **Fold conflict prompt**. Refuses a detached checkout, the **Trunk worktree** itself, a dirty checkout or trunk, a live **Runtime execution lock** on either, a branch already contained in trunk, and an unclassifiable scratch branch. Fold never pushes and never fetches: landing trunk anywhere else, and refreshing it, are the human's own concern. On success the checkout is left clean at the landed tip on its own branch — fold deletes nothing (ADR-0229).

+ Unrequested-act refusal
  The rule that decides whether **Fold** refuses or merely asks: it refuses only
  where it cannot proceed without performing an act nobody requested —
  inventing a branch for a detached HEAD, stashing a dirty checkout or trunk,
  waiting out or evicting a **Runtime execution lock** holder, guessing at an
  unclassifiable **Fold scratch branch** — or where there is no act to perform
  at all, as with the **Trunk worktree** itself or a branch already in trunk.
  Whatever fold can simply *do*, it does, after saying plainly what looks
  strange about it. A **Worktree binding** is the case that separates the two:
  nothing about it stops the rebase, so it earns a confirmation and never a
  refusal.
  avoid: magic operation, fold guard, eligibility check
  under: Fold

+ Bound-checkout fold confirmation
  What **Fold** asks before landing a checkout that live **Worktree binding**s
  still name. It lists every bound **Task set** with its status and states the
  consequence for each: a **DONE** or **Awaiting-approval** set is signed off
  and its binding released, while an unfinished set keeps its binding, because
  the set is still being worked on and the checkout is still where it lives.
  `--yes` answers it — that flag is the only way any non-interactive channel
  runs, so the fold prints the override it took rather than passing over it in
  silence. Releasing a binding here skips **Managed-worktree teardown reference
  count**: the checkout verb deletes nothing.
  avoid: bound worktree warning, fold override prompt
  under: Fold

~ `pop worktree fold`
  The checkout-addressed **Fold** verb, naming a worktree by picker name or
  defaulting to the current checkout. Sibling of `pop tasks fold`, and the one
  owner of the **Fold conflict prompt** for a set-less fold. It never refuses a
  checkout on account of a **Worktree binding**: it raises the **Bound-checkout
  fold confirmation** and then settles what a set can have settled — the
  **Awaiting-approval** sign-off and the binding release for a foldable set,
  nothing for an unfinished one. The **Worktree picker**'s Fold action delegates
  to it by spawning it into a tagged tmux pane rather than running it inline:
  the picker's stdout is the selected path, so an interactive verb cannot share
  it. Outside tmux the picker action refuses and points here.
  under: Fold
  was: The checkout-addressed **Fold** verb, naming a worktree by picker name or defaulting to the current checkout. Sibling of `pop tasks fold`, and the one owner of the **Fold conflict prompt** for a set-less fold. The **Worktree picker**'s Fold action delegates to it by spawning it into a tagged tmux pane rather than running it inline: the picker's stdout is the selected path, so an interactive verb cannot share it. Outside tmux the picker action refuses and points here.

~ Worktree picker
  The fuzzy-search picker in `pop worktree dashboard` for choosing, creating,
  folding, or deleting git worktrees in the current repository. It lists every
  checkout git reports and marks each one managed-bound, managed-unbound, or
  ordinary. Interactive creation is in scope (`ctrl+a`, ADR-0076): pick a **Base
  branch**, name the new branch/worktree, then `git worktree add`; `ctrl+t`
  instead provisions a managed worktree ahead of any set (ADR-0152). The Fold
  action lands a row's branch on trunk via `pop worktree fold` (ADR-0229,
  ADR-0233), offered on every row — a bound row answers with the
  **Bound-checkout fold confirmation** in the fold's own pane, not with a
  refusal in the picker. The **Work daemon**'s worktree parallelism remains the
  separate path where pop owns `git worktree add` for **managed** **Worktree
  set**s forked from the **Trunk worktree**. User-defined creation commands may
  still hand a new path back via **Switch**. Deleting a worktree also removes
  its **History** entry; its tmux session is left alone.
  avoid: Repo picker
  was: The fuzzy-search picker in `pop worktree dashboard` for choosing, creating, folding, or deleting git worktrees in the current repository. It lists every checkout git reports and marks each one managed-bound, managed-unbound, or ordinary. Interactive creation is in scope (`ctrl+a`, ADR-0076): pick a **Base branch**, name the new branch/worktree, then `git worktree add`; `ctrl+t` instead provisions a managed worktree ahead of any set (ADR-0152). The Fold action lands a row's branch on trunk via `pop worktree fold` (ADR-0229), offered on rows with no live **Worktree binding** and refusing by name on the rest. The **Work daemon**'s worktree parallelism remains the separate path where pop owns `git worktree add` for **managed** **Worktree set**s forked from the **Trunk worktree**. User-defined creation commands may still hand a new path back via **Switch**. Deleting a worktree also removes its **History** entry; its tmux session is left alone.
