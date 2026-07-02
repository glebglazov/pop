---
status: accepted
supersedes: [ADR-0084, ADR-0085]
---

> **Relates:** amends [ADR-0078](0078-preferred-workbench-is-a-per-worktree-personal-setting.md); generalizes the precedence law of [ADR-0077](0077-config-precedence-is-scope-first-config-breaks-equal-scope-ties.md).

# Repo config is one shared schema usable in .pop.toml and [repo."<path>"] blocks

## Context

`RepoConfig` (the `.pop.toml` schema) and `RepoOverrideConfig` (the global
`[repo."<path>"]` schema) had drifted: `.pop.toml` accepted only `workbenches`,
while `[repo]` also accepted `trunk` and (ADR-0078) `preferred_workbench`. We
want repo-scoped settings authored **either** in the repo (`.pop.toml`, flat)
**or** centrally (`[repo."<path>"]`), from one key set, so adding a repo key once
makes both accept it.

This session first explored a much larger design — a fuller in-tree config
surface, a committed base + gitignored personal override (`.pop.local.toml`), and
a general leaf-keyed settings registry with a merge engine. We narrowed away from
all of it: only a couple of keys are genuinely repo-specific, so that machinery
was over-built. This ADR records the narrow result and supersedes the two ADRs
written for the abandoned design (ADR-0084 two-anchor engine rationale, ADR-0085
registry).

## Decision

- **One shared repo-scope schema.** `.pop.toml` and `[repo."<path>"]` decode the
  same key set. `.pop.toml` is the flat in-repo form; `[repo."<path>"]` is the
  central, path-keyed form. Adding a repo-scope key defines it **once** and both
  loci accept it. The sole exception is `trunk` — per-checkout machine topology,
  central `[repo]`-only, never valid in `.pop.toml` (a repo cannot name a
  machine-specific trunk; ADR-0078).
- **Curated, not general.** Repo scope holds only genuinely repo-specific keys —
  today `workbenches` and `preferred_workbench` — with room to add more. It is
  **not** a mirror of global config: `projects`, `queue`, daemon knobs, and other
  machine/global keys stay global-only and are rejected at repo scope.
- **`preferred_workbench` is repo-legal** (amends ADR-0078, which barred it from
  `.pop.toml`). A repo may commit its default session shape; whether the file is
  committed or gitignored is the author's git choice — pop never inspects
  tracking.
- **Precedence: personal beats committed at repo scope.** A user's
  `[repo."<path>"]` overrides the repo's committed `.pop.toml` for the same key.
  Full order, finest → coarsest:

  ```
  1  ./.pop.toml                        worktree · in-tree
  2  config.runtime.toml[<wt-path>]     worktree · CLI-scratch (ctrl+w)
  3  [repo."<path>"]                    repo · personal/central   (beats committed)
  4  <trunk>/.pop.toml  (→ id-root)     repo · committed/in-tree
  5  config.runtime.toml[<trunk-path>]  repo · CLI-scratch (ADR-0078 inheritance)
  6  config.toml                        global
  7  config.runtime.toml integrations   global · CLI-scratch
  8  embedded default
  ```

  Scope wins first (ADR-0077); within a scope hand-authored beats CLI-scratch;
  within repo hand-authored, personal `[repo]` beats committed `.pop.toml`.
- **Inheritance: two anchors, presence decides.** pop resolves repo-scope in-tree
  config at two anchors — this worktree (worktree scope) and the **Trunk
  worktree** (repo scope), the trunk read falling back to the **Repository
  identity** root for a bare repo. A worktree with its own `.pop.toml` overrides
  the inherited trunk one; a worktree without inherits trunk's. Reuses ADR-0078's
  `Deps.Trunk` resolver, its no-trunk fallthrough, and its this-is-trunk
  read-once guard.

## Considered options

- **Fuller in-tree surface + base/override (`.pop.local.toml`) + general settings
  registry.** Explored earlier this session; dropped. Only ~2 keys are genuinely
  repo-specific, so a general merge engine and a gitignored personal-override file
  were over-built. Personal per-repo override is served by central
  `[repo."<path>"]`; per-worktree quick override by `ctrl+w`→runtime; co-located
  repo content by committed `.pop.toml`.
- **Demote `[repo]` below committed `.pop.toml`** (an intermediate position this
  session). Rejected: `[repo]` is the *user's* personal, machine-local authoring
  of repo behaviour and should override what a cloned repo committed — personal
  beats committed. (This also restores the pre-session base behaviour.)
