---
status: accepted
supersedes: [ADR-0070]
---

# Fold lands a finished set's branch and releases its checkout

> **Relates:** supersedes [ADR-0070](0070-worktree-set-integration-is-removed-merging-is-the-humans-own-concern.md), carrying forward its retirement of the Integration target (below). Amends [ADR-0071](0071-managed-worktree-teardown-happens-only-at-archive-unbind-is-forget-only.md) (teardown gains triggers but keeps its confirm gate) and extends [ADR-0116](0116-managed-worktree-teardown-is-reference-counted.md)'s reference count to them.

ADR-0070 deleted worktree-set integration wholesale — the `Integrate` verb, the Integration backlog, Mergeability (a `git merge-tree` dry run), `auto_merge_clean`, and the dashboard `I` key — because a half-automated, consent-gated merge path was a second source of truth for "what's mergeable" that the operator did not trust. That judgment stands. But 0070 left a hole it names itself: the dashboard still shows a Done set holding a managed binding "as a clean-up reminder", and no verb was ever put behind that reminder. The worktree accumulates, the binding persists, and the human reconciles by hand or not at all.

Decision: **one explicit verb, `pop tasks fold <set>`**, with no background machinery of any kind. Pop computes no mergeability verdict, keeps no backlog, and adds no status suffix — the merge attempt *is* the check, run in the foreground where the human is standing. Fold merges **trunk into the set's branch, inside the set's own checkout**, then advances trunk by **fast-forward only**; trunk is never left mid-merge, and the conflict — with everything it may drag in — stays in the disposable worktree. If trunk moves in between, fold redoes the merge once and then refuses. On success it releases the Worktree binding and applies the reference-counted teardown. It does not push, and it does not archive the set.

A conflict opens **attended** agent assistance, resolving in the set's checkout only. It requires a TTY and is unreachable from the Queue or any daemon: an unattended resolver moving trunk is precisely the failure mode 0070 was right to delete.

Teardown consequently gains triggers. ADR-0071's single-trigger rule (Archive, and only Archive) is retired: Archive, a rebind that moves a set off a checkout, and Fold can each drop the last reference and reach teardown. **Its always-confirms rule is not retired** — every one of them asks first, and `--yes` remains the "I accept the consequences" channel.

**Carried forward from ADR-0070** (unchanged, restated here because that ADR is superseded): the Integration target abstraction stays retired in both roles — there is no merge-target abstraction beyond the trunk, and no fallback checkout for an unbound Queue drain. `pop integrate <agent>` is an unrelated monitor-setup feature and keeps its name; fold deliberately avoids the word.

## Considered Options

- **Full revival of integration** — backlog, mergeability verdicts, dashboard bucket. Rejected; this is the machinery 0070 objected to, and none of the objection has expired.
- **Precompute mergeability, then offer to merge.** Rejected — it recreates the second source of truth. A verdict computed before the attempt can be stale or wrong; the attempt cannot.
- **Merge the branch into trunk directly.** Rejected — a conflict then leaves the trunk, the one checkout every other set forks from and drains on, in a conflicted mid-merge state.
- **Leave merging outside pop and only add binding release.** Rejected — it clears pop's bookkeeping while leaving the human the actual work, so the reminder still has no completion.

## Consequences

- Landing trunk anywhere else remains the human's own concern; fold is local and never pushes.
- A set bound to the trunk itself has nothing to fold and is told so plainly.
- Fold requires the set to be DONE, which under enabled verification means a PASS in the current episode — a NEEDS-VERIFY set must be verified or accepted first.
- Fold works for adopted bindings too, but teardown does not: pop still deletes only what it demonstrably created.
