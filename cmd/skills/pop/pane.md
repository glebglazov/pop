---
description: Manage long-running processes and interactive terminals via named tmux panes, and control pop's pane attention/working status. A friendly wrapper around tmux — use it whenever you need to run a process in the background, monitor its output, send it input, or interact with a TUI. Covers dev servers, builds, watchers, REPLs, test runners, log tailing, docker containers, and anything else you'd run in a terminal. Also use this when you need to test or debug programs in a real shell environment — write a script, run it in a pane, capture output, send interactive input, and iterate. Whenever your task involves running something and then inspecting what happened, sending keystrokes to a running program, or marking another pane as needing attention, reach for pop pane. Run `pop pane --help` to discover all available subcommands.
---

# pop pane — Named Tmux Pane Management

## Why use this

`pop pane` gives you **named, persistent tmux panes** — real interactive shell sessions you can create, script against, and clean up by name. Each pane is a full shell with your rc files and environment loaded, running in the background without stealing focus.

Two main use cases:

1. **Long-running processes**: dev servers, file watchers, database containers, test suites in watch mode — anything that needs to persist while you keep working.
2. **Testing and debugging in a real shell**: run scripts, test CLI tools, interact with programs step by step, and capture output to verify behavior. Unlike the Bash tool (which runs commands in isolation), panes give you a persistent shell with state that carries across commands — environment variables, directory changes, shell history, and running processes all survive between `send` calls.

Because this wraps tmux `send-keys`, you can type into running processes, answer interactive prompts, navigate TUIs, send Ctrl-C, and pipe arbitrary input — all by name rather than by pane ID.

All panes live in a shared "agent" window within the current tmux session. Pane names are unique per session.

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
Prints the pane's visible content plus 50 lines of scrollback. ANSI codes are stripped for clean output.

### Kill a pane
```bash
pop pane kill <name>
```
Kills the pane. Remaining panes re-tile automatically.

### Set pane status
```bash
pop pane set-status <pane_id> <status>
```
Marks a tmux pane in the pop monitor as `working`, `needs_attention`, or `read`. Pane status drives the dashboard (`pop dashboard`) and the unread/attention indicators in the picker.

If you omit `<pane_id>`, the command reads `$TMUX_PANE` from the environment, so it operates on whatever pane it was invoked from. From an agent, you almost always want to pass an explicit pane id.

Statuses:
- `working` — actively in use; the agent or process is busy
- `needs_attention` — has output the user should look at
- `read` — user has acknowledged it (resets the attention flag)

You normally don't have to call this manually — running `pop integrate <agent>` installs hooks/extensions that keep the agent's own pane status in sync. Reach for `set-status` directly when:

- You spawned a long-running process in another pane and want to flag it for attention once it finishes:
  ```bash
  ID=$(pop pane create build "npm run build")
  # ... poll the pane until the build is done ...
  pop pane set-status "$ID" needs_attention
  ```
- You want to clear the attention flag on a pane after acknowledging its output:
  ```bash
  pop pane set-status "$ID" read
  ```

To discover which panes exist and their current ids, use `pop pane list`. To see the full monitor table including each pane's current status, use `pop pane status`.

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

## Testing & Debugging Workflows

Panes are real shells — use them as a feedback loop for testing programs, scripts, and CLI tools. The pattern is always: send a command, capture output, decide what to do next.

### Run a script and verify its output
```bash
pop pane create test "bash"
pop pane send test "./my_script.sh" Enter
sleep 2
pop pane capture test  # read stdout/stderr to verify behavior
```

### Check exit codes
There's no built-in exit code query — use `echo $?` right after the command:
```bash
pop pane create test "bash"
pop pane send test "./might_fail.sh" Enter
sleep 2
pop pane send test "echo EXIT_CODE=\$?" Enter
sleep 1
pop pane capture test  # look for EXIT_CODE=0 (or non-zero)
```

### Test a CLI tool with different arguments
```bash
pop pane create test "bash"
pop pane send test "mytool --input foo.csv --format json" Enter
sleep 1
pop pane capture test  # check output
pop pane send test "mytool --input bar.csv --format csv" Enter
sleep 1
pop pane capture test  # check second run
pop pane kill test
```

### Interactive debugging — step through a program
```bash
pop pane create debug "python3 -m pdb my_script.py"
sleep 1
pop pane capture debug    # see where it stopped
pop pane send debug "n" Enter   # next line
pop pane capture debug
pop pane send debug "p some_var" Enter  # inspect a variable
pop pane capture debug
pop pane send debug "c" Enter   # continue
pop pane capture debug    # see final output
pop pane kill debug
```

### Write a test script, run it, iterate
When you need to run something more complex than a one-liner, write a script first, then execute it in a pane:
```bash
# 1. Write the test script to a file (using Write tool or Bash)
# 2. Run it
pop pane create test "bash /tmp/test_something.sh"
sleep 3
pop pane capture test  # read results
# 3. If something is wrong, fix the script, then re-run:
pop pane create test "bash /tmp/test_something.sh"  # auto-recreates dead pane
```

### Test a server end-to-end
```bash
# Start the server
pop pane create server "npm run dev"
sleep 3
pop pane capture server  # verify it's listening

# Hit it from another pane
pop pane create client "bash"
pop pane send client "curl -s http://localhost:3000/api/health" Enter
sleep 1
pop pane capture client  # check the response

# Clean up
pop pane kill client
pop pane kill server
```

### Send multi-line input to a program
For programs expecting multi-line input (configs, heredocs, paste buffers), send each line separately:
```bash
pop pane create editor "python3"
sleep 1
pop pane send editor "data = {" Enter
pop pane send editor "    'name': 'test'," Enter
pop pane send editor "    'value': 42," Enter
pop pane send editor "}" Enter
pop pane send editor "print(data)" Enter
pop pane capture editor
```

## Discoverability

Run `pop pane --help` or `pop pane <subcommand> --help` to see full usage details and flags. Run `pop --help` to see other `pop` commands beyond pane management.

## Guidelines

- Always give panes descriptive names (`server`, `db`, `tests`, `build`, `repl`, `test`, `debug`)
- `create` is idempotent — safe to call repeatedly for the same name
- Dead panes are auto-recreated on next `create` call
- Use `pop pane capture` to check process output rather than guessing
- Use `send` to interact with processes: answer prompts, send Ctrl-C, type commands
- Send `C-c` before killing if you need a graceful shutdown
- Clean up panes with `pop pane kill` when done
- **For testing, prefer panes over the Bash tool** when you need persistent shell state, interactive input, or want to test how a program behaves in a real terminal environment. The Bash tool is still fine for simple, self-contained commands.
- **Check exit codes explicitly** with `echo $?` after a command — there's no built-in exit code query.
- **Use `sleep` between `send` and `capture`** to give commands time to produce output. Short commands need 1-2s, builds and servers may need longer.
- **Run processes in the foreground, not daemon mode.** The pane itself is already a background context — there's no need to double-detach. Running in daemon mode (e.g. `docker compose up -d`, `npm start &`) hides the output from the pane, which means `pop pane capture` returns nothing useful and you lose the ability to monitor logs, spot errors, or interact with the process. Use `docker compose up` (no `-d`), not `docker compose up -d`.
