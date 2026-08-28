---
status: accepted
---

# A repository pass lifts the work that lives where the pane stands, and the lift stops being called a pin

> **Relates:** amends [ADR-0201](0201-a-pane-is-attributed-to-work-kind-side-and-seeds-the-dashboard-cursor.md)
> and [ADR-0209](0209-an-attributed-pane-pins-its-rows-to-the-top-and-says-nothing-else.md).
> Their text is left intact — an ADR is a dated record, not a live description — so both
> still say "pin" throughout. The glossary carries the rename and the `_Avoid_` entry that
> stops a reader importing the old word.

**Pane work attribution** gains a third and weakest pass, keyed on the repository rather
than on a checkout, and alone among the passes it merges across Work kinds. The behaviour
its answer drives is renamed from **Pane pin** to **Work lift**.

## Context

The weakest rung resolved the pane's directory to a *bound checkout* containing it and
answered with the Task sets bound there. That is silent in the case it most needs to
answer. A repository whose Task set is bound to its trunk gives nothing to a pane standing
in a sibling feature worktree — `harmony`'s set is bound to `…/work/harmony`, a shell in
`…/work/harmony-upload-version-from-same-page` is under neither that path nor any other
binding, and the dashboard opens with nothing lifted. The containment test is right to
refuse: the suffix must not read as "inside `harmony/`". The rung is simply asking a
narrower question than the human is.

It is also narrow in two further ways that share one cause. A set with no **Worktree
binding** at all is unreachable by locality from anywhere, including its own trunk, because
the rung is populated from bindings. And Maps are unreachable by locality entirely: a Map
is Trunk-rooted and has only the **Work session** stamp, which fires solely inside
`pop-map-<id>` — so an editor shell in the repository has the same blind spot for a live
Map that it has for the task set.

Separately, the word. The dashboard spells two different things "pin": the **Pinned action
menu**, which is a mode the human enters and holds, and this, which is an ordering pop
derives and the human never asked for. The monitor dashboard's manual mark, **Following**,
already carries `_Avoid_: Pin` for the same reason. Every description of the derived
behaviour — ADR-0209's prose, `work/snapshot.go`'s own `lifted` variable — already reaches
for *lift*; only the noun was ever "pin".

## Decision

**1. A third pass, `AttributePaneRepository`, beneath the neighbourhood pass.** The ladder
becomes tags → neighbourhood → repository. It is reached only when nothing above answered,
so a bound checkout still beats a mere repository and today's precision where it already
works is untouched. It is a separate seam method rather than a third branch inside
`AttributePaneNeighbourhood` precisely so that first-hit precedence survives: a checkout
answer from *any* kind must outrank a repository answer from *any* kind, which a branch
inside one method cannot express.

**2. The repository pass merges across kinds; the passes above it stay first-hit.** Every
kind holding work in the repository contributes, concatenated in kind precedence order and,
within a kind, in the active sort's order. This is not an exception to the ladder but the
end of a line it already walks: ADR-0209 decision 2 made the weakest rung plural within one
kind ("every bound candidate pins"), and the pass's meaning is plural by construction — *the
work of mine that lives here* — where a tag means *this pane **is** that work*, which is
singular. First-hit here would silently decide that a repository with a Task set has no
Maps.

**3. It answers with every set in the repository's Task storage, bound or not, plus that
repository's Maps.** Restricting it to bound sets would reproduce the silence one layer up:
the binding that made the reported case reachable at all was luck. Volume is already
governed — the pass fires only when the two above it are silent, and ADR-0209 decision 7's
preset absolutism is untouched, so a repository with five Done sets and one open one lifts
one row under `active`.

**4. Repository identity is the git common directory.** It is already on every
`repogroup.Group` as `RepoCommonDir`, it is the identity **Task storage** is keyed under, and
it is true by construction for a sibling worktree. The **Project** name was rejected: it is a
config label, two unrelated repositories can collide on it, and renaming a project would
break the pass.

**5. The pane's repository is resolved once at launch, as a pane fact.** `work.PaneFacts`
gains `RepoCommonDir` and `LaunchPaneFacts` fills it with one `rev-parse --git-common-dir`
beside the one `display-message` round-trip it already makes. Asking during the snapshot
build was the obvious placement and is the wrong one: `MemoGit`'s lifetime is one load by
construction, the dashboard rebuilds every two seconds, so the memo would be discarded
before it helped and the pass would fork roughly 1800 times an hour for an answer that
cannot change — the pane's directory is read once and never re-read. Resolving at launch
also *preserves* ADR-0201 decision 3 rather than straining it: the ladder stays kind-side
holding plain data, and no kind gains a git dependency. `pop work status` builds with empty
pane facts and pays nothing.

**6. The field goes on `work.PaneFacts`, not on `internal/tmux.PaneFacts`.** ADR-0142 gives
that package tmux knowledge and nothing else; a git-derived fact is not tmux's to hold.

**7. Routines stay out, and the asymmetry is deliberate.** ADR-0209 decision 9 refused a
locality rung for Routines because a cwd rung would answer with a project's whole routine
list for any shell in the repo. That objection is not weakened here, it is *specific*: a
routine is project-scoped with no container-level locality to narrow it, its pane is
short-lived, and page B has no equivalent of the preset narrowing that keeps this pass to a
row or two. A Task set and a Map each name a definite piece of work that lives in a definite
repository; a routine names a schedule.

**8. Renamed to Work lift.** Named for what is lifted — Work containers — rather than for
what caused it. "Pane" is tmux's word for the thing pop merely read facts from, and the
parent term **Pane work attribution** rightly keeps it, because attribution really is a
question about the pane. The two subjects differ, so the two prefixes differ. "Pin" is left
to the manual senses.

**9. The preset field is renamed `pin` → `lift`, with `pin` a permanent silent alias.** A
glossary term the config contradicts is a term nobody learns; a key that hard-errors on a
config a human already wrote is worse than a two-line alias. No deprecation warning and no
sunset — the alias is cheap and permanent, and `lift` is the only spelling pop writes.

**10. The rename reaches the code.** `WorkViewPreset.Pin`, `Container.Pinned`, `PinPane`,
`pinAttributed` and their tests carry the retired word into every future reader's grep. The
term and the identifiers move together or the rename has not happened.

## Considered Options

**Widen the existing checkout rung in place.** One rung, no new seam method. Rejected: it
makes a bound checkout stop beating a mere repository, which is the precision the current
rung is *right* about. Two questions with different strengths need two rungs.

**Keep the repository pass first-hit across kinds.** Smaller change, matches the passes
above. Rejected in decision 2 — it wires Maps as an implementor and then guarantees they
never answer in any repository that also has a Task set, which is nearly all of them.

**Resolve the repository fork-free, by walking up for `.git` and parsing the `gitdir:`
pointer and `commondir` file.** Saves roughly ten milliseconds, once, at launch. Rejected:
it buys a hand-rolled reimplementation of git's own worktree resolution — relative gitdir
paths, `GIT_DIR`, submodules — to optimise a cost that is already paid once per session.

**Leave the word alone.** Rejected in decision 8. The collision is inside one surface: the
same dashboard has a manual pin and a derived one, and a reader has no way to tell which a
sentence means.

## Consequences

- `work.Kind` gains a third optional attribution seam, obtained by type assertion like the
  other two. `tasks/setkind` and `wayfinder` implement it; `routine` does not, per decision 7.
- `work.AttributePane` grows a third pass with different arity from the first two. The
  merge is the one place the ladder is not first-hit, and it is commented as such.
- **Bound-checkout pin order** becomes **Bound-checkout lift order** and stays scoped to the
  neighbourhood pass. The repository pass gets no order term of its own: "sort them the way
  they are already sorted" is not a concept.
- A user config carrying `pin = true` keeps working forever and is never rewritten.
- ADR-0201 and ADR-0209 now describe the vocabulary of their own moment rather than the
  current one. The glossary is the live description; that is what it is for.
