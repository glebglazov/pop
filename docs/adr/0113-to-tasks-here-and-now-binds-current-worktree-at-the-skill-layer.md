---
status: superseded by ADR-0115
---

# `to-tasks-here-and-now` binds the current worktree at the skill layer

> **Superseded by [ADR-0115](0115-to-tasks-always-writes-the-worktree-directive-here-and-now-retired.md):** `to-tasks-here-and-now` is retired. `to-tasks` itself now always writes the worktree directive (defaulting to the current checkout's name, uniformly and without the trunk/managed/bound guard below), and `auto_drain` becomes an explicit `to-tasks` argument. The guard's premise that a `{ "name": "<trunk>" }` directive is not Queue-drainable was found to be false.
