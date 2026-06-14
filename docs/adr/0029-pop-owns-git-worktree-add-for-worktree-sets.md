# Pop owns `git worktree add` for Worktree sets

Status: accepted

ADR-0028 deferred worktree-parallel execution but called out one deliberate reversal: the standing
Worktree-picker language said worktree creation was not built in. Now that Worktree sets exist,
Pop owns `git worktree add` for that one execution path. The Worktree picker remains a navigation
and deletion surface; the override belongs to Queue-driven Task-set execution.

## Why

A **Worktree set** is a Task set drained in its own checkout. That checkout is the isolation
boundary that lets sets run concurrently while preserving serial execution inside each set. If
Pop is the supervisor starting the set, Pop also has to own the checkout path, branch name, and
runtime lock relationship. Delegating creation to user commands would make the unattended
supervisor depend on out-of-band state it cannot reason about.

The first supported project contract is intentionally narrow: a repo-root `.pop.toml` may declare
`worktree_ready = true`. That says a bare `git worktree add` produces a runnable checkout. It is
declarative, does not register the project, and does not run repo-sourced shell. This preserves
ADR-0028's trust boundary while giving zero-setup projects real parallelism.

## Considered options

- **Keep all worktree creation outside Pop.** Rejected — Queue would be unable to provision the
  isolated Runtime paths that define Worktree-set execution.
- **Add creation to the interactive Worktree picker generally.** Rejected — the built need is
  unattended execution provisioning, not a broader picker workflow.
- **Accept a project provisioning command in `.pop.toml`.** Rejected for this cut — an unattended
  daemon running repo-sourced shell needs a trust model that the declarative `worktree_ready` flag
  avoids.

## Consequences

- The glossary no longer says worktree creation is never built in. It names Queue worktree
  parallelism as the explicit exception while preserving the picker boundary for ordinary
  navigation.
- Worktree-ready projects are limited to checkouts that run after a plain `git worktree add`.
  Projects needing installs, generated files, secrets, or environment setup stay ineligible until
  a separately designed provisioning command exists.
- ADR-0028's deferred note is fulfilled by this ADR for the creation-ownership portion; the
  broader queue/Task-set framing remains unchanged.
