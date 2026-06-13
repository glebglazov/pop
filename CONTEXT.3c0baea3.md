---
fragment: 3c0baea3
branch: master
---

~ Queue
  A daemon that supervises per-project Task-set draining, fanning `pop tasks implement`
  runs out concurrently across registered projects into tmux. Each project drains its own
  Ready sets serially by local Task set priority (enforced by the Runtime execution lock);
  projects run in parallel. Global cross-project priority ordering is a non-goal.
  was: (reserved, not implemented) Reserved for a future machine-global scheduler that picks the next Task set across all projects by priority and runs it. Do not use "queue" for today's per-repository scheduling; it has no current definition.
  under: Tasks
