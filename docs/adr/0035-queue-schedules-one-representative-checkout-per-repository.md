# Queue schedules one representative checkout per repository; bindings route per-set and may adopt user worktrees

Status: accepted — amends ADR-0027 (scheduling unit) and reverses the "no auto-adopt" stance of ADR-0028

## Context

Running `pop queue run` against a bare repo with many worktrees (e.g. `game_server` with `main`, `unification-experiment`, … as sibling checkouts) spawned the **same** Ready set as a drain **once per worktree** — ten `pop tasks implement <set>` runs in one tick, ten unattended agents committing the same task onto ten different feature branches.

Root cause is a unit mismatch. All worktrees of one repo share one **Repository identity** and therefore one **Task storage** and one set of Ready sets (CONTEXT.md). But a glob expands each worktree into a separate picker **Project**, and the Queue scanned and dispatched **per picker-Project** (ADR-0027's "one drain per idle project," written when "one Project stood in for one checkout"). The **Runtime execution lock** could not dedup them: it is a *physical checkout mutex* (`_Avoid_: Global task lock`), keyed per checkout, so ten distinct checkouts meant ten distinct locks and ten agents launched. The lock is *designed* to let isolated worktrees run concurrently (ADR-0028) — exactly the wrong guarantee here.

## Decision

The Queue's scheduling unit is **Repository identity**, not the picker Project. It collapses a repo's worktrees and dispatches at most one drain per idle repository per Ready set, routing to a single representative checkout resolved in order:

1. **per-set Worktree binding** — explicit routing for one set (see below);
2. **explicit `queue_base = true`** — a per-checkout flag in a global-config `[repo."<path>"]` override block; applies to any layout, and designates a non-bare repo's base when its git main worktree is not the wanted one;
3. **the repo's git main worktree** — the no-config fallback for any non-bare repo, even one with linked worktrees;
4. otherwise (bare repo, no `queue_base`) — **refuse and report**; a bare repo has no git main worktree, and the Queue never guesses a checkout.

**Worktree binding becomes the universal drain router**, decoupled from `worktree_ready`. A binding carries a `Provisioned` bit: **managed** (pop ran `git worktree add`; pop tears the checkout down on integration/abandon — ADR-0028/0029) versus **adopted** (a human pointed an existing, owned worktree at a set via `pop queue bind-worktree <set>`; pop drains into it but **never deletes it**; `abandon` only forgets the binding). Bindings default to adopted/never-delete so a hand-written or unrecognized binding can never trigger a directory deletion — pop deletes only what it demonstrably created.

As a backstop, `pop tasks implement` refuses to start if the same (repository, set) is already live in **any** checkout — grouping the SetID-carrying runtime locks by Repository identity — so at most one agent per (repo, set) ever launches, even under a future scheduler bug, a daemon restart, or a hand-run drain.

## Considered options

- **Aggregate picked-up across a repo's checkouts but keep per-worktree dispatch.** Rejected: state is read at tick start, so within one tick every worktree still sees "not running" and all dispatch. Fixes nothing for the single-tick storm that actually occurred.
- **A genuine per-set advisory lock** (the rejected "global task lock"). Rejected: re-implements the runtime lock's PID-liveness machinery, still spawns N wasted panes, and leaves the target checkout nondeterministic.
- **Heuristic representative (folder named `main`, or the trunk branch).** Rejected: game_server has no checkout on trunk (`master`) and its folder named `main` is on a feature branch — both heuristics either misfire or skip the repo. Git's main worktree (non-bare default) plus an explicit `queue_base` override is unambiguous.
- **Make `queue_base` bare-only.** Rejected: a non-bare layout may also want a checkout other than git's main worktree as its base, so the flag overrides for any layout; the git main worktree is only the no-config fallback.
- **Universal opt-in (every repo must declare a base).** Rejected: the storm came only from bare/multi-checkout repos; single-checkout and non-bare repos were never ambiguous, so forcing a declaration there is friction without safety.
- **Auto-pick the most-recently-active checkout for unbound bare repos.** Rejected: trades a loud 10× storm for a silent "why did my work land on *that* branch?" — the same surprise, quieter.

## Consequences

- Amends ADR-0027: "one drain per idle **project**" becomes "per idle **repository**." Cross-project (cross-repository) parallel fan-out is unchanged for the common single-checkout / non-bare case.
- Reverses ADR-0028's "pre-binding orphan checkouts are not auto-adopted": `bind-worktree` is deliberate human adoption, and the `Provisioned` bit keeps teardown safe.
- New global-config surface: `[repo."<path>"]` override blocks (the `.pop.toml` RepoConfig subset — `worktree_ready`, `auto_merge_clean`, `queue_base` — at higher priority than `.pop.toml`), keyed by any path that canonicalizes to a Repository identity. Machine-specific routing lives here, never in branch-riding `.pop.toml`.
- New command `pop queue bind-worktree <set>`, sibling to `pop queue abandon`; refuses to re-point a bound set without `--force` and never while the set holds a live lock.
