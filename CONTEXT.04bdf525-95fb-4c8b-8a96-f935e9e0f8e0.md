# Context delta: worktree parallelism

## Language

### Tasks

**Worktree set**:
A **Task set** drained in its own pop-provisioned git worktree. Worktree sets run in parallel across sets because each set gets an isolated **Runtime path**, while tasks inside one set remain serial because the Task set is still the ordered unit of work.
_Avoid_: Worktree task, per-task worktree, queue shard

**Worktree-ready project**:
A Project whose repo-root `.pop.toml` declares `worktree_ready = true`, meaning a bare `git worktree add` yields a runnable checkout without a project-specific setup command. The flag is declarative capability, not project registration and not executable provisioning.
_Avoid_: Provisioning command, queue-enabled project, registered project

**Mergeability**:
The clean-or-conflicts verdict from pop's no-side-effect `git merge-tree` dry run of a completed **Worktree set** branch against the working branch it forked from. Mergeability is algorithmic evidence for integration routing; it is not semantic validation and does not mean pop has integrated the branch.
_Avoid_: Merge result, integration status, semantic safety
