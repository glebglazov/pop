---
description: Spawn a new coding-agent CLI session in a background tmux pane via pop pane, in auto-approve mode, and report remote-control options when the spawned agent supports them. Use when the user asks to launch/create another agent session in tmux, start a background agent, or spin up a remote-controllable agent session.
argument-hint: "Optional: agent name (default same agent running this skill), a directory to start in, and/or an initial prompt"
---

Spawn a new coding-agent CLI in a background tmux pane through `pop pane` (see the tmux-pane skill). The pane lands in the Spawn window (`pop-spawn`). Capture the pane id from stdout and use it for all follow-up — agents rewrite their pane title on launch, which breaks name-based lookup.

## Agent registry

Each row is one agent preset pop supports. Auto-approve and remote-control facts were read from each CLI's `--help` on 2026-07-29 and have not been smoke-tested by launching each agent.

| Agent | Keyword(s) | Launch binary | Auto-approve | Remote control |
|-------|------------|---------------|--------------|----------------|
| claude | `claude` | `claude` | `--dangerously-skip-permissions` | `/remote-control` slash command prints a per-session `https://claude.ai/code/...` link |
| codex | `codex` | `codex` | `--dangerously-bypass-approvals-and-sandbox` on `exec` (interactive flag unverified) | `codex remote-control start\|stop\|pair` — experimental, daemon-shaped; `pair` prints a short-lived code (not a hosted session link) |
| cursor | `cursor` | `cursor-agent` | `--force` (alias `--yolo`) | none — do not report or automate a remote-control link |
| pi | `pi` | `pi` | no permission gate; `-a`/`--approve` only trusts project-local files | `/share` opens a read-only viewer, not control — do not report a remote-control link |
| opencode | `opencode` | `opencode` | `--auto` | `opencode web` / `opencode serve` plus `attach <url>` — self-hosted server, no hosted per-session link |

If the user names an agent not in this table, say it is not supported yet rather than guessing flags.

## Default agent

Unless the user names another agent, spawn **the same preset as the agent running this skill**:

- Claude Code → `claude`
- Codex CLI → `codex`
- Cursor Agent → `cursor`
- Pi → `pi`
- OpenCode → `opencode`

Naming a different keyword from the registry overrides this default (for example, while running in Claude, spawn Cursor with `cursor`).

## Parsing arguments

Arguments are optional and free-form. Extract:

- **Agent name** — keyword from the registry; default per above.
- **Target directory** — a path (`~/Dev/foo`, `../bar`, absolute). If none, use the current directory/session. Expand `~`/relative to absolute and confirm it exists (`ls -d <path>`) before spawning.
- **Initial prompt** — remaining text to send after the session is up.

## Steps

1. **Verify tmux.** Run `echo "$TMUX"`. If empty, stop — this skill requires an active tmux session.

2. **Resolve the agent** from the registry — launch binary, auto-approve flag, and remote-control shape.

3. **Build the launch command** with auto-approve baked in, for example `claude --dangerously-skip-permissions` or `cursor-agent --force`.

4. **Spawn via pop pane** into the Spawn window. Use a distinct pane name (`claude-spawn`, `cursor-spawn`, …). Capture the pane id:

   ```bash
   PANE_ID=$(pop pane create <agent>-spawn "<launch command>")
   echo "PANE_ID=$PANE_ID"
   ```

   With a target directory, add `--project <abs-path>`:

   ```bash
   PANE_ID=$(pop pane create <agent>-spawn "<launch command>" --project <abs-path>)
   ```

   Pane ids are global across the tmux server; later commands use `tmux ... -t "$PANE_ID"` or `pop pane` subcommands that accept a pane id.

5. **Wait for startup.** direnv and agent boot take a few seconds. Sleep ~4s, then capture:

   ```bash
   sleep 4; tmux capture-pane -p -t "$PANE_ID" | grep -v '^$' | tail -20
   ```

   Retry until the agent banner and input line appear.

6. **Remote control — only when the registry row offers it.**

   - **claude:** send the slash command, wait, capture; extract the `claude.ai/code` URL:

     ```bash
     tmux send-keys -t "$PANE_ID" "/remote-control" Enter
     sleep 3; tmux capture-pane -p -t "$PANE_ID" | grep -v '^$' | tail -20
     ```

   - **codex:** tell the user about `codex remote-control pair` (short-lived code). Do not fabricate a session link.
   - **cursor, pi:** skip — no remote control.
   - **opencode:** tell the user about `opencode web` / `serve` and `attach <url>`. Do not claim a hosted per-session link.

7. **(Optional) Send an initial prompt** after any remote-control step:

   ```bash
   tmux send-keys -t "$PANE_ID" "<prompt>" Enter
   ```

8. **Report back:** agent, pane id, directory, auto-approve mode, and remote-control info (or explicitly that this agent has none / name the relevant command).

## Notes

- **Always target the pane by id (`$PANE_ID`), not by name** — title rewrites break `pop pane capture <name>` / `find` after startup.
- To spawn several at once, loop with distinct pane names (`claude-spawn`, `claude-spawn-2`, …). `pop pane create` is idempotent per name and auto-recreates a dead pane.
- Never list a per-session remote link for cursor, pi, or opencode.
