# An implementation commit names its task in a trailer

Since [ADR-0207](0207-commit-conventions-resolve-at-plan-time-into-planned-subjects.md) an **Implementation commit** carries a **Planned commit subject** written in the repository's own grammar, so nothing in the commit says which task made it. The task→commit edge lives only in the manifest (`Task.Commit.SHA`), which is machine-local, keyed on a SHA, and destroyed by the rebase that `Fold` performs as a routine step of pop's own pipeline. Once a set folds to trunk, a commit is anonymous: the branch is deleted, the worktree is gone, and the only surviving link is a SHA in a store on one machine.

The decision: **every Implementation commit ends with a `Pop-Task: <task-set-id>/<task-id>` trailer**, written algorithmically by the executor as its own paragraph after the agent summary. The value is the canonical **Task identifier** pair — the set's full dated directory name, not the timestamp-stripped slug pop uses for display subjects — so it reads the same as a **Task target reference** and needs no reverse mapping. The trailer is the commit→task edge history keeps for itself, and it is what the human uses to land a review fix in the commit that earned it: `git log --grep` for the ref, `git commit --fixup=<sha>`, autosquash.

The trailer is **identity, not style**, so it sits outside the **Convention stack** entirely and is unconditional — no configuration key suppresses it. A **Commit convention** governs the grammar an author writes in; a repository must not be able to blind pop to its own history through prose, and a key here would silently degrade the range recovery below months before anyone connected the two.

## Scope: task commits only

Only commits pop makes *for a task* carry it. Deliberately excluded:

- **The dirty-runtime checkpoint commit.** It knows its set and task, but it captures the human's pre-existing changes, not the task's work, and marking it would return two commits for one ref.
- **Skill commits** — `/to-tasks` publishing a set, a `grill-with-docs` close commit, anything an agent commits by following skill prose. Pop can instruct but not enforce there, and nothing would read the mark.
- **Human commits** made by hand in a pop worktree. No git hook: a hook cannot tell the two apart, and marking a human's commit as pop's is worse than leaving pop's own skill commits unmarked.

Remediation-task commits are task commits and carry it like any other.

## The commits recipe is unchanged

The obvious second use — teaching `conventions/recipes/commits.md` to discard `Pop-Task` commits before sampling the log — is **rejected**. A pop commit written under a resolved convention is a faithful sample *of that convention*, not a foreign accent; discarding it teaches the recipe nothing, and once pop writes most of a repository's history it starves the sample entirely. What must stay discarded is pop's **default** format (`tasks(<slug>): <id>`), which is pop's accent and nobody's convention, and the recipe already names exactly that. A convention that is wrong is fixed in the layer that holds it — pop memory, the repository document, or the **Convention overlay** — never by filtering the log.

## Consequences

- **The Verifier's layer-two range recovery switches to the trailer.** `resolveVerifyRange` (`tasks/verify.go`) keeps layer one unchanged — recorded **Set base commit** plus every recorded task SHA reachable ⇒ `base..HEAD`. Layer two stops grepping recorded **Planned commit subject**s (ADR-0207's consequence) and greps `Pop-Task: <set-id>/` instead: one pattern for the whole set rather than one per task, matching only text pop wrote. `earliestRecordedSubjectCommit` is deleted rather than kept as a third layer — a revert, a fixup, or a merge commit *quoting* a planned subject matches it, and being older it anchors the range too early, silently widening the changeset instead of failing loudly. Sets drained before the trailer existed therefore lose rebase recovery and park NEEDS-HUMAN, which is a loud failure on a small and shrinking population. The pre-ADR-0207 `legacyPrefixWorkDiff` path is untouched.
- **The range stays a contiguous range, not a set of `Pop-Task` commits.** Enumerating the set's own commits is now possible and is *not* adopted: a fix the human commits by hand mid-drain would drop out of verification, which is worse than a docs commit drifting in. Anything committed in the same checkout between the first task commit and HEAD remains in the Verifier's context, deliberately.
- **A task may match more than one commit.** A re-run of an already-committed task commits again and overwrites the manifest record but not history. The manifest stays the authority on "one task, one commit"; readers of the trailer take the **newest reachable** match, matching the manifest's own "the latest commit is the reachable one".
- **Pop ships no verb that consumes the trailer for fixups.** The Verifier is its only reader. A lookup command or `pop tasks fixup` is deferred until the by-hand flow has shaped what it should do.
- The checkpoint commit still has no body at all; giving it one is not required by anything here.
