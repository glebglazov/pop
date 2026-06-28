---
status: accepted
---

# Queue drains share a per-project `pop-queue` window

ADR-0029/0031 made Pop own `git worktree add` for **Worktree sets** and bind one checkout per
set. The first implementation spawned each worktree-set drain into its **own tmux session**, named
after the worktree (`project.SessionNameWith(worktreePath)` → `repo/<safeSet>`). A daemon draining
several sets across several projects therefore manufactured a myriad of sessions the human never
chose to open, none of which map to a navigable Project.

A **Worktree set** is now treated as an *ephemeral execution context, not a navigable Project peer*:
Pop no longer auto-creates a session for it. The checkout is still a registered git worktree, so it
stays reachable on demand through the **Worktree picker**, which creates a session only when the
human selects it.

All queue-spawned drains — both in-place and Worktree set — now land in a single per-project
**Queue window** named `pop-queue` inside the **originating project Session**, as panes under a
balanced (`tiled`) layout. The user's working windows (window 0 and friends) are left untouched.
Concretely: `prepareWorktreeDrain` stops overwriting `scan.SessionName` with the worktree's session
(it keeps `ProjectPath` rewritten to the checkout so the pane's cwd is the runtime path), and
`spawnDrain` targets the named `pop-queue` window — creating the project session detached if absent
and the window if absent.

Drains keep their existing persistence mechanism (a `split-window` shell that `send-keys` runs the
command into), so a finished drain leaves a live shell, not a closed pane. To bound pane growth
under re-spawn churn, Pop keeps **one pane per set**: each drain pane is tagged with a pane-scoped
tmux user option `@pop_set=<setid>`; a re-spawn discovers the set's pane via `list-panes` and reuses
it instead of splitting a new one. Releasing a **Worktree binding** (integration or **Abandon
worktree**) does not kill the pane — the **captured stream** (ADR-0016) is the durable record, and
the scrollback is left for the human to close.

## Considered options

- **Keep a session per worktree set.** Rejected — manufactures unchosen sessions that look like
  Projects but aren't; the very proliferation this ADR removes.
- **A window per set instead of a shared `pop-queue` window.** Rejected — the human wanted panes in
  one named window, navigable as a group, rather than a tab per set.
- **Scope `pop-queue` to worktree sets only; leave in-place drains in window 0.** Rejected — in-place
  drains splitting into the user's shell window is the same intermixing, just in-session; unifying
  keeps one invariant ("queue activity lives in `pop-queue`").
- **Split a new pane on every spawn.** Rejected — unbounded pane growth under re-spawn (agent-quota
  pause, failure retry) makes the `tiled` layout unreadable.
- **Record the drain pane id (`%N`) in Queue daemon state.** Rejected — couples transient UI plumbing
  to durable state (ADR-0031 reserves state for bindings/backoffs/mergeability) and leaves stale
  handles when the human closes a pane. The pane-scoped `@pop_set` option makes tmux the source of
  truth and self-heals.
- **Kill the set's pane when its binding is released.** Rejected — the human prefers the scrollback
  left in place; the captured stream already preserves output durably.

## Consequences

- `prepareWorktreeDrain` no longer derives a worktree session name; `scan.SessionName` stays the
  originating project session for the whole decision.
- `spawnDrain`/`resolveDrainWindowTarget` target a window by name (`pop-queue`), creating it if
  absent, rather than the session's lowest-index window.
- Each drain pane carries `@pop_set`; spawn first looks for the set's existing pane and reuses it.
- A freshly queue-created project session has an idle window 0 plus `pop-queue` — visible when the
  human later opens the project.
- Requires tmux ≥ 3.0 for pane-scoped user options.
- The glossary's "each worktree gets its own session" applies to user Worktrees, not Worktree sets.
