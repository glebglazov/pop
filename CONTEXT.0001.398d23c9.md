---
fragment: 398d23c9
generation: 0001
branch: queue-worktree-window-placement
---

+ Queue window
  The single tmux window, named `pop-queue`, that the Queue daemon spawns its drains into within a Project's Session. All queue-spawned drains for that project — both in-place and Worktree set — land here as panes under a balanced (`tiled`) layout, instead of in the user's working windows or in per-worktree sessions. One Queue window per project session; created on first spawn, reused thereafter.
  avoid: drain session, worktree session, queue tab
  under: Tasks

~ Worktree set
  A Task set drained in its own pop-provisioned git worktree under a Worktree binding. The checkout is an ephemeral execution context, not a navigable project peer: pop does not auto-create a session for it. It is still a registered git worktree, so it remains reachable on demand via the Worktree picker, which creates a session only when the human selects it.
  was: A Task set drained in its own pop-provisioned git worktree under a Worktree binding. Worktree sets run in parallel across sets because each set gets an isolated Runtime path, while tasks inside one set remain serial because the Task set is still the ordered unit of work.
