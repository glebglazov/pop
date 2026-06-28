---
status: deferred
---

# Worktree-parallel execution is a deferred Task-set feature, not a Queue feature

> ⚠️ **DEFERRED — intended design, not shipped behavior.** Original design; implementation decisions recorded in ADR-0029 and ADR-0030.

The parked note `queue-worktree-parallelism.dormant.md` framed "drain several of a project's Task
sets concurrently, each in its own worktree" as a future **Queue** capability. A design pass
showed the framing was wrong and resolved the open questions. This ADR records the reframe and
the resolved shape so a revival starts here instead of re-deriving; it supersedes and replaces
that dormant note. It is **deferred** — none of this is built. It is filed now because several of
the decisions are hard to reverse (a `.pop.toml` format, overriding "worktree creation is not
built in") and worth pinning before code exists.

## The reframe: the checkout is the unit, not the Queue

The dormant note treated this as the Queue learning a new trick. It isn't. The **Runtime
execution lock** is per-canonical-checkout, and **Runtime path** "may be overridden for a
command." So "run this Task set in a worktree" is just *a Runtime-path override to a freshly
provisioned checkout* — which already has its own lock and therefore already drains concurrently
with everything else. The real unit of parallelism was always the **checkout**; "parallel across
projects" (ADR-0027) was only ever "parallel across checkouts," with one Project standing in for
one checkout. Give a Project a second checkout and the existing supervisor fans out across both
with **no scheduler change**.

So the feature's home is **Task set + Project + Runtime path**, not the Queue. The Queue is a
beneficiary. The genuinely new work is worktree provisioning and merge reconciliation, both of
which live outside the scheduler. This also resolves the note's open "drop to per-task dispatch?"
question: **no** — per-set dispatch (ADR-0027) is unchanged.

## Resolved design

- **Unit of a worktree run = a Task set, never a single task.** A Task set is an ordered plan;
  task N builds on N-1. Fanning tasks of one set into isolated worktrees would have each agent
  work blind to the others' prerequisite code — reconciliation would be a blind redo, not a
  merge. So parallelism is **across independent sets**; within a set, tasks stay serial in one
  worktree. Isolation (own checkout per set) is the substrate; whether two sets were truly
  independent is a bet that gets **settled at integration time**, not asserted up front.

- **Capability is declared, not executed.** A repo-root `.pop.toml` carries per-project drain
  behaviour; its first member is a declarative `worktree_ready` flag. It does **not** register a
  project (global config's glob registry still does that) and it contains **no command**, so it
  has no execution attack surface. Because nothing in `.pop.toml` runs, **pop itself owns
  `git worktree add`** — which **overrides the standing "worktree creation is not built in"**
  stance (CONTEXT.md, Worktree picker), now recorded as the implementation decision in ADR-0029.
  The first cut therefore serves **zero-setup projects only**: a bare `git worktree add` must
  yield an immediately runnable checkout (no `npm install`, no `.env` copy). An *executable*
  provisioning command for projects that need setup is deliberately left for later — and that is
  where a trust model (the command is repo-sourced shell run by an unattended daemon) would have
  to re-enter.

- **Branch model.** Each Worktree set forks off the working branch's HEAD at spawn and drains on
  its own branch; the reconcile target is that same **working branch** (continuing v1's "implement
  commits where you are"), serialized by trunk's Runtime execution lock. Acceptable because
  `pop queue run` is standing AFK consent that the operator isn't hand-editing trunk meanwhile; a
  dedicated integration branch is a possible opt-in safe mode, not the default.

- **Merge is git-algorithmic; pop never integrates unattended.** A clean git merge is *textual,
  not semantic* — two sets can merge with zero conflicts and still break trunk. So the daemon
  produces Worktree-set branches and **stops there**. On set DONE, pop computes **mergeability**
  with a no-side-effect dry run (`git merge-tree`) and reports each set as *merges clean* or
  *conflicts* via `pop queue status`. The **human triggers** integration: a one-shot `pop` command
  for clean sets, an attended agent (ADR-0012) for conflicted ones. Conflict resolution is
  **necessarily an agent** — pop makes zero model calls (ADR-0024), so it cannot resolve
  semantically itself. This no-unattended-integration boundary is recorded as the implementation
  decision in ADR-0030. An opt-in `auto_merge_clean` per-project knob exists for operators who
  accept the semantic risk; it is **off by default**.

- **Lifecycle.** A worktree is torn down on clean reconcile + set DONE; **kept** when a set is
  awaiting integration or its merge is parked, so the human can inspect it — mirroring the Queue's
  "finished panes are kept as a visible log."

- **No new quota mechanism.** More parallel worktrees burn agent quota faster, but the **global
  per-agent cooldown** in Queue agent fallback (quota is per subscription) already freezes every
  set using an exhausted agent across all projects and worktrees. Fan-out is self-correcting, not
  a bug. A per-project concurrency cap, if ever needed, belongs in **global queue settings**
  (an operator/subscription concern), not in `.pop.toml` (a repo-capability concern). None is
  added in the first cut (YAGNI).

## Why deferred

v1's value is the supervisor + cross-project fan-out + agent fallback. Worktree parallelism is an
orthogonal multiplier that adds real complexity (worktree lifecycle, mergeability reporting,
integration UX) and should not gate v1. It only pays off for zero-setup projects, which most
projects are not on day one.

## Considered options

- **Keep it a Queue feature (the dormant note's framing).** Rejected — the checkout, not the
  Project, is the unit; the scheduler needs no change, and the hard parts live in the
  Task-set/Runtime-path domain.
- **Worktree unit = a single Task.** Rejected — tasks within a set are an ordered plan;
  parallelizing them produces blind redo, not a merge.
- **Executable provisioning command in `.pop.toml` for the first cut.** Rejected for now — it
  reintroduces a repo-sourced-shell trust surface under an unattended daemon. A declarative flag
  serves zero-setup projects with zero surface; defer the command until a real setup-needing
  project demands it.
- **Auto-merge clean results unattended.** Rejected — "clean" is textual, not semantic; silent
  corruption of trunk is the worst failure mode. Surface mergeability and let the human decide,
  consistent with pop's park-don't-surprise posture.
- **pop calls an LLM to resolve conflicts.** Rejected — violates ADR-0024 (pop makes zero model
  calls); conflict resolution is necessarily delegated to an agent.

## Consequences

- Pop now owns `git worktree add` for Worktree sets, **overriding** the "creation is not built in"
  stance in CONTEXT.md for that execution path; ADR-0029 records the override and the glossary
  now names the exception.
- Introduces a repo-root `.pop.toml` (project-local, declarative; first use is `worktree_ready`)
  as a new config surface distinct from global `config.toml`.
- The revival glossary terms are now live via a CONTEXT fragment: **Worktree set**,
  **Worktree-ready project**, **Mergeability**.
- The Queue is unchanged: per-set dispatch stays, and more checkouts simply use the parallelism
  the lock-per-checkout source of truth already permits.
