---
fragment: 6082C698
generation: 0006
branch: master
---

+ Server socket
  The tmux server pop addresses, selected by the global `tmux.socket` config key
  rather than assumed. Defaults to tmux's own `default` socket — the one a plain
  `tmux` creates — so pop lives in the user's tmux; setting it to `pop` gives pop
  an isolated server of its own. The two are exclusive modes, not a preference:
  pop shares the user's server, or pop owns its own and the user does not run tmux
  personally. It is also the identity pop tests itself against — "inside tmux" is
  not whether `$TMUX` is set but whether its socket path matches this one, so a pop
  command run from a foreign server reads as outside and is refused rather than
  nested.
  avoid: tmux server, pop server (pop does not own it in the default configuration)
  under: Sessions

+ Base tmux config
  The complete opinionated tmux configuration pop generates into its data dir and
  passes as `-f` when it starts a server for a user who has no tmux config of their
  own. For that user it is not a supplement to their tmux config — it *is* their
  tmux config, keybindings included. Regenerated rather than edited, and restricted
  to `set-option` and `bind-key` so it can be re-sourced into a server pop already
  started when pop upgrades.
  avoid: default config, tmux.conf (that name belongs to the user's own file)
  under: Sessions

+ Tmux config include
  A user-authored tmux configuration file, its path held in pop config, that the
  **Base tmux config** sources and pop never writes. The seam that lets someone
  running pop's config still bind their own keys.
  under: Sessions

~ Session
  The tmux session pop creates or attaches to on the **Server socket** when you
  select a project or worktree. One project maps to one session; selecting it puts
  you in that session (creating it first if needed). Sessions on any other socket
  are not pop's, and pop neither lists nor addresses them.
  was: The tmux session pop creates or attaches to when you select a project or
  worktree. One project maps to one session; selecting it puts you in that session
  (creating it first if needed).
