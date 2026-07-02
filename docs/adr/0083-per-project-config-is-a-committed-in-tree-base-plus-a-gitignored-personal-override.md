---
status: accepted
relates: "amends [0078](0078-preferred-workbench-is-a-per-worktree-personal-setting.md); extends [0077](0077-config-precedence-is-scope-first-config-breaks-equal-scope-ties.md)"
---

# Per-project config is a committed in-tree base plus a gitignored personal override

## Context

`.pop.toml` today is a narrow, committed, team-shareable in-tree surface
(**RepoConfig** — `workbenches` only). [ADR-0078](0078-preferred-workbench-is-a-per-worktree-personal-setting.md)
deliberately barred session-shape (**Preferred workbench**) from it *because* a
committed `.pop.toml` imposes one person's taste on the whole team; per-repo
personal config instead lives centrally in `[repo."<path>"]` /
`config.runtime.toml`, keyed by path. Users want per-project config
**co-located** in the repo — committed on solo/personal repos, kept personal on
team repos — and want it to grow toward a fuller settings surface.

## Decision

- **Base + override pair.** `.pop.toml` (committed, team-shareable) and a new
  `.pop.local.toml` (personal), with **local winning**. pop treats both as
  hand-authored config loci and **never inspects git-tracking**: whether either
  file is committed or gitignored is the user's own `git` choice, not a pop
  policy. This **reverses ADR-0078's objection** — session-shape is allowed in
  `.pop.toml` now, because "committed and team-imposing" is no longer an
  assumption pop makes; the user decides per project by committing or ignoring.
- **They slot into ADR-0077's scope × modality lattice** as new hand-authored
  loci. Repo-scope order, finest → coarsest:
  `.pop.local.toml` > `.pop.toml` > central `[repo."<path>"]`. Central `[repo]`
  is **demoted** to the lowest repo-scope hand-authored locus — the escape hatch
  for a repo you cannot or will not drop a file into. Scope still wins first;
  hand-authored still beats CLI-scratch (`config.runtime.toml`) only at equal
  scope. ADR-0077 and ADR-0078 are otherwise unchanged, and the change is purely
  additive — nothing shipped is migrated.

## Considered options

- **Repurpose `.pop.toml` as the personal file.** Rejected: breaks the committed
  team-shareable role (shared workbench blueprints).
- **A single file with pop probing git-status** (`git check-ignore`) to warn when
  a personal-nature key is committed. Rejected: adds git calls to the resolve
  path and re-inserts pop into a choice we just made the user's.
- **Machine-override semantics for central `[repo]`** (it wins over in-tree).
  Rejected: "a central entry silently trumps the file in front of you" is exactly
  the spooky-action that makes layered config infuriating to debug.

## Consequences

The **fuller-surface ambition** (arbitrary global keys becoming legal at
repo/worktree scope, driven by a declarative **settings registry** that carries
per-key allowed-scopes, merge strategy, and natural file) is **deferred**. A
chunk of keys — the project registry, machine-global daemon knobs, the
cross-repo queue — stay global-only *by nature* and a registry would formally
reject them at repo scope. This ADR fixes the file model and precedence only;
the registry and per-key enumeration are follow-on work.
