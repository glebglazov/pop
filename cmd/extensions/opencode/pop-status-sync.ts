/**
 * pop-status-sync
 *
 * opencode plugin that keeps the surrounding pop tmux pane's status in sync
 * with the agent's lifecycle:
 *   - working → opencode is busy (a tool is running / agent is mid-turn)
 *   - unread  → opencode finished a turn, awaiting the user
 *
 * `idle` is also sent on plugin load and on session.created/deleted, but only
 * as housekeeping: `pop pane set-status idle` is a no-op for untracked panes,
 * so it cannot pollute the dashboard. For already-tracked panes it clears any
 * stale "working" status left over from a crashed previous run.
 *
 * Installed by `pop integrate opencode` to ~/.config/opencode/plugins/pop-status-sync.ts.
 */

export const PopStatusSync = async ({ $ }) => {
	const paneID = process.env.TMUX_PANE;
	if (!paneID) return {};

	const setStatus = (status: "idle" | "working" | "unread") => {
		// Fire-and-forget; swallow errors so a missing `pop` binary never
		// breaks the agent.
		$`pop pane set-status ${paneID} ${status}`.catch(() => {});
	};

	// Clear any stale "working" status left over from a previous run of the
	// plugin in this pane. Pop ignores this for untracked panes, so it cannot
	// register a brand-new pane and skew the dashboard sort.
	setStatus("idle");

	// Dedupe redundant transitions: `tool.execute.before` (named hook) and
	// `session.status` (event handler) can both fire for the same busy period,
	// but the named hook arrives first so we get a snappier transition.
	let working = false;
	const markWorking = () => {
		if (!working) {
			working = true;
			setStatus("working");
		}
	};
	const markUnread = () => {
		working = false;
		setStatus("unread");
	};
	const markIdle = () => {
		working = false;
		setStatus("idle");
	};

	return {
		event: async ({ event }) => {
			switch (event.type) {
			case "session.created":
			case "session.deleted":
				// Housekeeping only — see top-of-file note about `idle`.
				markIdle();
				break;
			case "session.idle":
				// Agent finished a turn — flag the user.
				markUnread();
				break;
			case "session.status":
				if (event.properties.status.type === "busy") {
					markWorking();
				} else if (event.properties.status.type === "idle") {
					markUnread();
				}
				break;
			}
		},
		"tool.execute.before": () => {
			markWorking();
		},
	};
};
