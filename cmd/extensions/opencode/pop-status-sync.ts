/**
 * pop-status-sync
 *
 * opencode plugin that keeps the surrounding pop tmux pane's status in sync
 * with the agent's lifecycle. Two states:
 *   - working          → opencode is busy (user submitted input, or a tool is running)
 *   - needs_attention  → opencode is idle, awaiting the user
 *
 * Installed by `pop integrate opencode` to ~/.config/opencode/plugins/pop-status-sync.ts.
 */

export const PopStatusSync = async ({ $ }) => {
	const setStatus = (status: "working" | "needs_attention") => {
		// Fire-and-forget; swallow errors so a missing `pop` binary never
		// breaks the agent.
		$`pop pane set-status ${status}`.catch(() => {});
	};

	return {
		event: async ({ event }) => {
			switch (event.type) {
			case "session.created":
			case "session.idle":
				setStatus("needs_attention");
				break;
			case "tool.execute.before":
			case "tui.prompt.append":
				setStatus("working");
				break;
			}
		},
	};
};
