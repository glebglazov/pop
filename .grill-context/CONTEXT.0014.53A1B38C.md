---
fragment: 53A1B38C
generation: 0014
branch: master
---

+ Task transition
  The governed move of one task between the four statuses (open, done, failed, skipped) through a single chokepoint. Legality is keyed by (from, to, actor): the **Task executor** may drive only open→done and open→failed; the human — via **Complete task**, **Skip**, and **Open task** — drives open→done (clearing a HITL task is this edge), failed→open, failed→done, open→skipped, skipped→open, skipped→done, and done→open. Every transition appends a **Progress record**, maintains the recorded attempt count (set on entering failed, cleared otherwise), lands as one atomic manifest write per batch, and applies **Verification invalidation** per its trigger rule. No other writer may change a task's status.
  under: Tasks

~ Verification invalidation
  Clearing every cached **Verify verdict** row for a `(repo, set)` in the Drain store — ending the current **verification episode** so the next completion requires fresh Agent verification. Triggered whenever a **Task transition** moves an AFK task into open or into done (a reopen restarts the episode; a manually completed AFK body was never judged), and on **Remediation task** spawn. HITL-task transitions never invalidate — the **Verifier** judges only done-AFK work. Implemented as `DELETE` of all `verify_verdicts` rows for that key (not a soft epoch). The table is a cache, not the audit trail; **Captured verify run**s remain on disk.
  was: Clearing every cached **Verify verdict** row for a `(repo, set)` in the Drain store when a **Task set** leaves the terminal zone — ending the current **verification episode** so the next completion requires fresh Agent verification. Implemented as `DELETE` of all `verify_verdicts` rows for that key (not a soft epoch). Triggered on human **Open task** / batch reopen and on **Remediation task** spawn. The table is a cache, not the audit trail; **Captured verify run**s remain on disk.

~ Verification episode
  One contiguous stretch during which a **Task set**'s done-AFK work composition is unchanged: AFK work complete, Agent verification, then DONE or AWAITING-APPROVAL. A PASS within the episode immunizes against post-PASS commits. The episode ends when the done-AFK composition changes — an AFK task re-opens or newly becomes done (including **Remediation task** spawn) — not on mere terminal-zone exit: HITL-only movement (skip, complete, or reopen of a HITL task) never ends it, even when the set detours out of the terminal zone (e.g. skip-HITL→DEFERRED and back). The next terminal arrival after an episode end requires fresh verification.
  was: One contiguous pass through the terminal zone for a **Task set**: AFK work complete, then Agent verification, then DONE or AWAITING-APPROVAL. A PASS within the episode immunizes against post-PASS commits. The episode ends when the set leaves the terminal zone (reopen, **Remediation task** spawn, or any return to open AFK work); the next terminal arrival starts a new episode requiring fresh verification.
