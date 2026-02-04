# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**pop** is a CLI tool for quickly switching between projects and git worktrees using tmux. It provides a fuzzy-searchable TUI for selecting from configured project directories and manages tmux sessions based on selections.

## Build & Test Commands

```bash
make build          # Build binary
make install        # Build and install to ~/.local/bin
make test           # Run all tests

# Run tests for a specific package
go test ./cmd
go test ./project
go test ./history
```

## Architecture

### Package Structure

- **cmd/** - Cobra CLI commands (`select`, `worktree`)
- **config/** - TOML config loading and glob pattern expansion
- **project/** - Domain models (Project, Worktree, RepoContext) and git operations
- **history/** - JSON-based project access tracking for recency sorting
- **ui/** - Bubbletea-based fuzzy picker TUI

### Key Workflows

**`pop select`**: Loads config → expands project paths (parallel worktree detection for bare repos) → sorts by history recency → displays picker → creates/attaches tmux session

**`pop worktree`**: Detects repo context → lists worktrees via `git worktree list --porcelain` → sorts by tmux activity → displays picker with delete/create actions

### Worktree Branch Display

In worktree mode, each worktree has an associated branch parsed from `git worktree list --porcelain`:
- `branch refs/heads/<name>` → extracted as branch name
- `detached` → shown as "detached"

The branch is stored in `ui.Item.Context` field. Display format follows `[branch] name` with aligned brackets for visual consistency.

### Important Patterns

- **Parallel Processing**: Project expansion uses goroutines for concurrent worktree detection
- **Two Worktree Detection Modes**:
  - File-based (fast): Checks for `.bare` directory and `.git` files - used for initial expansion
  - Git-based (accurate): Parses `git worktree list --porcelain` - used for in-repo operations
- **Session Name Sanitization**: Replaces `.` and `:` with `_` for tmux compatibility
- **History Sorting**: Unvisited projects first (alphabetical), then by access time (oldest→newest), cursor at end

### Configuration

Config file: `~/.config/pop/config.toml` (respects XDG_CONFIG_HOME)
```toml
projects = [
    "~/Dev/*/*",
    "~/.local/share/chezmoi",
]
```

History file: `~/.local/share/pop/history.json` (respects XDG_DATA_HOME)

### Dependencies

- `spf13/cobra` - CLI framework
- `charmbracelet/bubbletea` - TUI framework
- `BurntSushi/toml` - Config parsing
- `bmatcuk/doublestar` - Glob patterns
- `sahilm/fuzzy` - Fuzzy matching
