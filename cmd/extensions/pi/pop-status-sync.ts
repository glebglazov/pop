/**
 * pop-status-sync
 *
 * pi extension that keeps the surrounding pop tmux pane's status in sync with
 * the agent's lifecycle:
 *   - working         → pi is busy (user submitted input, or a tool is running)
 *   - needs_attention → pi finished a turn, awaiting the user
 *
 * `idle` is also sent on `session_start`, but only as housekeeping: pop
 * ignores `set-status idle` for untracked panes, so it cannot pollute the
 * dashboard. For already-tracked panes it clears any stale "working" status
 * left over from a crashed previous run.
 *
 * Installed by `pop integrate pi` to ~/.pi/agent/extensions/pop-status-sync.ts.
 */

import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";

export default function (pi: ExtensionAPI) {
	const setStatus = (status: "working" | "needs_attention" | "idle") => {
		// Fire-and-forget; swallow errors so a missing `pop` binary never
		// breaks the agent.
		pi.exec("pop", ["pane", "set-status", status]).catch(() => {});
	};

	// UserPromptSubmit → working
	pi.on("input", async (event) => {
		if (event.source !== "extension") {
			setStatus("working");
		}
		return { action: "continue" };
	});

	// PreToolUse → working
	pi.on("tool_call", async () => {
		setStatus("working");
		return undefined;
	});

	// Stop → needs_attention (agent finished a turn — flag the user)
	pi.on("agent_end", async () => {
		setStatus("needs_attention");
	});

	// Housekeeping: clear any stale "working" status left over from a
	// previous run on session start, so a freshly resumed pane isn't stuck
	// "working". Pop treats `idle` as a no-op for untracked panes, so this
	// cannot register a brand-new pane and skew the dashboard sort.
	pi.on("session_start", async () => {
		setStatus("idle");
	});
}
