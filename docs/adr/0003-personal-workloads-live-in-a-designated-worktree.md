# Personal workloads live in a designated worktree

> Superseded by [ADR 0039](./0039-issue-sets-live-in-pop-data-dir-keyed-per-repository.md): Issue sets moved out of the repository tree into pop's data dir.

Personal workload artifacts live beneath `thoughts/` in a designated ordinary worktree, typically the main worktree used to start an investigation. The `thoughts/` directory is ignored globally by git. Pop discovers Issue sets from that worktree's workload definition path rather than inferring a location from git's shared directory.

Issue execution starts from a workload runtime path. In the initial iteration, both paths default to the selected project's path and may be overridden for a command. Automatic creation of runtime worktrees is deferred.

## Why

Issue sets and their optional planning documents currently support a personal planning workflow. Keeping `thoughts/` globally ignored prevents planning artifacts from entering product commits and allows the designated main worktree to remain the place where investigations are outlined.

A bare repository's shared git directory is not a workload definition path. It stores repository metadata, while the Issue sets live in an ordinary worktree. Future execution may create separate worktrees for isolated implementation, so discovery and runtime paths remain distinct concepts even though they initially default to the same directory.

## Considered Options

- **Version-control `thoughts/` immediately.** Deferred: team-shared workloads may need this later, but personal planning should not leak into implementation commits.
- **Infer workload definitions from the shared git directory.** Rejected: Issue sets live in a designated ordinary worktree, not in repository metadata.
- **Require separate definition and runtime paths immediately.** Rejected: the common non-worktree workflow should stay simple.
- **Default both paths to the selected project and allow command overrides (chosen).** This keeps the first iteration small without collapsing the two concepts.

## Consequences

Moving to team-shared workload definitions will require an explicit migration from globally ignored local artifacts to version-controlled definitions and separately stored execution state.

Automatic runtime-worktree creation must define how local workload artifacts are made available to the execution checkout without accidentally committing them.
