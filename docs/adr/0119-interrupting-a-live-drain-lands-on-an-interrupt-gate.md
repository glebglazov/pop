---
status: accepted
---

# Interrupting a live drain lands on an interrupt gate, not an exit

## Context

Today SIGINT during active AFK execution is caught inside `runAgentAttempt` (`tasks/attempts.go:396-435`): it tears the agent process group down (SIGTERM→grace→SIGKILL), returns `ExitInterrupted` (130), and the drain finalizes with the `interrupted` terminal and the process exits. The interrupt deliberately *bypasses* the interactive gates — `run_selected_task.go:106-108` notes an interrupt never reaches the Failed gate. So the only thing a human can do by pressing Ctrl-C is quit; there is no way to pause, look, and hand back.

## Decision

Add a fourth interactive gate — the **Interrupt gate prompt** — as a sibling of the **HITL gate prompt**, **Failed gate prompt**, and **Verify-fail gate prompt**, reusing the same `gateEnv` machinery and lock discipline. On SIGINT during a live drain on a TTY:

1. Tear down the running agent attempt as today (graceful SIGTERM→SIGKILL), persisting the **Interrupted task**'s **Captured run**.
2. Park the **Runtime execution lock** and register a checkout gate hold (ADR-0067/0100), so the menu runs lock-free.
3. Present: **1 Continue draining** / **2 Get agent assistance** / **3 open Runtime shell** / **0 Exit**.
   - **Continue** re-acquires the lock and re-runs the interrupted task, carrying its interrupted-attempt digest forward ([ADR-0091](0091-resume-carries-in-flight-attempt-context-forward.md)), then keeps draining the rest of the set. Continue produces no drain terminal — it is a park-and-resume, like reaching any gate.
   - **Get agent assistance** launches an attended agent session (as today's HITL/Failed assistance, `runAttendedAssistanceCommand`) loaded with the interrupted task + set context; on exit it returns to this menu. It advises/edits by hand and does not itself mutate task state or resume the drain.
   - **Runtime shell** is a side-trip that returns to this menu.
   - **Exit** (`0`) finalizes the drain with the `interrupted` terminal.

The gate applies to **any** interactive-TTY drain — foreground `pop tasks implement` and **Queue**-spawned panes alike, since a human Ctrl-C on either pane means "I am taking over." `--yes` and non-interactive input keep today's teardown-and-exit with no menu.

Scope: this ADR governs interrupting **active AFK execution**. Interrupting a wait state (quota-recovery wait, retry backoff) keeps its current deregister-and-exit path; only the auto-drain revocation of [ADR-0120](0120-manual-interrupt-revokes-auto-drain.md) is shared across both.

## Considered options

- **Keep exit-only, tell the user to re-launch.** Rejected: loses the in-flight attempt narrative and forces a full re-drain to get back to where you were; the digest-carrying resume already exists (ADR-0089) and is wasted.
- **A bespoke pause protocol distinct from the gates.** Rejected: the three existing gates already model park-lock → lock-free menu → re-acquire; a fourth gate is a small, consistent addition, not a new mechanism.

## Consequences

- A running drain can no longer be quit with a single Ctrl-C on a TTY — the first Ctrl-C now opens the menu, and Exit (`0`) is the quit. A second Ctrl-C at the menu is the force-quit escape hatch.
- The interrupt terminal is now recorded only on the Exit branch; see [ADR-0120](0120-manual-interrupt-revokes-auto-drain.md) for its reclassification and the auto-drain revocation that pairs with this gate.
