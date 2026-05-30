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

Run `pop project` — on first run it will walk you through picking your project directories interactively.

Or create `~/.config/pop/config.toml` manually:

```toml
projects = [
    { path = "~/Dev/*/*", display_depth = 2 },
    { path = "~/.local/share/chezmoi" },
]
```

Add a tmux binding for quick access:

```bash
# ~/.tmux.conf
bind-key p display-popup -E -w 60% -h 60% 'pop project'
bind-key P display-popup -E -w 60% -h 60% 'cd "$(pop worktree)" && exec $SHELL'
```

## Commands

### `pop project`

Fuzzy-pick a project and switch to its tmux session. Bare git repos are automatically expanded into their worktrees.

| Key | Action |
|-----|--------|
| `enter` | Open project |
| `ctrl-k` | Kill tmux session |
| `ctrl-r` | Remove from history |
| `ctrl-u` | Clear filter |

Flag: `--tmux-cd <pane>` — send `cd` to a tmux pane instead of switching session.

### `pop worktree`

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

`pop dashboard` shows all tracked tmux panes sorted by status and last-visit time. Switch between them with fuzzy search.

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
