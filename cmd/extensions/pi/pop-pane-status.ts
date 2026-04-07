/**
 * pop-pane-status
 *
 * pi extension that keeps the surrounding pop tmux pane's status in sync with
 * the agent's lifecycle. Two states:
 *   - working          → pi is busy (user submitted input, or a tool is running)
 *   - needs_attention  → pi is idle, awaiting the user
 *
 * Installed by `pop integrate pi` to ~/.pi/agent/extensions/pop-pane-status.ts.
 */

import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";

export default function (pi: ExtensionAPI) {
	const setStatus = (status: "working" | "needs_attention") => {
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

	// Stop → needs_attention
	pi.on("agent_end", async () => {
		setStatus("needs_attention");
	});

	// Mark idle on session start so a freshly resumed pane isn't stuck "working"
	pi.on("session_start", async () => {
		setStatus("needs_attention");
	});
}
