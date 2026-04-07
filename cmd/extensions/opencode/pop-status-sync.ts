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
		// User submitted input → working
		"tui.prompt.append": async () => {
			setStatus("working");
		},

		// Tool execution starting → working
		"tool.execute.before": async () => {
			setStatus("working");
		},

		// Agent became idle → needs_attention
		"session.idle": async () => {
			setStatus("needs_attention");
		},

		// Session started → reset to idle
		"session.created": async () => {
			setStatus("needs_attention");
		},
	};
};
