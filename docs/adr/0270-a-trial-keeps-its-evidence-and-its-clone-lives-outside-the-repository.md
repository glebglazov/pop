---
status: accepted
relates: "amends decision 7 of [ADR-0268](0268-the-eval-harness-compares-a-bare-agent-with-pop-on-spend-and-graded-quality.md), which put the per-Trial clones beside the results in `eval/`; every other decision in ADR-0268 stands"
---

# A Trial keeps its evidence and its clone lives outside the repository

## Context

The first Eval Matrix — two Arms, one Case — produced two Invalid Trials and no
graded figures, for about fourteen dollars.

The Pop-arm Trial died on its first implementation commit with *Author identity
unknown*. Decision 3 of ADR-0268 sets `XDG_DATA_HOME` and `XDG_CONFIG_HOME` to
directories of the harness's own so a Trial reproduces from a clean state. On a
machine whose git identity lives at `~/.config/git/config` and nowhere else —
no `~/.gitconfig` — that isolation also takes git's identity away. The
condition is deterministic: it recurs on every run, on every such machine.

The Bare-arm Trial's clone was deleted underneath the running agent, sixteen
minutes and $5.39 in, by something outside the harness — `eval/work/` is
gitignored, so a `git clean -xfd` in the checkout reaches it while `-fd` does
not. The harness recorded an Invalid Trial with an empty patch. The agent's
transcript survived only because Claude Code happens to keep its own copy: the
Arm's **Captured run** was the one Captured run the harness did not lift out of
the work directory, while the Grader's already went to the Trial's result
directory. The spend figures in a Trial record are derived from that Captured
run, so the harness was recording a number whose evidence it then deleted.

Neither failure was visible while it happened. The Trial ran before the
harness's progress lines existed, and the phase, waiting and saved-path lines
that landed afterwards close that gap; nothing further is needed here.

## Decision

**1. Every clone the harness makes carries a fixed identity.** `cloneAtCommit`
writes `pop-eval <eval@pop.invalid>` into the clone right after checkout, so
Arm clones and grading clones alike can commit under any environment. Pointing
`GIT_CONFIG_GLOBAL` at the human's real configuration was rejected: this
machine's git config sets `commit.gpgsign` with an ssh signing key, so
inheriting it would drag commit signing into every Trial and make a Trial's
success depend on a key being present. The isolation of decision 3 stays; only
the identity is restored, and it is the harness's own rather than a human's.

**2. The Eval work root moves out of the repository**, to `$XDG_STATE_HOME`
else `~/.local/state/pop/eval/work`. Nothing a `git clean -xfd` in the pop
checkout can reach. State rather than pop's usual `XDG_DATA_HOME` because the
Pop arm already points `XDG_DATA_HOME` at a per-Trial directory *inside* the
work root, and the two would nest confusingly; state rather than
`XDG_CACHE_HOME` because a cache cleaner wiping a live Trial is the failure
being fixed. **The results root stays in the repository and stays committed**,
as decision 6 of ADR-0268 has it — the records are the product, and they are
what a later worktree forked from HEAD must be able to see.

**3. A Trial's evidence is lifted into its result directory, and the clone is
then deleted.** The Arm's Captured run is written to `capture/` as soon as the
Arm returns and before the patch step, mirroring the `grading/` the Grader's
Captured run already goes to; a Pop arm's **Verify reports** and **Refine
reports** are copied to `reports/`. The Trial's directory under the work root
is then removed — at the end of the Trial, not after grading, since grading
restores the tree from `diff.patch` in a clone of its own and never reads the
Trial's. `--keep-work` opts one run out and `go run ./eval clean` sweeps the
root by hand. Without this the thirty-Trial Matrix holds roughly five gigabytes
of clones; with it, one clone at a time.

**4. A paid-for run whose tree vanished is a Lost Trial**, the fourth **Trial
outcome**. `invalid` could not carry it: an Invalid Trial is a crash or a quota
pause, worth retrying and worth nothing, while a Lost Trial bought something the
eval must still charge itself for. It is excluded from quality figures, counted
in spend, and given a column of its own in the Rollup.

**5. Nothing is spent before the environment is checked.** One preflight per
Matrix, beside the existing Acceptance-list approval gate and before the first
clone: each selected Arm's agent binary resolves, the `pop` binary resolves for
Pop arms, and both roots are writable. It aborts the whole Matrix. A per-Trial
preflight was rejected — an environment fault is not a Trial fault and must not
consume that Trial's one retry.

**6. A saved Trial can be re-run on request.** `runMatrix` re-runs a saved Trial
only while `Outcome == invalid && Attempts < 2`, which is the right guard
*within* a Matrix but leaves a twice-failed Trial permanently skipped. `--redo`
re-runs a selected Trial whatever its recorded attempts. The alternative was
teaching the human to delete directories inside `eval/results` by hand, which
is the shape of the accident this ADR is about.

## Consequences

- A Trial's result directory grows a `capture/` and, for a Pop arm, a
  `reports/` — both committed. Measured against this machine's store, every
  Captured run of a 20.7M-token Task set totals 788K, so thirty Trials add tens
  of megabytes to a repository whose `.git` is already 117M.
- The two Trials from the first Matrix are deleted rather than salvaged. The
  Bare arm has no tree and made no commits; the Pop arm's patch holds one of two
  tasks with no Verify and no gate run. Both remain in git history.
- The eval's outputs are now split across two roots — records in the
  repository, scratch in local state. The `--work` and `--results` flags already
  made both movable; only their defaults diverge.
- A Case's Trial no longer reproduces a human's git configuration, so a
  convention that depends on signed commits cannot be measured by this harness
  without a further decision.
