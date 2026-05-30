# Pop

A CLI for navigating between development directories and their tmux sessions. Pop tracks which panes need attention and provides fuzzy-search pickers for switching context quickly.

## Language

**Project**:
A directory on disk that pop knows about — either listed explicitly in config or matched by a glob pattern. Selecting a project is the primary action in `pop select`; attaching to or creating a tmux session follows from that choice.
_Avoid_: Folder, workspace, session (when you mean the directory itself)

**Session**:
The tmux session pop creates or attaches to when you select a project. Its name is derived from the project path.
_Avoid_: Project (when you mean the tmux session, not the directory)

**Standalone session**:
A tmux session that appears in the picker but has no corresponding project in config. Pop discovers these from tmux directly.
_Avoid_: Orphan session, external session

**Worktree**:
A linked checkout of a git repository at a separate path. Each worktree is also a project — it appears in the picker and gets its own session. Bare repos expand into their worktrees rather than appearing as a single entry.
_Avoid_: Checkout, clone (when you mean a worktree specifically)

**Pane**:
A tmux pane that pop tracks for attention status, visit time, and optional notes. Untracked tmux panes are outside pop's domain.
_Avoid_: Terminal, window (tmux window ≠ pane)

### Pane status

**Working**:
The pane's agent or process is actively running.
_Avoid_: Busy, active

**Unread**:
The pane has output or a state change that needs your attention.
_Avoid_: Needs attention

**Clear**:
No attention is required — either you've acknowledged the pane or nothing new is pending.
_Avoid_: Idle, read

**Dashboard**:
The presentation of the monitored set of panes — a browsable view of registered panes, their status, and visit times. `pop dashboard` opens this view.
_Avoid_: Monitor (when you mean the tracking mechanism, not the view)

**Monitor**:
The subsystem that maintains the monitored set of registered panes — tracking status, visit times, and notes via daemon, state, and tmux hooks. Agent integrations report into the monitor; the dashboard reads from it. Exposed via `pop pane monitor-start`, `monitor-stop`, and `monitor-status`.
_Avoid_: Dashboard (when you mean the view, not the mechanism)

**Agentic pane**:
A pane running an AI coding agent or its runtime (e.g. Claude, OpenCode, Pi). These panes self-register with the monitor via integrations and appear on the dashboard. Plain shell panes are not agentic and are not registered unless you explicitly track them.
_Avoid_: Agent pane, bot pane

### Pickers

**Project picker**:
The fuzzy-search picker in `pop select` for choosing a project, worktree, or standalone session.
_Avoid_: Session picker, normal mode

**Worktree picker**:
The fuzzy-search picker in `pop worktree` for choosing, creating, or deleting git worktrees in the current repository.
_Avoid_: Repo picker

**History**:
The persisted record of projects you've accessed, with timestamps. Used for recency sorting in the project picker and visit ordering on the dashboard.
_Avoid_: Recents, access log

**Unread view**:
The sub-view in the project picker (entered via `→` on sessions with unread panes) for quickly jumping to unread panes without opening the dashboard.
_Avoid_: Attention view, triage view

**Visit**:
Recording interaction with a pane — focus, switch, or an explicit `pop pane visit`. Updates the pane's last-active time and drives recency ordering on the dashboard. Not the same as clearing unread; a Clear pane still accumulates visits.
_Avoid_: Acknowledgment, last seen

**Following**:
A dashboard-scoped way to mark a pane for ongoing interest. Followed panes persist across sessions; following mode filters the dashboard to show only followed panes.
_Avoid_: Pin, watch

**Integration**:
An agent setup that connects a coding tool (Claude, Pi, OpenCode) to the monitor, so its pane self-reports status. Installed via `pop integrate <agent>`.
_Avoid_: Hook, plugin (when you mean the whole setup, not a single file)

## Flagged ambiguities

**Clear vs idle/read** — Domain term is **Clear**. The CLI accepts `idle` and `read` as deprecated aliases; persisted state uses `"clear"`.

**Dashboard vs monitor** — **Monitor** maintains the monitored set; **Dashboard** presents it. Code uses both names loosely (`monitor` package, `dashboard` command); use domain terms when writing docs or discussing behavior.

**Project picker naming** — Code calls this "normal" mode (`viewNormal`). Domain term is **Project picker**.

**Unread vs needs_attention** — Domain term is **Unread**. `needs_attention` is a deprecated CLI alias.

**Visit vs clear** — **Visit** records interaction (updates last-active time). **Clear** is a status meaning no attention is required. Switching to an unread pane on the dashboard typically both visits and clears it.

## Example dialogue

> **Dev:** I picked a worktree in the project picker — did I select a project or a worktree?
>
> **Expert:** Both, in a sense. A **worktree** is also a **project** — it's a directory pop knows about. The **worktree picker** is different: that's for git operations inside the repo you're already in.
>
> **Dev:** What happens after I select it?
>
> **Expert:** Pop attaches to or creates a **session** for that project. The path goes into **history** for recency sorting next time.
>
> **Dev:** My Claude pane finished — the integration marked it Unread. Do I visit it or clear it?
>
> **Expert:** Different things. **Unread** is the status — something needs your attention. A **visit** records that you interacted with the pane. When you switch to it on the **dashboard**, that typically clears it to **Clear**.
>
> **Dev:** Is the dashboard the same as the monitor?
>
> **Expert:** No. The **monitor** tracks pane status in the background — that's what the **integration** talks to. The **dashboard** is just the view over that monitored set. An **agentic pane** self-registers with the monitor; you browse it on the dashboard.
>
> **Dev:** I saw a `!` in the project picker and pressed `→`. Is that the dashboard?
>
> **Expert:** That's the **unread view** — a quick triage shortcut scoped to one session's unread panes. The full **dashboard** shows all registered panes across sessions.
>
> **Dev:** I want to keep an eye on one agent even when it's Clear.
>
> **Expert:** **Following** on the dashboard. Toggle follow on the pane, then use following mode to filter to just followed panes.

