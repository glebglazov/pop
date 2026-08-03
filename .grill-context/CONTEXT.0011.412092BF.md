+ Checkout locality
  Whether the current directory is the **Trunk worktree** or a linked worktree —
  `trunk` or `worktree`, derived purely from git (`binding.IsLinkedWorktree`,
  the same predicate a drain routes on) and never from config, with a bare
  repository always reading `worktree`. Reported by `pop tasks checkout`
  (`--locality` for the bare word, `--json` for the whole checkout) and read by
  `to-tasks` to pick the registration default: `trunk` → managed, `worktree` →
  plain, bound here. Distinct from **Trunk worktree** resolution, which answers
  where a managed worktree forks *from* and is config-aware.
  avoid: trunk detection, worktree detection, in-trunk

+ pop tasks checkout
  The read-only verb reporting the current checkout's **Checkout locality**.
  `--locality` prints one bare line, `trunk` or `worktree`, so a skill needs no
  JSON parser; `--json` prints `path`, `locality`, `branch`, `trunk_path`
  (omitted when unresolvable), `bare` and `managed`. Needs no registered task
  set, unlike the `Checkout:` line in `pop tasks status`. Sibling of
  `pop tasks show-path`.
