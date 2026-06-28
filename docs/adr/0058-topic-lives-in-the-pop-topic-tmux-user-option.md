---
status: accepted
---

# Topic lives in the `@pop_topic` tmux user-option

A pane's **Topic** is stored as the per-pane tmux user-option `@pop_topic` (set via `tmux set-option -p`, read via `#{@pop_topic}`), which becomes its **single source of truth** — the Monitor no longer keeps a separate Topic field, and the dashboard reads the option directly. We chose this so the Topic is reusable across *any* tmux surface (pane-border-format, status line, `list-panes`), not just pop's dashboard — the user can drop `#{@pop_topic}` into custom tmux labels. The existing `@pop_set` option (queue) establishes the per-pane user-option pattern this follows.

Crucially, the Topic is **not** written to `#{pane_title}`. `pane_title` is already pop's pane **identity** key — `pop pane create <name>` sets it and every `pop pane <name>` lookup matches on it (`findPaneWith`) — and tmux offers only one title slot per pane. Overloading it with a mutable Topic would break pane addressing, so identity stays on `pane_title` and the Topic gets its own option.

## Considered Options

- **Write the Topic into `#{pane_title}`.** Rejected: it's pop's identity/lookup key; a mutable Topic there breaks `pop pane <name>` and fights the agent's own OSC title-setting. Freeing the title would force re-keying identity onto another option — a large, hard-to-reverse change for no gain over a user-option that's equally reusable in tmux formats.
- **Keep the Topic in Monitor state and dual-write to the option.** Rejected: two stores to keep in sync and drift between, with no upside once the option alone is canonical.

## Consequences

- `@pop_topic` dies with the pane, which is exactly the lifecycle [ADR-0025](0025-topic-command-derives-once-per-pane.md) wants: the once-per-pane guard is now `#{@pop_topic} == ""`, so a retired/restarted pane re-derives. Clearing the option is a free manual "refresh."
- **Drain pre-seeds the Topic** from the task **Title** (slugified to the same kebab format): pop sets `@pop_topic` at drain spawn, so the agent's `set-topic --derive` hook sees it already set and no-ops. Drained panes get a perfect Topic with no model call; only non-drain panes run the recipe chain ([ADR-0057](0057-topic-generation-is-pop-owned-via-curated-agent-recipes.md)).
- A user-authored **Note** still outranks the Topic in dashboard display; the Note stays in Monitor state (different writer, different lifecycle), so storage splits by author — hook-written Topic on the pane, dashboard-written Note in the Monitor.
- Surfacing the Topic in tmux now depends on the user referencing `#{@pop_topic}` in their tmux config; pop sets the option but does not impose a default `pane-border-format`.
