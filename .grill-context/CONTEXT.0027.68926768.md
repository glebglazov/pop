---
fragment: 68926768
generation: 0027
branch: master (recovery-wait-visibility grill)
---

+ Recovery block reason
  Why an eligible Recovery waiter (cooldown elapsed) was not granted a Recovery turn on its checkout, computed inside the same acquisition transaction: a kind — gate hold, live drain, turn held, or behind another waiter — plus the blocking set's ID. Surfaced in the Agent quota recovery wait status line so a post-cooldown wait names its blocker instead of claiming the quota is still the cause.
  avoid: block cause, wait reason, denial reason
  under: Quota Recovery

~ Agent quota recovery wait
  The poll loop an implement process enters after **Agent quota pause**: park the drain (`quota_paused` terminal, **Runtime execution lock** released per ADR-0067), register a **Recovery waiter**, poll the **Agent quota recovery coordinator** until a **Recovery turn** is granted, then `BeginDrain` and resume at the **Quota recovery resume point**. Applies to foreground and unattended drains alike — the pane shows the wait rather than exiting for human re-run. Pre-reset it prints a local-time countdown on the regular poll cadence; post-reset it prints the **Recovery block reason** on change plus a periodic heartbeat, never on the fast external-deregistration check. SIGINT deregisters the waiter and exits as an **Interrupted task** drain (`ExitInterrupted`); the open task and partial checkout changes are preserved.
  was: The poll loop an implement process enters after **Agent quota pause**: park the drain (`quota_paused` terminal, **Runtime execution lock** released per ADR-0067), register a **Recovery waiter**, poll the **Agent quota recovery coordinator** until a **Recovery turn** is granted, then `BeginDrain` and resume at the **Quota recovery resume point**. Applies to foreground and unattended drains alike — the pane shows the wait rather than exiting for human re-run. SIGINT deregisters the waiter and exits as an **Interrupted task** drain (`ExitInterrupted`); the open task and partial checkout changes are preserved.

~ Checkout gate hold
  A lightweight registration with the **Agent quota recovery coordinator** when implement parks at a **Failed gate prompt** or **HITL gate prompt** (runtime lock released per ADR-0067). It names the task set and **Runtime path** and blocks **Recovery turn** acquisition on that checkout until the gate session ends — resume, exit, or interrupt — so a quota waiter on another set cannot resume agent work on the same dirty tree while a human sits at a gate. The hold-registering gate menus display the count of **Recovery waiter**s blocked on the checkout, so the human at the gate sees who they are holding up.
  was: A lightweight registration with the **Agent quota recovery coordinator** when implement parks at a **Failed gate prompt** or **HITL gate prompt** (runtime lock released per ADR-0067). It names the task set and **Runtime path** and blocks **Recovery turn** acquisition on that checkout until the gate session ends — resume, exit, or interrupt — so a quota waiter on another set cannot resume agent work on the same dirty tree while a human sits at a gate.
