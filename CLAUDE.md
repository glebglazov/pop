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
- **internal/deps/** - Interfaces and implementations for external dependencies (git, filesystem, tmux)

### Testing Approach

External dependencies (git commands, filesystem operations, tmux) are abstracted behind interfaces in `internal/deps/`. This enables fast, reliable unit tests without real system calls.

**Pattern:**
- Each package has a `Deps` struct holding its dependencies
- `DefaultDeps()` returns real implementations for production
- `*With(d *Deps, ...)` functions accept injected dependencies for testing
- Wrapper functions (e.g., `DetectRepoContext()`) call the `*With` variant with default deps

**Example:**
```go
// Production code calls the simple wrapper
ctx, err := project.DetectRepoContext()

// Tests inject mocks
d := &project.Deps{
    Git: &deps.MockGit{
        CommandFunc: func(args ...string) (string, error) {
            return "/mock/path", nil
        },
    },
    FS: &deps.MockFileSystem{...},
}
ctx, err := project.DetectRepoContextWith(d)
```

**Available mocks in `internal/deps`:**
- `MockGit` - git command execution
- `MockFileSystem` - file/directory operations
- `MockTmux` - tmux session management
- `MockFileInfo`, `MockDirEntry` - test helpers for fs operations

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

### Cursor Memory Behavior

The picker remembers cursor position per filter query during a session. This allows users to navigate between different filters without losing their place.

**Behavior:**
1. When entering a filter for the **first time**, cursor positions at the best match (bottom of list)
2. When user **moves cursor** while filtered, that selection is remembered for that filter query
3. When **returning to the same filter**, cursor restores to the previously selected item
4. When **clearing the filter** (empty query), cursor returns to the remembered position in the unfiltered list

**Example scenario:**
```
Initial state (no filter):     Filter "a":                After moving cursor up:
  zhw                            ab1                        ab1
  ab1                            abl                        <cursor>abl
  abl                            <cursor>abc                abc
  tqr
  <cursor>abc

Clear filter:                  Re-apply filter "a":
  zhw                            ab1
  ab1                            <cursor>abl    <- remembered!
  <cursor>abl  <- remembered!   abc
  tqr
  abc
```

**Implementation details:**
- `cursorMemory map[string]string` maps filter query → selected item's Path
- On query change: save current selection to memory, then restore from memory if available
- Empty string `""` is a valid query key (represents unfiltered state)

### Configuration

Config file: `~/.config/pop/config.toml` (respects XDG_CONFIG_HOME)
```toml
projects = [
    { path = "~/Dev/*/*", display_depth = 2 },
    { path = "~/.local/share/chezmoi" },
]
```

Each project entry is an object with:
- `path` (required) — exact path or glob pattern
- `display_depth` (optional, default 1) — number of trailing path segments to show in the picker display name. E.g. with depth 2, `/home/user/Dev/work/app` displays as `work/app`

History file: `~/.local/share/pop/history.json` (respects XDG_DATA_HOME)

### Dependencies

- `spf13/cobra` - CLI framework
- `charmbracelet/bubbletea` - TUI framework
- `BurntSushi/toml` - Config parsing
- `bmatcuk/doublestar` - Glob patterns
- `junegunn/fzf` - Fuzzy matching (uses fzf's FuzzyMatchV2 algorithm)
