---
fragment: 1c6b28cc
generation: 0009
branch: 2026-07-01-worktree-native-create
---

~ Worktree picker
  The fuzzy-search picker in `pop worktree` for choosing, creating, or deleting git worktrees in the current repository. Interactive creation is in scope (`ctrl+a`, ADR-0076): pick a **Base branch**, name the new branch/worktree, then `git worktree add`. Queue worktree parallelism remains the separate path where pop owns `git worktree add` for **managed** **Worktree set**s forked from the **Trunk worktree**. User-defined creation commands may still hand a new path back via **Switch**. Deleting a worktree also removes its **History** entry; its tmux session is left alone.
  was: The fuzzy-search picker in `pop worktree` for choosing or deleting git worktrees in the current repository. Interactive picker creation remains out of scope for ordinary worktree navigation, but queue worktree parallelism is the explicit exception where pop owns `git worktree add` for **managed** **Worktree set**s forked from the **Trunk worktree**. User-defined creation commands may still hand a new path back via **Switch**. Deleting a worktree also removes its **History** entry; its tmux session is left alone (killing it stays an explicit, separate action).

+ Base branch
  The ref picked in the interactive worktree-create flow that the new worktree is forked from — the `git worktree add -b <name> <path> <base>` start-point. Distinct from the typed **worktree name**, which becomes the new branch. Shown in the name prompt as `(base: <ref>)`. A remote base (`origin/x`) yields a local tracking branch.
  avoid: source branch, target branch, selected branch
  under: Pickers
