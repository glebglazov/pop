---
fragment: 54fab5dd
generation: 0008
branch: master
---

~ Worktree picker
  The fuzzy-search picker in `pop worktree` for choosing, creating, or deleting git worktrees in the current repository. Interactive creation is a first-class action (`ctrl+a`): pick a branch, name the worktree, pop runs `git worktree add`, then opens the session — shaped by a **Workbench** when `[workbench] pick_on_create` is set, else flat — and attaches. This reverses the former navigation-and-deletion-only boundary; queue **Worktree set** parallelism is no longer the sole place pop owns `git worktree add`. User-defined creation commands may still hand a new path back via **Switch**. Deleting a worktree also removes its **History** entry; its tmux session is left alone (killing it stays an explicit, separate action).
  was: The fuzzy-search picker in `pop worktree` for choosing or deleting git worktrees in the current repository. Interactive picker creation remains out of scope for ordinary worktree navigation, but queue worktree parallelism is the explicit exception where pop owns `git worktree add` for **managed** **Worktree set**s forked from the **Trunk worktree**. User-defined creation commands may still hand a new path back via **Switch**. Deleting a worktree also removes its **History** entry; its tmux session is left alone (killing it stays an explicit, separate action).
