---
fragment: D82C3A41
generation: 0061
branch: master
---

+ Fold landing gate
  The attended choice a **Fold** presents once its rebase has completed and before
  the fast-forward — the one moment where the folded result exists and trunk has
  not moved. It offers **land now** (the Enter default), **verify set**, **abandon**
  and **exit**, and prints the three tips the decision turns on: the trunk branch,
  the set branch at its pre-fold tip, and the **Fold scratch branch** at the rebased
  tip. Sibling of the **Fold conflict prompt**, not a rename of it: that prompt
  speaks for a rebase that stopped, this one for a rebase that succeeded. Reached
  twice — after a conflict was resolved, and on re-entry into a fold left in the
  **rebased** scratch state — so an interrupted fold lands back on the gate it was
  interrupted at rather than on a refusal. It never appears on a clean fold: a
  rebase that met no conflict has nothing to say, and stopping every fold would make
  the verb attended.
  avoid: verify prompt, pre-landing gate, fold menu
  under: Language

~ Fold conflict prompt
  The attended, TTY-only choice pop presents when a **Fold** rebase stops on a
  conflict in the folding checkout: agent assistance (default, Enter), **resume**
  (continue the in-flight rebase), **retry** (abort it and restart fold from
  preflight), **abandon** (abort, restore the checkout, delete the **Fold scratch
  branch** — exactly the pre-fold state), and **exit**, which parks the rebase for a
  later fold to resume. Abandon and exit are deliberately distinct: walking away and
  stopping for now are different intentions. On a **Task-set fold** it also offers to
  verify the set and carries the **Verified-at SHA** badge; with no set in play both
  are suppressed. It re-appears after every unsuccessful resolution rather than
  refusing once. A resolution that *completes* the rebase hands over to the **Fold
  landing gate** instead of asking its own question, so the prompt's whole subject
  stays the conflict. Unreachable without a TTY — an unattended resolver moving trunk
  is exactly what fold refuses to be.
  was: The attended, TTY-only choice pop presents when a Fold rebase stops on a
  conflict in the folding checkout: agent assistance (default, Enter), resume,
  retry, abandon and exit. On a Task-set fold it also offers to verify the set and
  carries the Verified-at SHA badge. Its post-resolution beat was a bare
  `Verify set? [y/N]`, whose two answers both continued the fold.
  avoid: conflict menu, merge prompt, resolver

~ Fold scratch branch
  The disposable ref a **Fold** rebases in place of the real branch: `pop/fold/<branch>`,
  with `/` flattened to `-`. It exists because a rebase rewrites whatever is checked
  out, so leaving the human's branch intact means rewriting a second ref that points
  at the same commits. Created at the branch's pre-fold tip, checked out in the
  folding worktree, rebased onto trunk, and deleted once the fold completes. Its name
  is **deterministic**, not unique — a re-run must compute the same ref, because that
  is what makes fold idempotent. Finding one at preflight is normal and means one of
  four things, read from git alone: a rebase in progress is **parked**, no rebase plus
  reachability from the branch or from trunk is **residue** from a fold that died
  after landing, a ref trunk is an ancestor of but the branch does not reach is
  **rebased** — the fold got past its rebase and stopped before the **Fold boundary**,
  so it resumes at the **Fold landing gate** — and anything else is **ambiguous**.
  was: … Finding one at preflight is normal and means one of three things, read from
  git alone: a rebase in progress is parked, no rebase plus reachability from the
  branch is residue from a fold that died after landing, and anything else is
  ambiguous and refused by name.
  avoid: temp branch, backup branch, fold branch, staging ref

~ Unrequested-act refusal
  The rule that decides whether **Fold** refuses or merely asks: it refuses only
  where it cannot proceed without performing an act nobody requested — inventing a
  branch for a detached HEAD, stashing a dirty checkout or trunk, waiting out or
  evicting a **Runtime execution lock** holder — or where there is no act to perform
  at all, as with the **Trunk worktree** itself or a branch already in trunk.
  Whatever fold can simply *do*, it does, after saying plainly what looks strange
  about it. A **Worktree binding** is the case that separates the two: nothing about
  it stops the rebase, so it earns a confirmation and never a refusal. An
  **ambiguous** **Fold scratch branch** is the case that shows the rule is about
  *pop* guessing, not about the human being stuck: pop still names the ref and
  chooses nothing, but it prints the three tips and offers the acts it could take,
  defaulting to none of them.
  was: The rule that decides whether Fold refuses or merely asks … or guessing at an
  unclassifiable Fold scratch branch … Whatever fold can simply do, it does. A
  Worktree binding is the case that separates the two.
  avoid: magic operation, fold guard, eligibility check
