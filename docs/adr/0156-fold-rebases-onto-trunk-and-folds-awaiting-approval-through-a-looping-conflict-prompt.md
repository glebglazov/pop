---
status: accepted
amends: [ADR-0148]
---

# Fold rebases onto trunk, folds an Awaiting-approval set, and loops on conflict

> **Relates:** amends [ADR-0148](0148-fold-lands-a-finished-set-and-releases-its-checkout.md) — its shape (one explicit foreground verb, no background machinery, trunk advanced by fast-forward only, conflicts confined to the disposable checkout, never pushes) is unchanged. What changes is the git mechanism, the eligible statuses, and the conflict exit. Leans on [ADR-0096](0096-pass-verdict-immunizes-terminal-status-against-sha-drift.md) for why rewriting the set branch cannot un-verify a set, and on [ADR-0020](0020-override-verbs-accept-whole-set-via-multi-task-selection.md) for the non-interactive override rule.

ADR-0148 merged *trunk into the set branch* so that a conflict would never leave trunk mid-merge. That guarantee was right, but the mechanism has three costs the operator hit in real use: every fold where trunk had moved left a `pop fold: bring trunk (master) into set branch` merge commit in shared history (two are already on `origin/master`, and they stay — this change is forward-only); a set awaiting human sign-off could not be folded at all, even though the human's sign-off *is* the act of landing it; and abandoning a conflict left the merge in progress, so the next `pop tasks fold` died at preflight on `set worktree is dirty` and never re-offered assistance.

Decision, in three parts.

**Rebase, not merge.** Fold runs a plain `git rebase <trunkBranch>` in the set's own checkout, then advances trunk with `--ff-only`. Conflicts still land only in the disposable worktree and trunk still never sits mid-operation, so 0148's safety property survives intact — but trunk's history stays linear and gains no pop-authored merge commits. Plain rebase, not `--rebase-merges`: a task-set branch is a linear run of task commits, and a merge commit inside one is noise worth flattening. The rebase may therefore stop once per replayed commit rather than once per fold. The rebase attempt is still the check — pop computes no mergeability verdict in advance. Nothing is attributed to the resolving agent in the commits; an agent-untangled rebase looks like any other.

**DONE and AWAITING-APPROVAL are both foldable.** An Awaiting-approval set has no open AFK work — only human sign-off — so folding it is that sign-off. Fold names the set's remaining open HITL tasks in its confirmation, and on a successful rebase *and* fast-forward completes all of them, then releases the binding as usual. Ordering matters: a failed rebase leaves the HITL tasks untouched. Every other status stays refused (NEEDS-VERIFY, VERIFY-FAILED, BLOCKED, READY, FAILED). Non-interactively, completing a human sign-off requires `--yes`; a bare non-TTY fold of an Awaiting-approval set refuses, per 0020's "nothing mass-mutates unwatched".

**A conflict loops instead of refusing once.** On any conflict stop, and again after every unsuccessful resolution, fold presents: agent assistance (default, Enter), **resume** (`git rebase --continue`, no preflight — preflight would refuse on the very dirtiness the conflict created), **retry** (`git rebase --abort`, then the whole fold again from preflight, for when the human has refreshed trunk by hand), **verify the set**, and exit. The prompt shows the set's **Verified-at SHA** badge so the human can see whether the work is still cleared. Verify is also offered once on the success path, before trunk fast-forwards — the last moment it is cheap, since after the fast-forward trunk already has the work. It is an offer, not a gate: a PASS is cached per `(repo, set)` episode and a rebase changes no done-AFK composition, so ADR-0096 and ADR-0109 already say a resolution cannot silently revoke verification. Preflight additionally detects a rebase left in progress and routes straight back into this prompt instead of the dirty refusal. Assistance stays TTY-only and unreachable from the Queue or a daemon.

## Considered Options

- **Keep the merge and add agent attribution to its message.** Rejected: it makes the commits self-documenting but does not stop them existing, and the objection is to the commits themselves.
- **`--rebase-merges`.** Rejected: preserves a shape that task-set branches should not have.
- **Hard-gate a conflicted fold on re-verification** (resolution invalidates PASS → set drops to NEEDS-VERIFY → fold refuses until verified). Rejected as safest-but-most-annoying, and it contradicts 0096's episode-keyed immunity; the explicit verify option covers the same worry on demand.
- **Fetch trunk inside `retry`.** Rejected: fold stays offline. The human refreshes trunk in the trunk worktree and hits retry.
- **Let fold leave an Awaiting-approval set's HITL tasks open.** Rejected: the fold would succeed and the set would still not be DONE, which is the same dangling reminder 0148 set out to close.

## Consequences

- The set branch is rewritten, so its pre-fold SHAs are orphaned. Harmless: the branch is disposable, never pushed, and verdicts key on `(repo, set)` episodes rather than SHAs.
- "Merge in progress" detection throughout fold becomes "rebase in progress"; the conflict-assist prompt's boundary (resolve in the set checkout only, never touch trunk, never push) is unchanged.
- A single fold can now run the assist loop several times for one conflict-heavy branch.
