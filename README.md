# pop

A CLI tool for quickly switching between projects and git worktrees using tmux. Provides a fuzzy-searchable TUI powered by fzf's matching algorithm.

## Install

```bash
# Homebrew (build from source)
brew install --head glebglazov/tap/pop

# Or build manually
make install  # installs to ~/.local/bin
```

## Setup

Run `pop project dashboard` - on first run it will walk you through picking your project directories interactively.

Or create `~/.config/pop/config.toml` manually:

```toml
projects = [
    { path = "~/Dev/*/*", display_depth = 2 },
    { path = "~/.local/share/chezmoi" },
]

[work.implement]
# Ordered fallback agent list for `pop tasks implement` when no --agent flag
# is given. The first live agent runs the task; on a quota pause the next is
# tried. Defaults to ["claude"].
# agents = ["claude", "codex"]
# Started-attempt cap and the wait schedule between retries (default 3 and
# ["1m", "5m", "15m"]; an empty list restores instant retries).
# max_tries = 3
# attempt_retry_delays = ["1m", "5m", "15m"]

[work.verify]
# Enable Agent verification as a pre-approval Done gate (default false).
enabled = false
# Ordered fallback agent list for the Verifier (falls back to
# [work.implement].agents when omitted).
# agents = ["claude"]
# Verifier model-strength tier: light, standard, or heavy (default heavy).
# effort = "heavy"
# Max verify→remediate cycles before parking at VERIFY-FAILED (default 3).
# max_remediation_depth = 3
# Verify declares its own retry loop; the caps are not shared with implement.
# max_tries = 3
# attempt_retry_delays = ["1m", "5m", "15m"]

[work.routine]
# Ordered fallback agent list for Routine runs. A Routine manifest's own
# `agents` beats this group; an empty list falls through to
# [work.implement].agents.
# agents = ["claude"]

[work.attended]
# Every session pop opens for a human shares this one list — gate assistance, an
# Assist session, Map assist, map grilling, a Routine refinement session. The
# entry's cmd is the whole invocation (model included); pop appends the preset's
# declared posture flag only where cmd does not already name it (ADR-0195).
# agents = ["claude", "codex"]

# Every group's agents list takes the same entry type: a table naming the entry
# and carrying the whole agent-CLI command, with a bare string as sugar for
# { cmd = "<string>" }. `pop tasks agents` lists each group's entries in order
# with the preset and model each resolves to. Two attended entries may name the
# same preset at different models.
# [work.attended]
# agents = [
#   { display_name = "Claude Usual", cmd = "claude --model opus" },
#   { display_name = "Claude Cheap", cmd = "claude --model haiku" },
#   "codex",
# ]

[agents.claude]
# Settings keyed by agent preset rather than by kind of work. The only key left
# is output (ADR-0195): use "text" as a compatibility fallback if an agent's
# structured output fails. An attended session's whole invocation — model and
# arguments — lives in its [work.attended].agents entry.
output = "auto"

[work.implement.git]
# Commit-time git config applied only to pop's own commits during a task drain.
# Each entry is a git `-c`-style `key=value` pair. Disable GPG signing so an
# unattended `pop work daemon` drain never blocks on a 1Password presence prompt:
commit_config_overrides = ["commit.gpgsign=false"]
```

Add a tmux binding for quick access:

```bash
# ~/.tmux.conf
bind-key p display-popup -E -w 60% -h 60% 'pop project dashboard'
bind-key P display-popup -E -w 60% -h 60% 'cd "$(pop worktree dashboard)" && exec $SHELL'
# The Config dashboard pairs a key list with a preview, so give it more room:
bind-key C display-popup -E -w 80% -h 80% 'pop config dashboard'
```

## Commands

### `pop project dashboard`

Fuzzy-pick a project and switch to its tmux session. Bare git repos are automatically expanded into their worktrees.

| Key | Action |
|-----|--------|
| `enter` | Open project |
| `ctrl-k` | Kill tmux session |
| `ctrl-r` | Remove from history |
| `ctrl-u` | Clear filter |

Flag: `--tmux-cd <pane>` — send `cd` to a tmux pane instead of switching session.

### `pop worktree dashboard`

Fuzzy-pick a worktree in the current repo. Prints the selected path (useful for `cd`).

| Key | Action |
|-----|--------|
| `enter` | Open worktree |
| `ctrl-d` | Delete worktree |
| `ctrl-x` | Force delete worktree |
| `ctrl-n` | Create new worktree |

Flag: `-s, --switch` — switch tmux session instead of printing path.

### `pop config dashboard`

Browse the config keys you can override, and what each one resolves to. The left
pane lists every override-exposed key (the ones `pop config keys` marks
`[override: <scope>]`) with its description beneath and a `●` on keys that
carry an override today; the right pane previews the highlighted key in config
format — the effective value as TOML, the layer that produced it, and the value
an override is standing on.

| Key | Action |
|-----|--------|
| type | Filter over key path and description |
| `↑`/`↓` | Move highlight |
| `enter` | Edit this key's override in `$EDITOR` |
| `C-y` | Copy the source value down as the override |
| `C-x` | Remove the override, restoring the source |
| `esc` | Close |
| `C-h` | Help |

`enter` opens your editor on the whole `key = value` line in force today.
Handing back an empty buffer cancels — `C-x` is how an override is removed —
while an explicitly empty collection is a real value, which is how the verify
and routine groups' fallthrough to `work.implement.agents` is disabled on
purpose. A value that would produce a config finding re-opens the editor with
the problem instead of being written: a file pop wrote itself is never the
source of a finding. Neither `C-y` nor `C-x` asks for confirmation, because each
undoes the other.

It needs a terminal: with stdout redirected it refuses rather than printing
something that is not the dashboard.

The same dashboard opens with `alt+c` from either page of `pop work dashboard`,
as a modal over the page you are on. While it is open the dashboard's own keys
do nothing, and closing it puts you back on the page exactly as you left it —
with any override you just wrote already in force in what the page reports.

### `pop layout`

Apply a named [session template](#session-templates) to shape the current tmux session.

```bash
pop layout list           # list resolved templates for the current repo
pop layout apply gs-dev   # build the template's windows in the current session
```

`apply` runs inside an existing tmux session and is **non-destructive**: windows are matched by name, so re-applying skips windows that already exist and never touches their live panes.

### `pop configure`

Interactively add project directories to your config.

### `pop doctor`

Print a read-only command-family readiness report for `pop project`, `pop worktree`, `pop monitor`, `pop pane`, `pop tasks`, and `pop integrate`. Doctor explains degraded or blocked workflows with nested checks and next actions; it uses agent integration state only as supporting evidence when a command family depends on it.

## Live Agent Smoke

To exercise task execution against real agent CLIs, run the opt-in smoke script:

```bash
scripts/live-agent-smoke.sh codex
make live-agent-smoke AGENTS="codex claude"
```

It creates disposable git repos with a temporary task set and runs `pop tasks implement` using each selected agent preset. This can consume agent quota and depends on local CLI authentication, so it is not part of normal tests.

## Custom worktree commands

```toml
[[worktree.commands]]
key = "ctrl-o"
label = "open in editor"
command = "code $POP_WORKTREE_PATH"
exit = true
```

Available environment variables: `POP_WORKTREE_PATH`, `POP_WORKTREE_NAME`, `POP_BRANCH`, `POP_REPO_ROOT`.

## Session templates

A session template is a named blueprint for a tmux session's windows and their
pane geometry. Define them in `~/.config/pop/config.toml` (a repo `.pop/config.toml`
or a global `[repo."<path>"]` block can add or override templates per checkout). Apply
one with [`pop layout apply <name>`](#pop-layout).

A window's `layout` is a tree. A leaf runs a `command`; a container splits its
`children` either into `"rows"` (stacked top→bottom) or `"columns"` (side-by-side),
sizing them by relative `weight` (default `1`).

### A single-pane window

```toml
[[workbenches]]
name = "logs"

[[workbenches.windows]]
name = "tail"
layout = { command = "tail -f app.log" }
```

### A split layout with weights and focus

`vim` takes 3/5 of the height, `claude` the rest; the cursor lands on `vim`:

```toml
[[workbenches]]
name = "dev"

[[workbenches.windows]]
name = "edit"
layout.children = "rows"
layout.panes = [
  { name = "vim",    weight = 3, command = "vim", focus = true },
  { name = "claude", weight = 1, command = "claude" },
]
```

- `weight` is normalized within siblings — `3` and `1` mean 75% / 25%.
- `focus = true` on one leaf makes it the active pane after apply (first wins).
- `cwd` sets a pane's working directory (relative to the session dir, or `~`/absolute);
  it inherits down into nested containers. Omit it to inherit the parent.

### Nesting containers

Containers nest to any depth. Here the bottom row is split into three columns:

```toml
[[workbenches]]
name = "gs-dev"

[[workbenches.windows]]
name = "dev"
layout.children = "rows"
layout.panes = [
  { name = "vim",    weight = 3, command = "vim", focus = true },
  { name = "claude", weight = 1, command = "claude" },
  { weight = 1, children = "columns", panes = [
    { name = "build",    command = "make watch" },
    { name = "services", command = "bin/services up" },
    { name = "web",      command = "npm run dev" },
  ] },
]
```

Multiline inline tables and trailing commas are accepted, so deep trees stay
readable. Multiple `[[workbenches.windows]]` blocks make a multi-window
template; the first window is active after apply.

## Dashboard

`pop monitor dashboard` shows all tracked tmux panes sorted by status and last-visit time. Switch between them with fuzzy search.

The old `pop dashboard` form remains available temporarily as a hidden compatibility alias and will be removed at the next major CLI change.

| Key | Action |
|-----|--------|
| `enter` | Switch to pane (mark as clear) |
| `shift-enter` | Peek pane (keep unread) |
| `ctrl-r` | Toggle clear/unread |
| `ctrl-f` | Toggle follow |

## Pane monitoring

`pop` can track which tmux panes need attention:

```bash
# Mark a pane as working / unread / clear
pop pane set-status %1 working
pop pane set-status %1 unread
pop pane set-status %1 clear

# Record a manual visit (updates last-visit time)
pop pane visit %1
```

### Auto-visit tracking

To automatically record pane visits when you switch between them, add to `~/.tmux.conf`:

```bash
set -g focus-events on
```

The monitor daemon installs a `pane-focus-in` hook that calls `pop pane visit` on every pane switch. Without `focus-events on`, tmux does not fire this hook and visits are not tracked automatically.
