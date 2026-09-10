---
fragment: 1F9137E4
generation: 0061
branch: master
---

+ Setup command
  A shell command a **Workbench** runs for its side effects before any of its windows exists. It is a command *line*, so an ordered chain within it is the shell's own `&&`.
  avoid: before_apply command, pre-command, hook command, lane, step
  under: Workbench

+ Setup group
  A set of **Setup command**s a **Workbench** declares as independent of each other, and which therefore run together rather than in sequence. Group boundaries are barriers — everything in one group finishes before the next begins — so two chains that must not wait on each other are two commands in one group, never two groups.
  avoid: parallel stage, stage, phase, batch
  under: Workbench

+ Setup progress line
  The single refreshing line pop draws while a **Setup group** runs, naming every command in the group and its elapsed state. It exists because a group's output is held back until the group ends, and silence is indistinguishable from a hang. Falls back to plain start and finish lines where there is no terminal to refresh.
  avoid: spinner, progress bar, status bar
  under: Workbench

+ Human shell
  The shell pop interprets a human-authored command with: the one a tmux pane would get, started so that the human's own functions and aliases resolve. It is what makes a **Workbench** speak one dialect throughout, rather than one for a pane and another for setup.
  avoid: login shell, interactive shell, user shell, default shell
  under: Workbench

~ Workbench
  A named blueprint for the shape of a whole tmux **Session** — its **Setup group**s, its named windows, and, within each window, a **Layout** (an explicit weighted split tree of **Pane spec**s). Defined in global config, a repo's `.pop/config.toml`, or a global `[repo."<path>"]` block; resolved per checkout as a most-specific-wins union by name. Instantiated into a live **Session** by `pop workbench apply` (alias `wb`). The whole-session thing, one tier above a **Layout**. Formerly called "Session template".
  was: A named blueprint for the shape of a whole tmux **Session** — its named windows and, within each window, a **Layout** (an explicit weighted split tree of **Pane spec**s). Defined in global config, a repo's `.pop/config.toml`, or a global `[repo."<path>"]` block; resolved per checkout as a most-specific-wins union by name. Instantiated into a live **Session** by `pop workbench apply` (alias `wb`). The whole-session thing, one tier above a **Layout**. Formerly called "Session template".
