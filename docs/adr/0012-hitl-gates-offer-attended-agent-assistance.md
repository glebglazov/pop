# HITL gates offer attended agent assistance

When `pop tasks drain` reaches a **Human-blocked Task set**, execution remains AFK-only but an interactive terminal may show a **HITL gate prompt** instead of only stop-and-advice text. The prompt defaults to **Get agent assistance**, which starts a **HITL assistance session** with a **HITL assistance prompt**; completion and deferral remain explicit human choices in the gate prompt and then let Drain continue. Choosing complete or defer in that menu is the confirmation; Pop does not ask for an additional yes/no prompt.

## Why

HITL work is not executable by the unattended workload executor, but a human at the gate often wants an agent to help inspect the codebase, verify the result, and decide which manual override is correct. A numbered gate prompt preserves that human boundary better than a yes/no launch prompt: completing, deferring, getting assistance, and exiting are different outcomes.

Agent assistance is owned by the selected **Agent adapter**, not by ad hoc menu logic. The adapter may use a native attended command or a configured fallback for that agent, while custom headless `--agent-cmd` remains limited to unattended issue attempts. This keeps support decisions with the same per-agent boundary that already owns headless invocation and output handling.

## Considered Options

- **Keep stop-and-advice only.** Rejected: it leaves the human to manually reconstruct useful context in a separate agent session.
- **Auto-launch assistance whenever a TTY is present.** Rejected: reaching a HITL gate does not mean the human wants a new attended process.
- **Use a yes/no prompt defaulting to launch.** Rejected: the gate has more than two legitimate outcomes.
- **Disable assistance for agents without native interactive support.** Rejected: the selected Agent adapter should be able to provide a fallback command so the menu does not expose dead options unnecessarily.

## Consequences

Non-interactive runs and `--yes` preserve the stop-and-advice behavior so automation never hangs in an attended agent. When the selected Task set is already Human-blocked at a HITL gate, interactive Drain may go directly to the HITL gate prompt, but it must still collect explicit AFK-execution consent before running any AFK task unblocked by the gate. After a HITL assistance session exits, Pop refreshes the Task set: if the blocking task was completed or skipped it continues, if it still blocks it prompts again, and if edits changed the set status normal task status handling applies.
