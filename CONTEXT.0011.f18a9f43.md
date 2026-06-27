---
fragment: f18a9f43
generation: 0011
branch: master
---

~ Runtime execution lock
  A machine-local lock held only while implement is actively executing in a checkout — it is the running **Drain** row, not a claim spanning the whole invocation. It is acquired around each contiguous run of AFK attempts and released at every wait for human input: the pre-run confirmation, the **HITL gate prompt**, and the **Failed gate prompt**. A drain that reaches a gate finishes (recording its park outcome) and the menu, plus any **HITL assistance session** or **Runtime shell** launched from it, runs lock-free; resuming after the human clears the gate re-acquires, refusing cleanly if another drain grabbed the checkout meanwhile. It prevents concurrent task execution in one checkout while allowing unrelated projects or isolated runtime worktrees to execute concurrently; a parked-at-gate pane is no longer treated as busy, so the **Queue daemon**'s anti-double-spawn relies on worktree isolation, not the lock. Lock metadata records the executor PID and running set identifier; a dead PID is reported and replaced as a stale lock.
  was: A machine-local lock held while implement executes for a canonical runtime path. Implement acquires it at the start of a drain or single-task run — before any mid-run menu — so the **Queue daemon** treats a pane waiting at **HITL gate prompt** or **Failed gate prompt** as busy and never double-spawns. It prevents concurrent task execution in one checkout while allowing unrelated projects or isolated runtime worktrees to execute concurrently. Non-execution tasks commands remain available. Lock metadata records the executor PID and running set identifier; a dead PID is reported and replaced as a stale lock.
