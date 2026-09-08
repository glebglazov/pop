# Herdr adapter investigation

Date: 2026-09-08. Scope: official Herdr documentation and source, plus the
current pop integration. This is a feasibility assessment, not a tested adapter.
Herdr was not installed or run. Source was read at
[`9e01168b140ce8e3821131345dc82bc2bf9994eb`](https://github.com/herdrdev/herdr/tree/9e01168b140ce8e3821131345dc82bc2bf9994eb).
The public site identifies its release as v0.9.0; master can contain newer work.
An implementation must check the installed binary's `herdr api schema --json`.
There was no existing research directory; this note establishes a location for
this investigation only.

## Result

A separate adapter is feasible for the main terminal workflows. Full tmux
behaviour is not established. Custom metadata **is supported**, but its limits
prevent a direct replacement of arbitrary tmux user options. Important open
areas are client focus, process exit handling, stable identity, and layout
changes that preserve running processes.

## Custom metadata

`pane.report_metadata` and `workspace.report_metadata` accept a `tokens` map
with caller-selected names. Pane and workspace read responses return these
tokens. They can also appear in Herdr's sidebar. A report patches existing
keys: strings set values, null clears values, and absent keys remain. There is
no tab metadata method in the inspected API method enum. This leaves pop's
window-level Workbench tag without a direct equivalent.
[API methods](https://github.com/herdrdev/herdr/blob/9e01168b140ce8e3821131345dc82bc2bf9994eb/src/api/schema.rs),
[pane schema](https://github.com/herdrdev/herdr/blob/9e01168b140ce8e3821131345dc82bc2bf9994eb/src/api/schema/panes.rs),
[workspace schema](https://github.com/herdrdev/herdr/blob/9e01168b140ce8e3821131345dc82bc2bf9994eb/src/api/schema/workspaces.rs).

The source enforces these limits:

- At most 16 keys in one update and 32 retained keys per resource.
- Key names: 1–32 ASCII letters, digits, underscores, or hyphens.
- Values: strings only, trimmed, control characters removed, **truncated to 80
  characters**. An empty result clears the key.
- Optional TTL: 1–86,400,000 milliseconds. Sequence numbers can reject old
  updates from the same source; at most 32 sequenced sources per resource.

Thus short pop tags can use token names such as `pop_topic`. Long paths,
unbounded identifiers, and arbitrary JSON need another store. In particular,
do not allow silent truncation of a value used for identity.
[Validation source](https://github.com/herdrdev/herdr/blob/9e01168b140ce8e3821131345dc82bc2bf9994eb/src/app/api_helpers.rs),
[token patch implementation](https://github.com/herdrdev/herdr/blob/9e01168b140ce8e3821131345dc82bc2bf9994eb/src/metadata_tokens.rs).

Tokens are not restored after a cold server restart. Omitted TTL means they
last until replacement, clearing, or resource closure in that server lifetime.
Metadata changes do not control semantic agent state. Workspace metadata
events reach API subscribers but do not invoke plugin event hooks.
[Socket API](https://herdr.dev/docs/socket-api/#agent-state-reporting).

## Runtime and UI fit

| pop requirement | Herdr evidence | Assessment |
| --- | --- | --- |
| Server socket | Local JSON socket; named sessions have separate socket paths | Maps to one selected Herdr server. A Herdr named session is the server boundary, not one pop Project. |
| Session / window / pane | Workspace / tab / pane; create, list, focus, rename, close | Natural hierarchy mapping, with ID/name translation. |
| Spawn commands | Process launch accepts argv, cwd, environment; managed processes receive `HERDR_*` context | Suitable in principle; shell command quoting must follow Herdr's contract. |
| Headless work | `herdr server` runs a headless server | Supported. |
| Detach and attach | Server owns the PTYs independently of clients | Supported; arbitrary processes do not survive a cold server restart. |
| Capture output and send input | Pane reads, text, keys, and combined input; read schema has ANSI format and `strip_ansi` | Supported primitives; exact capture range and key conversion need tests. |
| Process inspection | Shell PID, foreground process group, TTY and foreground process details | Supported when the platform supplies them. |
| Splits, zoom and geometry | Split ratios, resize, swap, move, layout tree, rectangle queries | Sufficient primitives for an adapter; weighted cell geometry requires verification. |
| Pop dashboard popup | Plugin terminal pane placement includes popup, overlay, split, tab, zoomed | Available through a declared plugin entrypoint; requires integration work. |

Sources: [API method schema](https://github.com/herdrdev/herdr/blob/9e01168b140ce8e3821131345dc82bc2bf9994eb/src/api/schema.rs),
[pane request and response schema](https://github.com/herdrdev/herdr/blob/9e01168b140ce8e3821131345dc82bc2bf9994eb/src/api/schema/panes.rs),
[headless CLI](https://github.com/herdrdev/herdr/blob/9e01168b140ce8e3821131345dc82bc2bf9994eb/src/cli/server.rs),
[session persistence](https://herdr.dev/docs/session-state/),
[plugin entrypoints](https://herdr.dev/docs/plugins/).

## Behaviour that prevents a direct swap

**Focus and Visits.** Public subscriptions include workspace, tab, and pane
focus. However, `PaneFocused` contains pane and workspace IDs only. It does not
identify the client or say whether the outer terminal gained focus. The server
does track outer terminal focus internally. These facts do not prove that pop
can reproduce both navigation-driven Clear and focus-driven Visit, or find all
Active panes across clients. No matching public client-list/outer-focus method
was found in the inspected API enum. Treat this as an API gap until a supported
path is demonstrated.
[Event schema](https://github.com/herdrdev/herdr/blob/9e01168b140ce8e3821131345dc82bc2bf9994eb/src/api/schema/events.rs),
[server focus handling](https://github.com/herdrdev/herdr/blob/9e01168b140ce8e3821131345dc82bc2bf9994eb/src/server/headless.rs).

**Exited panes and reruns.** `PaneExited` has pane/workspace IDs but no exit code.
`PaneInfo` has no dead-pane or exit-status field. Internal code can respawn a
shell after some agent exits, but that is not a public arbitrary-command
respawn API. No equivalent to tmux `remain-on-exit` plus `respawn-pane` was
found in the public method enum. A pop-owned command wrapper might preserve
results and accept reruns, but that is a proposed solution, not verified parity.
[Exit handling](https://github.com/herdrdev/herdr/blob/9e01168b140ce8e3821131345dc82bc2bf9994eb/src/app/api.rs),
[pane schema](https://github.com/herdrdev/herdr/blob/9e01168b140ce8e3821131345dc82bc2bf9994eb/src/api/schema/panes.rs).

**Identity.** A move between workspaces changes the public pane ID while the
terminal stays alive. `PaneInfo.terminal_id` exists and might supply a stable
identity for an adapter, but its lifetime and restart semantics were not
verified. Pop must handle moves and inherited pane environment values before
it uses public IDs as persistent claim keys.

**Layout updates.** `layout.apply` creates a new tab and closes the replaced tab.
It does not preserve live PTYs. It cannot directly implement a Workbench update
that must preserve running panes. The adapter would need incremental split,
move, swap, and ratio operations.
[Socket API](https://herdr.dev/docs/socket-api/#raw-methods).

**Popup context.** A plugin popup has no pane ID, is outside pane APIs, emits no
pane lifecycle events, and does not export `HERDR_PANE_ID`. Its context remains
the underlying tiled pane. Pop's popup handoff must pass the caller's target
explicitly. Plugin actions and pane entrypoints are declared in a manifest;
runtime arbitrary-argv plugin pane registration is not supported.
[Plugin API](https://herdr.dev/docs/socket-api/#plugin-apis).

**Attention state.** Herdr agent status is not pop's Monitor state. A working
agent, an unseen completion, a blocked permission request, Following, and a
Visit are different facts. Keep pop's current Working/Unread/Clear model and
its existing durable Monitor state. Use Herdr events as inputs after their
meaning is verified; do not replace pop state with a label conversion.

## Verification needed before a full adapter commitment

Use an isolated named Herdr server and its bundled schema. Check a Project
launch, Workbench creation and reapply, a retained failed command and rerun,
ANSI capture, Monitor transitions, dashboard popup return, two attached
clients, a moved pane, and a cold restart. Check metadata round trips at the
length boundary and document which fields pop stores itself. These checks
resolve the remaining behavioural questions; a method name alone does not.

## What must change in pop

Local source reviewed at `c86a76c`. The existing
[`Tmux` interface](../../internal/tmux/tmux.go) gives the change one starting
point. It is a pop-specific tmux contract, not a backend-independent contract:
it exposes global hooks, tmux presence, windows, tags, exact cell sizing, and
dead panes. Its consumers also retain backend assumptions.

- [`cmd/pane.go`](../../cmd/pane.go) reads `TMUX_PANE`, recognizes `%` as a pane
  ID prefix, and uses pane titles to find named panes. The
  [Pi](../../integrate/extensions/pi/pop-status-sync.ts) and
  [OpenCode](../../integrate/extensions/opencode/pop-status-sync.ts) extensions
  also read `TMUX_PANE`. A different constructor alone cannot change these.
- [`cmd/monitor.go`](../../cmd/monitor.go) clears status on pane/window/session
  navigation and records a Visit on `pane-focus-in`. The
  [Monitor store](../../monitor/store.go) can dismiss Unread when the pane is
  Active. [Monitor state](../../monitor/monitor.go) already owns status,
  Following, and visit time in `monitor.json`; those do not need Herdr tokens.
- [Topics](../../internal/tmux/topic.go),
  [Work tags](../../internal/tmux/taggedpane.go),
  [Work-session stamps](../../internal/tmux/worksession.go), and
  [Workbench identity](../../internal/tmux/workbench.go) use metadata at three
  scopes. Tab identity needs an adapter-owned mapping because Herdr exposes
  tokens only on panes and workspaces.
- [Map claims](../../wayfinder/claim.go) identify their owner by pane ID and
  PID. [Liveness](../../wayfinder/liveness.go) checks the foreground command
  and guards against reused IDs. Changing IDs during a move must not make a
  running claimant appear dead. Backend and server identity must be included
  if both adapters can be active at once.

Recommended design: keep tmux as the default and introduce one pop runtime
contract with explicit capability reporting. Keep backend syntax and context
resolution inside each adapter. Translate Session/window/Pane into Herdr
workspace/tab/pane within one selected Herdr server. Keep Project and Worktree
ownership in pop; opening a Herdr workspace does not require using Herdr's Git
worktree manager.

For Herdr, use short tokens for display and references. Keep full values and
missing tab-level identity in pop-owned storage, with lifecycle reconciliation.
Do not silently truncate identity or encode structured records into display
labels. Preserve native tmux Topic storage for the tmux adapter. Unsupported
features must produce a specific readiness finding rather than a successful
no-op. A small Herdr plugin can declare the popup entrypoints that run pop's
existing dashboards.

This proposal requires reopening
[ADR-0199](../adr/0199-pop-addresses-one-configurable-tmux-server-socket.md),
which rejected an abstraction until a second driver exists, and amending the
tmux-specific scope of
[ADR-0142](../adr/0142-tmux-operations-live-in-one-deep-internal-module.md) and
[ADR-0058](../adr/0058-topic-lives-in-the-pop-topic-tmux-user-option.md).
[ADR-0075](../adr/0075-workbench-apply-reconciles-by-pane-identity.md)'s rule
that reapply preserves running panes should remain a requirement.

Recommendation: proceed to a bounded compatibility prototype if Herdr's client
UI, direct attach, and event API are useful to the intended workflow. Do not
commit to full feature support until the checks above pass or the missing
Herdr APIs are added. This investigation changes no accepted decision or
production code.
