# The monitored set is the set of panes the dashboard may kill

`ctrl+x` on the **Dashboard** destroys the cursored pane: `kill-pane`, then the
pane's monitor entry is deleted in the same breath. It pairs with `x`
(**Unmonitor**), which forgets a pane and leaves it running, and it borrows the
force-destroy meaning `ctrl+x` already carries in the project picker and the
Config dashboard.

The question this ADR exists to answer is what pop may kill. The ask was "kill
the panes that have agents", and pop **cannot tell which panes those are**.
`PaneEntry.Label` is the obvious candidate and is empty for claude, codex, kimi,
pi and opencode — only cursor's hooks pass `--label`. `pane_current_command`
reads `node` for Node-based agents, which is why `--label` exists at all. Every
other signal (`@pop_*` tags, window name, bare-shell-ness) answers a different
question.

It does not matter, because **the monitored set already is the agentic set**, by
construction rather than by classification. Only two functions can create a
monitor entry, `ReportStatus` and `SetFollowing`. `pop pane create` never
registers, so a dev server or watcher is invisible here; the tmux global hooks
pass `--no-register`, so they cannot register anything either. A row exists
because an agent's hook reported a status — or because a human explicitly ran
`pop pane follow`, and killing what you asked pop to follow is defensible. So
the row set is the filter, and no agent-detection concept enters the codebase.

## Considered options

**Store provenance on the entry.** `pop pane set-status` already accepts a
`--source` and discards it; recording "registered by an agent hook" would make
the classification a stored fact rather than an inference. A reasonable feature,
but it changes monitor state to gate a keybinding — its own decision, not this
one.

**Infer agent-ness behaviourally** — gate on "this pane has ever reported
`working`/`unread`", since only agent hooks emit those. Rejected: inference
dressed as a fact, and wrong for a pane whose agent has not yet reported.

**Refuse on drain panes.** See below; rejected in favour of the prompt.

## Consequences

**A killed drain pane can orphan a live agent.** A `pop-work` pane runs
`pop tasks implement`, which starts the headless agent in its own process group
(`Setpgid`, deliberately, so pop can signal it as a unit). tmux's SIGHUP reaches
pop, not that group, and only pop's own signal paths call
`terminateProcessGroup`. So the agent can keep writing into the checkout while
pop reconciles the drain as crashed and frees that tree for a second drain. It
usually self-limits when the agent's stdout pipe dies. Nothing else breaks: no
stuck RUNNING, no leaked checkout claim, no wedged occupancy — liveness is
pid-based, reconciled opportunistically and filtered at read time. What is lost
is the interrupt gate ADR-0163 designed for this, so **Ctrl-C in the pane
remains the right way to interrupt a drain**.

Whether drain panes even appear here is unverified: pop's Go code never
registers them, pi and opencode are launched with their status-sync mechanism
explicitly disabled, and for claude/codex/cursor it depends on whether the CLI
runs hooks in print/exec mode — which pop neither documents nor depends on.

We took the risk uniformly rather than special-casing it. The `y/N` prompt is on
by default and is the mitigation; someone who sets
`[monitor.dashboard] kill_pane_prompt_enabled = false` has accepted this class of
risk knowingly. Teaching a monitor-level view about Work vocabulary in order to
refuse a legitimate action was the worse trade.

**Killing a Map pane needs no special care** — it is the sanctioned interface.
Per ADR-0193 a ticket claim is owned by the pane and lasts exactly as long as
the agent in it, so a dead owner's claim is never returned and the ticket is
back on the frontier at the next read. That ADR explicitly declined a release
verb on the grounds that killing the pane already is one.

## The narrower rules

- **Never the pane you are in.** The refusal is on `PaneID == currentPaneID`,
  not on virtualness, and the current pane id is now always resolved and passed
  in — a guard on whether you can destroy your own terminal must not depend on
  an unrelated `cursor_position` setting.
- **`--pick` stays read-only.** Picker mode promises it does not mutate state,
  and scripts may rely on that; the key is inert there.
- **No retile.** `pop pane kill` re-evens the Spawn window, but dashboard rows
  live in several windows (`pop-spawn`, `pop-work`, `pop-map-*`, hand-split
  ones) and re-evening the wrong one is worse than letting tmux hand the space
  to a neighbour.
- **Hard kill, no graceful stop.** Pop has no graceful-shutdown convention in
  code; a wait-then-kill ladder would add a timeout state machine to a TUI for a
  benefit agent CLIs, which persist their transcripts continuously, do not need.
- **The row goes immediately**, cursor clamped to the same index, and the
  dashboard quits when the kill empties the list — matching `x`. Deleting the
  monitor entry in-band matters because pruning is the only other cleanup: it
  runs on a five-second daemon poll, there is no tmux `pane-died` hook, and
  nothing prunes at all when the daemon is not running.
- **Every outcome is a Flash message** (ADR-0204) — the kill, and more
  importantly the failure, which otherwise has nowhere to appear.
