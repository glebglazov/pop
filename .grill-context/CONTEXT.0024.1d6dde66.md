---
fragment: 1d6dde66
generation: 0024
branch: master
---

+ Interrupt gate prompt
  The interactive menu shown when SIGINT (Ctrl-C) interrupts a live **Drain** on a TTY, instead of the drain exiting 130. The fourth sibling of the **HITL gate prompt**, **Failed gate prompt**, and **Verify-fail gate prompt**: the signal first tears down the running agent attempt (graceful SIGTERM→SIGKILL, persisting the **Interrupted task**'s **Captured run**), parks the **Runtime execution lock** and registers a checkout gate hold, then presents 1 Continue draining / 2 Get agent assistance / 3 open **Runtime shell** / 0 Exit. Continue re-acquires the lock and re-runs the interrupted task carrying its attempt digest forward (ADR-0091), then keeps draining; assistance and shell are side-trips that return to this menu; `0` is exit. Shown on any interactive TTY drain — foreground or a **Queue**-spawned pane; `--yes` and non-interactive input keep today's teardown-and-exit with no menu.
  avoid: interrupt menu, SIGINT prompt, Ctrl-C gate

+ Interrupt auto-drain revocation
  Interrupting a live **Drain** clears the set's **Auto-drain** consent bit unconditionally at the moment of interrupt — the human is taking manual ownership, so `pop queue run` stops re-firing the set. The pre-interrupt value is snapshotted: choosing Continue at the **Interrupt gate prompt** revives it (announced to the user), so a peek-and-continue leaves consent unchanged, while Exit or a crash-at-gate leaves it cleared. Net: consent is truly discarded only when the human does not resume, yet it is cleared throughout the at-gate window so the daemon cannot grab the set before the human decides. Re-enabling after Exit is a fresh human mark. Distinct from **Auto-drain clearing**'s terminal (DONE/AWAITING-APPROVAL) trigger and unrelated to clearing on pick-up (rejected, see ADR-0120).
  avoid: interrupt auto-drain clear, clear-on-pickup, queue stop-on-interrupt

~ Auto-drain clearing
  The automatic flip of a set's **Auto-drain** bit from on to off. Two triggers: (1) at drain finalization to a terminal disposition — derived status DONE or AWAITING-APPROVAL, the states in which all AFK work is drained (ADR-0098); and (2) a manual interrupt of a live drain (**Interrupt auto-drain revocation**), which clears unconditionally at interrupt with Continue reviving the prior value. Both fire only from a live/finishing drain, never from a background reader; both are idempotent, announced, and a durable per-set trace. Because they discard consent rather than hide the marker, a later **Open task**, **Remediation task**, or **Verification invalidation** does not auto-re-fire the daemon — a human must re-mark **Auto-drain**.
  was: The automatic flip of a set's Auto-drain bit from on to off when a drain finalizes with the set's derived status DONE or AWAITING-APPROVAL — the two states in which all AFK work is drained and the daemon has nothing left to do. It fires only from a finishing drain … never from a background reader …

~ Drain outcome
  The `interrupted` outcome is now a deliberate human handoff, not an abnormal exit. Interrupting a live drain lands on the **Interrupt gate prompt** (park-and-resume, like reaching any gate); only Exit from that gate records the `interrupted` terminal, and Continue produces no terminal at all (the drain resumes to its own later stopping point). `interrupted` is reclassified as a **clean** exit: it no longer drives **Queue backoff** (only `crashed`/kill remain abnormal), because a manual interrupt now clears **Auto-drain** so there is no re-spawn to throttle. finished, quota-paused, verify_failed, and interrupted are clean; only crashed is abnormal.
  was: finished, quota-paused, and verify_failed are clean exits; interrupted and crashed are abnormal and drive crash backoff.

~ Queue backoff
  The daemon's response to an abnormal drain exit — now crash or kill only. A manual interrupt is no longer abnormal (it clears **Auto-drain** via **Interrupt auto-drain revocation**, so there is nothing left for the daemon to re-spawn and throttle). The daemon applies an escalating per-set delay and, after N consecutive abnormal exits, parks the set until a human clears it; a clean exit resets the counter. Distinguishing abnormal (crash/kill) from clean (finished/quota-paused/verify-failed/interrupted) reads the **Drain**'s terminal `state` directly (`store.drainStateAbnormal`).
  was: The daemon's response to an abnormal drain exit, such as crash, kill, or interrupt. … Distinguishing abnormal (crash/interrupt/kill) from clean (finished/quota-paused/verify-failed) exits reads the Drain's terminal state directly (store.drainStateAbnormal)…
