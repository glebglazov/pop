---
status: accepted
amends: [ADR-0148, ADR-0156]
---

# Fold is an act on a checkout, performed on a scratch branch

> **Relates:** amends [ADR-0148](0148-fold-lands-a-finished-set-and-releases-its-checkout.md) and [ADR-0156](0156-fold-rebases-onto-trunk-and-folds-awaiting-approval-through-a-looping-conflict-prompt.md). Their shape survives whole — one explicit foreground verb, no background machinery, no mergeability verdict, trunk advanced by fast-forward only, conflicts confined to the folding checkout, never pushes, never fetches. What changes is the **subject** (a checkout, not a Task set), the **ref the rebase rewrites** (a scratch branch, not the real one), and the **recovery model** that falls out. Extends [ADR-0152](0152-managed-worktrees-can-be-created-ahead-of-a-task-set.md): the picker could already mint a managed worktree and had no way to land one. Leans on [ADR-0076](0076-worktree-picker-owns-interactive-creation.md) for the picker owning worktree lifecycle verbs.

ADR-0148 gave the Done-set clean-up reminder a verb. But it addressed that verb by **Task set**, and `preflightFold` still resolves everything from a set id, so a checkout with no set cannot name itself to fold at all — the human reconciles by hand, which is the same hole 0148 set out to close, one level down. Meanwhile the eligibility story drifted: the verb never reads the `Provisioned` bit (0148 says so outright), but every offering surface gates on `Unfolded`, which requires it, so an adopted-binding fold is reachable only by typing the CLI.

Decision, in three parts.

**Fold's subject is a checkout.** Folding is: rebase this checkout's branch onto trunk, then advance trunk by fast-forward. `pop tasks fold <set>` becomes a **specialization** — it resolves a set to a checkout, adds the gates a *set* owes (`FoldEligibleStatus`, the Awaiting-approval sign-off, binding release, reference-counted teardown), and delegates the git work. In code that is `FoldCheckout(path)` inside `tasks/binding`, wrapped by the existing `Fold(setID)`; the package already owns every path-level helper the primitive needs, so nothing moves. The new surfaces are `pop worktree fold [<name>]` and a key in `pop worktree dashboard`, offered on rows with **no live non-archived binding** — an ordinary human worktree or an unbound managed one. A row that *is* bound keeps answering through its set, so the picker never becomes a route around a set's status gate. The key stays live on ineligible rows: fold's refusals are already specific ("this is the Trunk worktree; nothing to fold"), and a silently dead key teaches nothing.

**The rebase rewrites a scratch branch, never the real one.** A rebase is a rewrite of whatever is checked out, so leaving branch `B` intact means pointing a second ref at the same commits and rewriting that instead. Fold creates `pop/fold/<B>` — the **Fold scratch branch**, named by flattening `/` to `-`, deterministically, because "just run it again" depends on a re-run computing the same ref. The flow:

1. record `S`, the tip of `B`
2. `git branch <scratch> S`
3. `git checkout <scratch>` in the folding worktree
4. `git rebase <trunkBranch>` — only the scratch branch is rewritten; a conflict opens the Fold conflict prompt
5. re-read trunk HEAD; if it moved, reset the scratch branch to `S` and redo once, then refuse
6. re-check trunk is clean and unclaimed, then `git merge --ff-only <scratch>` in the Trunk worktree
7. `git branch -f B <scratch>` — `B` moves for the first time
8. `git checkout B`
9. `git branch -d <scratch>`

Step 7 is possible only because the worktree is sitting on the scratch branch: git refuses to force a checked-out branch. Preflight additionally refuses a `B` already contained in trunk, which would otherwise fold to a silent no-op, and re-checks trunk immediately before step 6 because conflict resolution can run for an hour after the first check.

**Recovery has exactly two regimes, and the boundary is step 6.** Through step 5 neither `B` nor trunk has moved, so every exit is a **total rollback**: abort the rebase, check `B` back out, delete the scratch branch. There is nothing to restore because nothing was ever rewritten — which is why "abandon" joins the Fold conflict prompt as an exit distinct from "exit", the latter still parking the rebase for a later attempt to resume. Step 6 lands the work and is the one irreversible act; after it, steps 7–9 are local ref updates, so a failure there gets a bounded inline retry and then a precise report. **Trunk is never unwound** — undoing a landed fold means rewriting the one ref everything else forks from, which is the failure mode this whole line of ADRs exists to prevent. A re-run instead **converges**: the scratch branch is recreated at `S`, rebased onto a trunk that already contains that work, every commit drops as already-upstream, the fast-forward is a no-op, and `B` force-moves where it should have gone.

Recovery state is **read from git, never journalled**. The scratch branch's existence, a rebase-in-progress directory, its reachability from `B`, and trunk's containment of it fully determine where a fold stopped, so a killed process is indistinguishable from any other interruption. Finding a scratch branch at preflight is therefore normal and means one of three things: a rebase in progress is **parked** (enter the conflict prompt); no rebase and reachable from `B` is **residue** from a fold that died after step 6 (delete it and proceed); anything else is **ambiguous** and refused by name rather than guessed at.

The Task-set fold inherits all of this. Two mechanisms would mean two recovery stories, and the set path gains the same protection for free — today a failed fast-forward there leaves the set branch already rewritten with no clean way back.

## Considered Options

- **Keep fold set-addressed and require the human to bind a set first.** Rejected: it makes a bookkeeping record the price of a git operation, for a checkout that may never want a set.
- **A separate `Worktree fold` concept beside the Task-set one.** Rejected: the mechanism, the trunk law, the conflict prompt and every refusal are one body of code; two nouns would drift apart. Worktree-primary with a named specialization says the same thing without the duplication.
- **Back up the pre-fold tip to `refs/pop/fold-backup/<branch>` and keep rebasing in place.** Rejected in favour of the scratch branch: a backup makes failure *recoverable*, whereas rewriting a disposable ref makes it *not have happened*. Recovery that needs no restore step cannot half-succeed.
- **Rebase the scratch branch in a separate throwaway worktree**, so the folding checkout is never touched. Rejected despite better isolation: ADR-0156 resolves conflicts in the checkout deliberately, and a fresh directory has none of the build state an agent needs to test its resolution.
- **Journal the fold's progress to pop's data dir.** Rejected — a second source of truth about git that can go stale, the same objection ADR-0070 raised against a precomputed mergeability verdict.
- **Unwind trunk when a post-landing step fails.** Rejected on principle; see above.
- **Gate the picker key per row kind.** Rejected: it would be the first picker action whose availability depends on the row, and the refusals are better teachers than an absent key.
- **Offer to delete or recycle the checkout on success.** Rejected: after a fold the branch and trunk are the same commit, so the checkout is already clean at the landed tip and the only residue is a branch name. Fold stays non-destructive by default; the picker's existing delete is right there.

## Consequences

- `CONTEXT.md`'s **Fold** entry is rewritten checkout-first, and its "foldable means a managed (provisioned) binding" line goes: that was **Unfolded**'s read-surface predicate borrowed by mistake. The verb takes any checkout; managed-ness governs *teardown*, not eligibility.
- ADR-0156's consequence that the set branch is rewritten and its pre-fold SHAs orphaned is **retired**, not narrowed — the real branch is no longer rewritten while anything can still fail.
- The Fold conflict prompt gains **abandon** and loses **verify** (and its Verified-at badge) when there is no set — an absent set is a field on the context struct, not a second prompt.
- Fold becomes reachable on a checkout pop did not create, which is the first surface to do so. Teardown is unaffected: pop still deletes only what it demonstrably created.
- A picker-initiated fold spawns `pop worktree fold` into a tagged tmux pane, as the Work dashboard already spawns `pop tasks fold` under `@pop_fold`. It cannot run inline: the picker's stdout *is* the selected path, and the `cd "$(pop worktree dashboard)"` contract makes that structural rather than a matter of discipline. Outside tmux the key refuses and points at the headless verb.
- One irreversible boundary means one place to reason about: everything before step 6 is undoable, everything after it is a converging re-run.
