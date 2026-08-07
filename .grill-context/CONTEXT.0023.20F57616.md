---
fragment: 20F57616
generation: 0023
branch: master
---

~ Checkout locality
  Whether the current directory is the **Trunk worktree** or a linked worktree —
  `trunk` or `worktree`, derived purely from git (`binding.IsLinkedWorktree`,
  the same predicate a drain routes on) and never from config, with a bare
  repository always reading `worktree`. Reported by `pop tasks checkout`
  (`--locality` for the bare word, `--json` for the whole checkout). Distinct
  from **Trunk worktree** resolution, which answers where a managed worktree
  forks *from* and is config-aware.
  avoid: trunk detection, worktree detection, in-trunk
  was: Whether the current directory is the Trunk worktree or a linked worktree —
    trunk or worktree, derived purely from git (binding.IsLinkedWorktree, the
    same predicate a drain routes on) and never from config, with a bare
    repository always reading worktree. Reported by pop tasks checkout
    (--locality for the bare word, --json for the whole checkout) and read by
    to-tasks to pick the registration default: trunk → managed, worktree →
    plain, bound here. Distinct from Trunk worktree resolution, which answers
    where a managed worktree forks from and is config-aware.
