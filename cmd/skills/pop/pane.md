---
description: Manage long-running processes and interactive terminals via named tmux panes. A friendly wrapper around tmux — use it whenever you need to run a process in the background, monitor its output, send it input, or interact with a TUI. Covers dev servers, builds, watchers, REPLs, test runners, log tailing, docker containers, and anything else you'd run in a terminal. Run `pop pane --help` to discover all available subcommands.
---

# pop pane — Named Tmux Pane Management

## Why use this

Running commands inline with Bash works for short-lived tasks, but many workflows need processes that persist: dev servers, file watchers, database containers, test suites in watch mode. `pop pane` gives you **named, persistent tmux panes** that you can create, monitor, interact with, and clean up — all without losing focus on your current work.

Because this wraps tmux, you get the full power of tmux send-keys: you can type into running processes, answer interactive prompts, navigate TUIs, send Ctrl-C to stop things gracefully, and pipe arbitrary input — all by name rather than by pane ID.

All panes live in a shared "agent" window within the current tmux session. Pane names are unique per session. Panes are created in the background without stealing focus.

## Commands

### Create a pane
```bash
pop pane create <name> "<command>"
```
Creates a named pane running the given command. Prints the tmux pane ID.
- First pane creates the "agent" window
- Subsequent panes split and auto-tile
- Pane stays open after command exits so you can read the output
- **Idempotent**: if a pane with that name is already running, returns its ID
- **Auto-recreate**: if a pane with that name exists but its command has exited, kills it and creates a fresh one

### Find a pane
```bash
pop pane find <name>
```
Prints the tmux pane ID (e.g., `%5`). Useful to check if a pane exists — returns error if not found.

### List panes
```bash
pop pane list
```
Lists all panes in the agent window as `title<TAB>pane_id` lines.

### Send keys to a pane
```bash
pop pane send <name> <keys...>
```
Sends literal keys to a pane via tmux `send-keys`. This is the most versatile command — it lets you interact with any running process as if you were typing into it. Keys are NOT auto-terminated with Enter — include `Enter` explicitly if needed.

Examples:
```bash
pop pane send server "npm run dev" Enter    # type command + press Enter
pop pane send server C-c                     # send Ctrl+C
pop pane send server q                       # send literal "q" (e.g. to quit less)
pop pane send repl "print('hello')" Enter    # type into a Python REPL
pop pane send app y Enter                    # answer a yes/no prompt
```

### Capture pane content
```bash
pop pane capture <name>
```
Prints the pane's visible content plus 500 lines of scrollback. Includes ANSI color codes.

### Kill a pane
```bash
pop pane kill <name>
```
Kills the pane. Remaining panes re-tile automatically.

## Cross-project targeting

All commands accept `--project <path>` to target another project's tmux session:
```bash
pop pane create server "npm start" --project ~/Dev/frontend
pop pane capture server --project ~/Dev/frontend
```
The session is auto-created if it doesn't exist.

## Common Workflows

### Start a dev server and verify it's running
```bash
pop pane create server "npm run dev"
sleep 2
pop pane capture server  # check for "ready" or "listening" in output
```

### Run a short-lived command and read its output
```bash
pop pane create build "npm run build"
sleep 5
pop pane capture build  # check for errors — pane stays open after exit
pop pane kill build     # clean up when done
```

### Re-run a command (auto-recreate)
```bash
pop pane create build "npm run build"   # first run
# ... time passes, build finishes (pane stays with output) ...
pop pane create build "npm run build"   # detects dead pane, kills it, creates fresh one
```

### Restart a long-running process
```bash
pop pane send server C-c                      # stop current process
pop pane send server "npm start" Enter        # restart
```

### Interact with a running process
```bash
pop pane create repl "python3"
sleep 1
pop pane send repl "import json" Enter
pop pane send repl "json.dumps({'a': 1})" Enter
pop pane capture repl  # see the output
```

### Check what's running
```bash
pop pane list                    # see all active panes
pop pane capture server          # see server output
```

### Idempotent setup (safe to call multiple times)
```bash
pop pane create server "npm run dev"      # creates if not running
pop pane create db "docker compose up"    # creates if not running
pop pane create server "npm run dev"      # already running — returns existing ID
```

## Discoverability

Run `pop pane --help` or `pop pane <subcommand> --help` to see full usage details and flags. Run `pop --help` to see other `pop` commands beyond pane management.

## Guidelines

- Always give panes descriptive names (`server`, `db`, `tests`, `build`, `repl`)
- `create` is idempotent — safe to call repeatedly for the same name
- Dead panes are auto-recreated on next `create` call
- Use `pop pane capture` to check process output rather than guessing
- Use `send` to interact with processes: answer prompts, send Ctrl-C, type commands
- Send `C-c` before killing if you need a graceful shutdown
- Clean up panes with `pop pane kill` when done
