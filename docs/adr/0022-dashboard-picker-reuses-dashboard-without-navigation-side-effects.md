# Dashboard picker reuses Dashboard without navigation side effects

`pop monitor dashboard --pick` reuses the **Dashboard** UI as a selection-only **Dashboard picker**: it returns the selected **Pane ID target** to the caller and does not switch tmux focus, record History, clear Unread, or apply other dashboard maintenance mutations. Message-sending callers use **Session-local panes** by default, excluding the current pane itself; picker mode auto-selects when exactly one candidate exists and exits unsuccessfully without output when no candidate exists. Quick selection is enabled for picker mode only in the first pass, reusing the existing `quick_access_modifier` setting; fuzzy text filtering is deferred.

This keeps the Neovim message-sending flow scriptable while preserving the Dashboard's pane preview and familiar navigation. We are deliberately not creating a separate agent-only picker: the candidate model remains tracked **Panes**, narrowed by session locality rather than by inferred agent identity.

Picker mode is intentionally strict about locality: if Pop cannot determine the current tmux session, it exits unsuccessfully instead of broadening to all tracked panes. Cancel remains silent; state/setup failures may print a short stderr message while keeping stdout reserved for the selected pane ID.

The first implementation does not include an escape hatch for all tracked panes: under `pop monitor dashboard`, `--pick` means session-local selection.

`pop pane send --pane-id` is the matching write primitive: it sends keys directly to an explicit tmux pane ID and does not require the pane to be tracked by Pop, resolve a project session, or live in an `agent` window.

`pop pane send` remains a general key-sending primitive: Enter is explicit, and no dedicated `message` command is introduced for the first implementation.

## Considered Options

- **Create a separate pane picker.** Rejected: it would duplicate Dashboard preview/navigation behavior and diverge from the place users already inspect tracked panes.
- **Make normal Dashboard Enter return a pane ID.** Rejected: the existing Dashboard is a navigation and triage surface with side effects; picker mode needs a pure selection contract for write actions.
- **Filter to inferred agentic panes.** Rejected: tracked panes are the domain concept Pop already owns; "agentic" is not reliable enough to infer from labels or process names for this targeting contract.
