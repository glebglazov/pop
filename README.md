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

[workload.agents.claude]
# Use "text" as a compatibility fallback if an agent's structured output fails.
output = "auto"
```

Add a tmux binding for quick access:

```bash
# ~/.tmux.conf
bind-key p display-popup -E -w 60% -h 60% 'pop project dashboard'
bind-key P display-popup -E -w 60% -h 60% 'cd "$(pop worktree dashboard)" && exec $SHELL'
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

### `pop configure`

Interactively add project directories to your config.

### `pop doctor`

Print a read-only command-family readiness report for `pop project`, `pop worktree`, `pop monitor`, `pop pane`, `pop tasks`, and `pop integrate`. Doctor explains degraded or blocked workflows with nested checks and next actions; it uses agent integration state only as supporting evidence when a command family depends on it.

## Live Agent Smoke

To exercise task execution against real agent CLIs, run the opt-in smoke script:

```bash
scripts/live-workload-agent-smoke.sh codex
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
