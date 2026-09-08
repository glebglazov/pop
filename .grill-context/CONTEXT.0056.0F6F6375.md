---
fragment: 0F6F6375
generation: 0056
branch: master
---

+ Drain turn
  One pass of a **Drain**'s loop: the Drain re-reads its inputs — the **Task set**'s
  manifest and pop's configuration — then either selects the next AFK task or, at an
  **AFK standstill**, reaches the phase gates (**Refine**, **Agent verification**, the
  terminal status). A configuration change made while a Drain runs applies at its next
  turn, never sooner and never later; a configuration that fails to load leaves the
  previous turn's configuration in force, and the Drain says so once.
  avoid: iteration, tick, loop pass, phase boundary
  under: Task execution

~ Drain
  One supervised execution of draining a **Task set**, tracked through an explicit
  lifecycle from start to a terminal disposition (its **Drain outcome**). A Task set
  may be drained many times — after a reset, a crash, or a quota pause — and each is
  a distinct Drain; a set's Drain history is the ordered record of them. The Drain,
  not the Task set, carries execution lifecycle state; the set's manifest-derived
  **Task set status** (what work remains) is a separate, derived concern. It runs as
  a sequence of **Drain turn**s and reads configuration afresh at each, except for
  what it settles at start: which agents implement, how they report, how a dirty
  checkout is treated, and the commit rules — those hold for the whole Drain.
  avoid: Run, attempt, drain record
  was: One supervised execution of draining a **Task set**, tracked through an explicit lifecycle from start to a terminal disposition (its **Drain outcome**). A Task set may be drained many times — after a reset, a crash, or a quota pause — and each is a distinct Drain; a set's Drain history is the ordered record of them. The Drain, not the Task set, carries execution lifecycle state; the set's manifest-derived **Task set status** (what work remains) is a separate, derived concern.
