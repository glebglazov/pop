---
fragment: 6082C698
generation: 0006
branch: master
---

+ Server socket
  The tmux server pop addresses, selected by config rather than assumed. Defaults
  to tmux's own `default` socket — the one a plain `tmux` creates — so pop lives in
  the user's tmux; setting it to `pop` gives pop an isolated server of its own. It
  is also the identity pop tests itself against: "inside tmux" is not whether
  `$TMUX` is set but whether its socket path matches this one, so a pop command run
  from a personal tmux while pop is aimed elsewhere correctly reads as outside.
  avoid: tmux server, pop server (pop does not own it in the default configuration)
  under: Sessions

~ Session
  The tmux session pop creates or attaches to on the **Server socket** when you
  select a project or worktree. One project maps to one session; selecting it puts
  you in that session (creating it first if needed). Sessions on any other socket
  are not pop's, and pop neither lists nor addresses them.
  was: The tmux session pop creates or attaches to when you select a project or
  worktree. One project maps to one session; selecting it puts you in that session
  (creating it first if needed).
