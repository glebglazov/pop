# HITL gates offer attended agent assistance

When `pop tasks drain` reaches a **Human-blocked Task set**, execution remains AFK-only but an interactive terminal may show a **HITL gate prompt** instead of only stop-and-advice text. The prompt defaults to **Get agent assistance**, which starts a **HITL assistance session** with a **HITL assistance prompt**; completion and deferral remain explicit human choices in the gate prompt and then let Drain continue. Choosing complete or defer in that menu is the confirmation; Pop does not ask for an additional yes/no prompt.

## Why

HITL work is not executable by the unattended workload executor, but a human at the gate often wants an agent to help inspect the codebase, verify the result, and decide which manual override is correct. A numbered gate prompt preserves that human boundary better than a yes/no launch prompt: completing, deferring, getting assistance, and exiting are different outcomes.

Agent assistance is owned by the selected **Agent adapter**, not by ad hoc menu logic. Each adapter launches its own preset's interactive binary, while custom headless `--agent-cmd` remains limited to unattended issue attempts. This keeps support decisions with the same per-agent boundary that already owns headless invocation and output handling.

## Considered Options

- **Keep stop-and-advice only.** Rejected: it leaves the human to manually reconstruct useful context in a separate agent session.
- **Auto-launch assistance whenever a TTY is present.** Rejected: reaching a HITL gate does not mean the human wants a new attended process.
- **Use a yes/no prompt defaulting to launch.** Rejected: the gate has more than two legitimate outcomes.
- **Disable assistance for agents without native interactive support.** Rejected: the selected Agent adapter should be able to provide a fallback command so the menu does not expose dead options unnecessarily.
- **Fall back to a different agent's binary when the selected agent lacks native interactive support.** Originally adopted (cursor/pi → claude), reversed 2026-06-10 — see Amendment.

## Amendment (2026-06-10): no cross-agent fallback

The original decision let an adapter without a native interactive command fall back to launching a *different* agent's binary; cursor and pi fell back to `claude`. That premise turned out false: every supported preset (`claude`, `opencode`, `cursor-agent`, `codex`, `pi`) has an interactive REPL that accepts a positional prompt, so attended assistance is always `<binary> <prompt>`. The fallback never bridged a real capability gap — it silently overrode the human's agent choice and dropped that agent's augmentation args (ADR-0017), since those were written for the original, not the fallback.

The `AgentAssistanceFallback` mode is removed. Each adapter now launches its own preset's interactive binary, and the preset's extra args ride into the session. Assistance modes collapse to `native | unavailable`; `unavailable` remains the honest answer only for things with no usable interactive command at all, such as custom headless `--agent-cmd`, where the gate hides the option rather than substituting another agent.

## Consequences

Non-interactive runs and `--yes` preserve the stop-and-advice behavior so automation never hangs in an attended agent. When the selected Task set is already Human-blocked at a HITL gate, interactive Drain may go directly to the HITL gate prompt, but it must still collect explicit AFK-execution consent before running any AFK task unblocked by the gate. After a HITL assistance session exits, Pop refreshes the Task set: if the blocking task was completed or skipped it continues, if it still blocks it prompts again, and if edits changed the set status normal task status handling applies.
