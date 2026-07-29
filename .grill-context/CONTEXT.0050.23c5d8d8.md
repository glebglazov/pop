---
fragment: 23c5d8d8
generation: 0050
branch: master
---

+ Spawn window
  The single tmux window, named `pop-spawn`, that every named pane created by `pop pane create` lands in within a Project's session — spawned agent CLIs and long-running processes alike, tiled alongside each other. Sibling of the **Queue window**: one per project session, created on first spawn, reused thereafter. Supersedes the window formerly named `agent`; live `agent` windows are left where they are rather than renamed or adopted.
  avoid: agent window, agent tab, spawn tab, pane window
  under: Language

~ Pane ID target
  A raw tmux pane identifier used as an explicit command target, such as `%63`. A Pane ID target is global within tmux and bypasses Pop's name-based **Spawn window** lookup.
  was: A raw tmux pane identifier used as an explicit command target, such as `%63`. A Pane ID target is global within tmux and bypasses Pop's name-based agent-window lookup.

~ Pane skill
  The embedded skills that teach an agent to drive `pop pane` — driving panes (`tmux-pane`) and spawning another agent CLI into one (`spawn-agent`). Installed together via the **Integration component id** `pane-skills`, each resolved under the **Skills prefix**. Still selected in config via the **Integration skill alias** `pane`. An opt-in **Integration component**; pane monitoring works without it.
  was: The embedded skill that teaches an agent to drive `pop pane`. Installed via the **Integration component id** `pane-skills` (one resolved skill, typically `tmux-pane` when **Skills prefix** is empty). Still selected in config via the **Integration skill alias** `pane`. An opt-in **Integration component**; pane monitoring works without it.
