/**
 * pop-status-sync
 *
 * pi extension that keeps the surrounding pop tmux pane's status in sync with
 * the agent's lifecycle:
 *   - working → pi is busy (user submitted input, or a tool is running)
 *   - unread  → pi finished a turn, awaiting the user
 *
 * `clear` is sent on `session_start` to clear any stale "working" status
 * left over from a crashed previous run.
 *
 * Installed by `pop integrate pi` to ~/.pi/agent/extensions/pop-status-sync.ts.
 */

import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";

export default function (pi: ExtensionAPI) {
	const paneID = process.env.TMUX_PANE;
	if (!paneID) return;

	const setStatus = (status: "working" | "unread" | "clear") => {
		// Fire-and-forget; swallow errors so a missing `pop` binary never
		// breaks the agent.
		pi.exec("pop", ["pane", "set-status", paneID, status]).catch(() => {});
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

	// Stop → unread (agent finished a turn — flag the user)
	pi.on("agent_end", async () => {
		setStatus("unread");
	});

	// Housekeeping: clear any stale "working" status left over from a
	// previous run on session start, so a freshly resumed pane isn't stuck
	// "working".
	pi.on("session_start", async () => {
		setStatus("clear");
	});
}
