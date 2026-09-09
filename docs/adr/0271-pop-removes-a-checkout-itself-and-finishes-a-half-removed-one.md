# Pop removes a checkout itself, and finishes a half-removed one

`git worktree remove --force` walks the tree with git's `remove_dir_recurse`,
which **breaks on the first entry it cannot remove**. When a live process is
writing into the checkout — a bootsnap cache under `tmp/`, a puma, an fsmonitor
daemon — the walk aborts part way, and it has usually already deleted the
checkout's `.git` file. Git then refuses every later removal with `validation
failed, cannot remove working tree: '<path>/.git' does not exist`, so the
checkout can never be deleted through pop again while `git worktree list` keeps
listing it as `prunable`. One repository accumulated eight such directories
before anyone noticed, because `deleteWorktree` wrote the failure to stderr and
the **Worktree picker** repainted over it in the next frame.

**Checkout removal** is therefore pop's own act, not git's: a recursive delete
that does not stop at the first entry it cannot remove, then `git worktree
prune`, then the checkout's **History** entry. Both call sites use it — the
picker's delete and `binding.TeardownWorktree`. It **finishes a Half-removed
checkout** rather than refusing one, because the human's intent was the same
either way and a second verb for "gone, but harder" is vocabulary nobody
remembers at the moment they need it. Processes still holding the directory are
named before it proceeds, not treated as a refusal: pop cannot see every holder
(fsmonitor is a daemon, not a shell in the directory), so a refusal would turn a
delete into a scavenger hunt while still missing cases. What a pass cannot empty
is reported by path.

## Consequences

Pop hand-rolls a recursive delete beside a git command that appears to do the
job. That is the point: the git command's abort-on-first-error walk is the
defect, so wrapping it or parsing its output only re-enters the same trap.
