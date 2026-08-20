---
fragment: 8124D25E
generation: 0028
branch: master
---

~ Fold
  The act of replaying a checkout's branch onto the **Trunk worktree**'s branch:
  a plain `git rebase` of a **Fold scratch branch** inside the folding checkout,
  then trunk advanced by fast-forward only. Trunk is never left mid-operation,
  never gains a merge commit, and is never unwound. Its subject is a **checkout**
  — `pop worktree fold [<name>]`, or the Fold action on an eligible row in the
  **Worktree picker** — and **Task-set fold** is the specialization that
  addresses one by set instead. Eligible rows are those with no live
  non-archived **Worktree binding**; a bound row answers through its set, so the
  picker is never a route around a set's status gate. Pop computes no
  mergeability verdict and keeps no backlog — the attempt itself is the answer,
  discovered in the foreground; a conflict opens the **Fold conflict prompt**.
  Refuses a detached checkout, the **Trunk worktree** itself, a dirty checkout or
  trunk, a live **Runtime execution lock** on either, a branch already contained
  in trunk, and an unclassifiable scratch branch. Fold never pushes and never
  fetches: landing trunk anywhere else, and refreshing it, are the human's own
  concern. On success the checkout is left clean at the landed tip on its own
  branch — fold deletes nothing (ADR-0229).
  avoid: integrate, merge, land, ship, reconcile
  was: The act of replaying a finished **Task set**'s branch onto the **Trunk worktree**'s branch and releasing its checkout — `pop tasks fold <set>`, or the Fold action on a foldable set in the **Work dashboard** and the **Assist session**. Trunk is never left mid-operation and never gains a merge commit: fold **rebases the set branch onto trunk** inside the set's own checkout (plain rebase — merge commits inside the set branch are flattened), then moves trunk by fast-forward only; if trunk moved in between it redoes the rebase once and then refuses. Foldable means a **managed** (provisioned) **Worktree binding** plus **DONE** or **Awaiting-approval** — the same condition named **Unfolded Task set**; folding an Awaiting-approval set *is* the sign-off, so after a successful rebase and fast-forward it completes every remaining open HITL task in the set, named up front in the confirmation. Pop computes no mergeability verdict in advance and keeps no backlog — the attempt itself is the answer, discovered in the foreground; a conflict opens the **Fold conflict prompt**. On success it releases the **Worktree binding**, then applies **Managed-worktree teardown reference count**; the set is not archived, which stays a separate confirmed act. Fold never pushes and never fetches: landing trunk anywhere else, and refreshing it, are the human's own concern.

+ Fold scratch branch
  The disposable ref a **Fold** rebases in place of the real branch:
  `pop/fold/<branch>`, with `/` flattened to `-`. It exists because a rebase
  rewrites whatever is checked out, so leaving the human's branch intact means
  rewriting a second ref that points at the same commits. Created at the
  branch's pre-fold tip, checked out in the folding worktree, rebased onto
  trunk, and deleted once the fold completes. Its name is **deterministic**, not
  unique — a re-run must compute the same ref, because that is what makes fold
  idempotent. Finding one at preflight is normal and means one of three things,
  read from git alone: a rebase in progress is **parked**, no rebase plus
  reachability from the branch is **residue** from a fold that died after
  landing, and anything else is ambiguous and refused by name.
  avoid: temp branch, backup branch, fold branch, staging ref
  under: Fold

+ Fold boundary
  The single irreversible step in a **Fold** — the fast-forward that advances
  trunk — and the reason fold has exactly two recovery regimes rather than a
  ladder of failure states. Before it, neither the real branch nor trunk has
  moved, so every exit is a total rollback: abort, restore the checkout, delete
  the **Fold scratch branch**, with nothing to restore because nothing was
  rewritten. After it the work is landed and the remainder is local ref
  bookkeeping, so a failure earns a bounded retry and a precise report, and a
  re-run **converges** — the replayed commits drop as already-upstream, the
  fast-forward becomes a no-op, and the branch moves where it should have gone.
  Trunk is never unwound past it.
  avoid: point of no return, commit point, fold checkpoint
  under: Fold

+ Task-set fold
  The specialization of **Fold** that addresses a checkout by **Task set** —
  `pop tasks fold <set>`, or the Fold action in the **Work dashboard** and the
  **Assist session**. It resolves the set's **Worktree binding** to a checkout,
  adds the promises a *set* owes, and delegates the git work unchanged: the set
  must be **DONE** or **Awaiting-approval** (folding an Awaiting-approval set
  *is* the sign-off, completing its open HITL tasks after the fast-forward,
  named up front in the confirmation), and on success it releases the binding
  and applies **Managed-worktree teardown reference count**. The set is not
  archived, which stays a separate confirmed act. Managed-ness governs teardown,
  never eligibility: the verb folds an adopted binding too.
  avoid: set fold, tasks fold, fold a set
  under: Fold

~ Fold conflict prompt
  The attended, TTY-only choice pop presents when a **Fold** rebase stops on a
  conflict in the folding checkout: agent assistance (default, Enter),
  **resume** (continue the in-flight rebase), **retry** (abort it and restart
  fold from preflight), **abandon** (abort, restore the checkout, delete the
  **Fold scratch branch** — exactly the pre-fold state), and **exit**, which
  parks the rebase for a later fold to resume. Abandon and exit are deliberately
  distinct: walking away and stopping for now are different intentions. On a
  **Task-set fold** it also offers to verify the set and carries the
  **Verified-at SHA** badge; with no set in play both are suppressed. It
  re-appears after every unsuccessful resolution rather than refusing once.
  Unreachable without a TTY — an unattended resolver moving trunk is exactly
  what fold refuses to be.
  avoid: conflict menu, merge prompt, resolver
  was: The attended, TTY-only choice pop presents when a **Fold** rebase stops on a conflict in the set's checkout: agent assistance (default, Enter), **resume** (continue the in-flight rebase), **retry** (abort it and restart fold from preflight), verify the set, or exit. It re-appears after every unsuccessful resolution rather than refusing once, and carries the set's **Verified-at SHA** badge so the human can see whether the work is still cleared. Unreachable without a TTY — an unattended resolver moving trunk is exactly what fold refuses to be.

+ `pop worktree fold`
  The checkout-addressed **Fold** verb, naming a worktree by picker name or
  defaulting to the current checkout. Sibling of `pop tasks fold`, and the one
  owner of the **Fold conflict prompt** for a set-less fold. The **Worktree
  picker**'s Fold action delegates to it by spawning it into a tagged tmux pane
  rather than running it inline: the picker's stdout is the selected path, so an
  interactive verb cannot share it. Outside tmux the picker action refuses and
  points here.
  under: Fold

~ Worktree picker
  The fuzzy-search picker in `pop worktree dashboard` for choosing, creating,
  folding, or deleting git worktrees in the current repository. It lists every
  checkout git reports and marks each one managed-bound, managed-unbound, or
  ordinary. Interactive creation is in scope (`ctrl+a`, ADR-0076): pick a **Base
  branch**, name the new branch/worktree, then `git worktree add`; `ctrl+t`
  instead provisions a managed worktree ahead of any set (ADR-0152). The Fold
  action lands a row's branch on trunk via `pop worktree fold` (ADR-0229),
  offered on rows with no live **Worktree binding** and refusing by name on the
  rest. The **Work daemon**'s worktree parallelism remains the separate path
  where pop owns `git worktree add` for **managed** **Worktree set**s forked
  from the **Trunk worktree**. User-defined creation commands may still hand a
  new path back via **Switch**. Deleting a worktree also removes its **History**
  entry; its tmux session is left alone.
  avoid: Repo picker
  was: The fuzzy-search picker in `pop worktree` for choosing, creating, or deleting git worktrees in the current repository. Interactive creation is in scope (`ctrl+a`, ADR-0076): pick a **Base branch**, name the new branch/worktree, then `git worktree add`. The **Work daemon**'s worktree parallelism remains the separate path where pop owns `git worktree add` for **managed** **Worktree set**s forked from the **Trunk worktree**. User-defined creation commands may still hand a new path back via **Switch**. Deleting a worktree also removes its **History** entry; its tmux session is left alone.
