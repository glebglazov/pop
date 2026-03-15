---
description: Manage named tmux panes for running dev servers, builds, and background processes. Use when you need to start a process, check its output, send it commands, or clean it up.
---

# pop pane — Named Tmux Pane Management

You have access to `pop pane` for managing named tmux panes. Use this to run dev servers, builds, watchers, and other background processes in dedicated panes that you can monitor and interact with.

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
Sends literal keys to a pane via tmux `send-keys`. Keys are NOT auto-terminated with Enter — include `Enter` explicitly if needed.

Examples:
```bash
pop pane send server "npm run dev" Enter    # type command + press Enter
pop pane send server C-c                     # send Ctrl+C
pop pane send server q                       # send literal "q"
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

## Guidelines

- Always give panes descriptive names (`server`, `db`, `tests`, `build`)
- `create` is idempotent — safe to call repeatedly for the same name
- Dead panes are auto-recreated on next `create` call
- Use `pop pane capture` to check process output rather than guessing
- Send `C-c` before killing if you need a graceful shutdown
- Clean up panes with `pop pane kill` when done
