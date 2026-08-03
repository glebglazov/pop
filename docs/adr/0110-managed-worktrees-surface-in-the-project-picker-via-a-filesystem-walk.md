# Managed worktrees surface in the project picker via a filesystem walk

## Context

**Managed** worktrees live outside the configured project globs — under the queue
data dir at `<dataDir>/queue/worktrees/<repoKey>/<worktreeName>` (see
[ADR-0071](0071-managed-worktree-teardown-happens-only-at-archive-unbind-is-forget-only.md)) —
so `pop project`'s file-based expansion never saw them, even though the **Worktree**
glossary term already promises that every worktree "appears in the picker." We want
them listed, with minimal effect on the picker's startup cost.

## Decision

`pop project` discovers managed worktrees with a single **filesystem walk** of
`<dataDir>/queue/worktrees/` (`ReadDir` the root → each `<repoKey>` dir → its
worktree dirs), emitting each as a flat project entry: display name
`<basename>/<worktreeName>` (basename = repoKey minus its `-<shortHash>` suffix) and
session name `basename/worktreeName`, matching the drain's own **Session name** so a
live session dedupes. The walk runs once (not per configured project) and touches
**neither `pop.db` nor git**, keeping project expansion store- and fork-free.

## Considered Options

- **Query `pop.db` bindings** — authoritative about what is bound, but adds a store
  open to a hot path that currently never touches the store, and can list dirs that
  no longer exist on disk.
- **`git worktree list` per repo** — accurate but forks git once per configured repo
  during expansion (tens of ms each). Rejected on perf grounds.

We chose the filesystem walk because its cost is bounded by managed-worktree count
(a handful of `ReadDir`/`Stat` calls, run concurrently with the existing file-based
expansion), and filesystem presence — not a store record — is exactly what decides
whether a checkout is openable.

## Consequences

The picker can show an orphaned managed dir the store no longer tracks, or miss a
freshly-recorded binding whose dir does not yet exist; both are acceptable because
the picker's job is to open real directories. The list is scoped to disk, so managed
worktrees for repos absent from config still appear.

**Amended by the `pop queue` → `pop work` cut:** the root walked is now
`<dataDir>/work/worktrees/`, and the walk runs once per **managed-worktree root**
rather than once — a machine whose gated root move has not completed still has
worktrees under the retired `queue/worktrees`, and a checkout the picker cannot
open is worse than one extra `ReadDir` of a directory that is usually absent. The
decision itself — a filesystem walk, no store and no git — is unchanged. See
ADR-0174 decision 8.
