/**
 * pop-status-sync
 *
 * pi extension that keeps the surrounding pop tmux pane's status in sync with
 * the agent's lifecycle:
 *   - working → pi is busy (user submitted input, or a tool is running)
 *   - unread  → pi finished a turn, awaiting the user
 *
 * `clear` is sent on `session_start` to clear any stale "working" status
 * left over from a crashed previous run, and to wipe the pane Topic so the
 * next prompt can re-derive it for the new session.
 *
 * It also derives a pane *topic* from each submitted prompt (`set-topic
 * --derive --label pi`), riding the same status wiring (ADR 0023).
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

	const clearTopic = () => {
		pi.exec("pop", ["pane", "set-topic", "--clear", paneID]).catch(() => {});
	};

	// Derive a pane topic from the submitted prompt. pi.exec runs a binary
	// directly with no stdin option, so we route through `sh -c` and feed the
	// normalized {prompt} JSON on stdin. The payload rides as a positional
	// argument ($1) — never interpolated into the script — so prompt text can
	// never break out into the shell. `--label pi` selects pop's pi payload
	// adapter (prompt-only, no transcript_path). Fire-and-forget.
	const setTopic = (text: string) => {
		if (!text) return;
		const payload = JSON.stringify({ prompt: text });
		pi.exec(
			"sh",
			[
				"-c",
				`printf '%s' "$1" | pop pane set-topic --derive --label pi ${paneID}`,
				"sh",
				payload,
			],
		).catch(() => {});
	};

	// UserPromptSubmit → working (status) + derive topic from the prompt text.
	pi.on("input", async (event) => {
		if (event.source !== "extension") {
			setStatus("working");
			setTopic(event.text);
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
	// "working". Wipe the Topic too so the next prompt re-derives for this
	// session.
	pi.on("session_start", async () => {
		setStatus("clear");
		clearTopic();
	});
}
