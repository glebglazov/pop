---
fragment: 96DFF404
generation: 0017
branch: master — RunTaskSet decomposition grill (handoff item 4)
---

+ Implement run
  One invocation of a whole-set **Implement** — from set selection to its exit. It holds at most one live **Drain** at a time and may comprise several: reaching a gate menu parks (finishes) the held Drain so the menu runs lock-free, and resuming AFK work begins a fresh one (quota waits likewise). The Implement run, not the Drain, owns the gate menus, the pre-approval verify phase, and the shared prompt reader.
  avoid: Drain (for the whole invocation), session, segment, drain session
