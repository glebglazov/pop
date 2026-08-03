---
status: accepted
relates: "narrows [ADR-0170](0170-to-tasks-defaults-to-managed-auto-drain.md) — the managed auto-drain default stays valid as the trunk case"
---

# Registration default routes on checkout locality

## Context

ADR-0170 made `--managed --auto-drain` the unconditional default for a task set
published by `to-tasks`: every set gets a fresh pop-owned worktree forked from
the **Trunk worktree**, drained unattended by the Work daemon.

That default is right from the trunk and wrong from a worktree. A human who has
already switched into a checkout — a scratch worktree from the picker, another
set's managed worktree, a hand-made feature worktree — and then breaks work down
there is asking for the work to happen *here*. Provisioning a second worktree
forked from trunk abandons the checkout they deliberately chose, and the set's
panes then open somewhere they are not.

Two further facts shape the fix:

- The rule has to live somewhere one publisher cannot drift from another.
  Registration mechanics are already owned by the Work-store doc
  (`integrate/issue-tracker.md`, installed to `~/.agents/docs/issue-tracker.md`
  per ADR-0169), whose *Register the set* section states the default today, while
  the `to-tasks` skill body restates it in its *Arguments* section. Two statements
  of one default is the drift surface.
- No pop verb answers "is this directory the Trunk worktree?". Wayfinding ticket
  06 established that `pop config show --json` reports *where* the trunk is
  (cwd-invariant), `pop work show-path` is repo-scoped, and the only cwd-relative
  answer is an ANSI-decorated prose line in `pop tasks status` that requires at
  least one registered set to exist. A skill can derive the predicate from
  `git rev-parse --git-dir` vs `--git-common-dir`, but that means a skill body
  restating pop's own routing predicate in prose, plus a hand-written bare-repo
  gate — `IsLinkedWorktree` returns false for a bare repo directory itself,
  contradicting its own doc comment.

## Decision

**The registration default routes on checkout locality, and the Work-store doc
owns the rule.**

1. **A new read verb: `pop tasks checkout`.** `--locality` prints exactly one
   line, `trunk` or `worktree`; `--json` prints the whole checkout —
   `path`, `locality`, `branch`, `trunk_path` (omitted when unresolvable),
   `bare`, `managed`. Read-only, needs no registered set, a sibling of
   `pop tasks show-path`. The scalar mode exists because a skill body must not
   depend on `jq`, which pop does not ship.

2. **`locality` is pure git, config-blind.** It derives from
   `binding.IsLinkedWorktree` — the predicate `pop tasks implement` already
   routes on — so the verb can never contradict where a drain actually lands. A
   checkout declared trunk in config that is nonetheless a linked worktree reads
   `worktree`, because that is what the drain does with it. `trunk_path` and
   `bare` ride along in `--json` as information, never as inputs to `locality`.
   **A bare repo is always `worktree`**, closing the `IsLinkedWorktree`
   false-negative on the bare directory itself.

3. **The default branches on it.** From `trunk`, `--managed` as before, forking a
   worktree from trunk. From `worktree`, **plain register bound to this
   checkout** — no new worktree — including when this checkout is already another
   set's managed worktree, where the second set binds alongside rather than
   provoking a managed worktree on top of a managed worktree.

4. **`--auto-drain` is the default in both localities.** It no longer depends on
   `--managed`. The clause "auto-drain is never applied without `--managed`" is
   retired outright: `--auto-drain` only sets the set's consent bit
   (`applyRegisterAutoDrain`, `cmd/tasks.go:495`) and never had a binary-level
   tie to provisioning. Standing in a deliberately-chosen non-trunk worktree is
   the isolation the old clause was reaching for.

5. **Explicit keywords beat detection.** `managed` / `isolated` typed from inside
   a worktree still provisions a fresh managed worktree forked from trunk — the
   escape hatch for "I am in a worktree but want this isolated anyway".
   `no-drain` / `manual` are unchanged in both localities.

6. **The trunk-less fallback narrows to the trunk branch.** "Default tried
   `--managed`, repo has no resolvable trunk, retry plain and warn" can only fire
   where the default asks for `--managed` — the trunk branch. In the worktree
   branch the default is already plain, so no trunk resolution is attempted and
   the fallback is unreachable. The doc says this, so the two paths do not read
   as contradictory.

7. **The doc owns the rule; the skill defers.** *Register the set* states the
   branch and the semantics. `to-tasks`'s *Arguments* section keeps only the
   keyword→flag mapping and carries no default of its own.

The two trunk notions stay deliberately separate: **locality** answers *where am
I standing* (git, config-blind); **trunk resolution** answers *where does a
managed worktree fork from* (config-aware, `--trunk`). Only the second consults
config, and only in the trunk branch.

## Considered Options

- **Skill-side `git rev-parse` comparison, no new verb.** Zero build. Rejected:
  it copies pop's routing predicate into a markdown body, where it can drift
  silently from the Go it is imitating, and the bare-repo gate has to be
  hand-written in prose beside it.
- **`pop config show --json` → `.current_repo.trunk`, compared to the cwd.**
  Already exists and is override-aware. Rejected as the locality source: ticket 06
  showed it reports TRUNK for a bare repo whose trunk was declared with
  `--trunk`, while the drain adopts that same checkout as an integrateable
  worktree binding — the verb and the drain would disagree in exactly the case
  the routing rule cares about.
- **`--json` on `pop tasks status` instead of a new verb.** Rejected: it keeps
  the "needs ≥1 registered set" precondition, which is wrong for a probe whose
  whole job is to run before anything is registered.
- **Rule in the `to-tasks` skill body.** Rejected: `pop map fan-out`, `to-spec`
  and any future publisher read the doc, not the skill. Registration defaults are
  store mechanics.
- **Rule in both, doc as reference.** Rejected as the drift surface this ADR
  exists to close.
- **Drop `--auto-drain` in the worktree branch.** The literal reading of today's
  doc. Rejected: it makes the path a human chose deliberately quietly worse, for
  no safety gain — the hazard the clause guarded was unattended draining *of the
  trunk*.
- **Amend ADR-0170 in place.** Rejected: the locality branch is a new decision on
  top of that one, not a correction of it. ADR-0170 remains valid as the trunk
  case.
- **Verb named `pop tasks locality`.** Rejected: `checkout` reads as a noun
  beside `show-path`, and the `--json` payload describes the checkout as a whole.
  `locality` is the flag.

## Consequences

- A human who breaks work down inside a worktree gets the set bound there,
  auto-drained, with its panes in that checkout's session — no second worktree,
  no set opening somewhere they are not.
- Registering from a worktree can bind a set to a checkout that already holds
  another set's binding. That is the shared-checkout case the
  **Managed-worktree teardown reference count** already covers; it is now
  reachable by default rather than only through a manifest directive.
- One more read verb on `pop tasks`, and one new fact for a skill to fetch before
  registering.
- "auto-drain requires `--managed`" stops being true anywhere. Any surface
  restating it needs the clause removed, not reworded.
- `locality` reporting `worktree` for a config-declared trunk that is a linked
  worktree is correct-by-construction but will read as surprising to whoever set
  `trunk = true`. The doc states the git-derived rule in one line so the question
  has an answer.
- A bare repo routes to plain register bound in place. Ticket 06 found that
  `pop tasks register` in a bare repo reports success and persists nothing —
  a pre-existing defect this decision does not fix and now leans on.
