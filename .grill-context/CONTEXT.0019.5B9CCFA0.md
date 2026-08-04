+ Attended argument defaults
  The per-**Agent preset** argument list pop passes to an attended agent session —
  a **HITL assistance session**, an **Assist session**, **Map assist**, map
  grilling, a **Routine refinement session**, and every gate that launches an
  agent. Each preset declares its own, and auto-approval is the default: claude
  `--dangerously-skip-permissions`, cursor `--force --trust`, codex
  `--dangerously-bypass-approvals-and-sandbox`, opencode and kimi none (kimi's
  auto-permission *is* its headless `-p`, and it rejects `--yolo`/`--auto`). They
  are **defaults, not pop-owned flags**: `[agents.<preset>].attended_args`
  replaces the list wholesale instead of being overridden by it, the one
  deliberate exception to the flags-come-last rule of **Agent preset**. A preset
  with no auto-approval flag of its own passes nothing rather than refusing, so
  the option is uniform across presets that cannot honour it.
  avoid: yolo mode, auto-mode, skip-permissions flag, assist args
  under: Agents

+ Agents config root
  `[agents.<preset>]` — the per-preset home for agent-invocation settings that
  belong to no single verb: **Attended argument defaults** (`attended_args`) and
  the attended model (`attended_model`, unset meaning pop names no model and the
  agent's own configuration decides). It exists because attended sessions span
  **Task set**s, **Map**s and **Routine**s, so `[tasks.presets.<name>]` — which
  keeps `output` — is the wrong root for them. Includable, merged map-first-wins
  per preset with per-field merge inside, so a machine include can set one
  preset's arguments without erasing another's.
  avoid: [assist], [gates], per-verb agent flags, [tasks.presets] (for attended settings)
  under: Configuration

~ Agent preset
  A headless agent the task executor recognizes — `claude`, `opencode`, `cursor`,
  `codex`, `pi`, or `kimi` — selected by name and optionally augmented with extra
  invocation arguments (e.g. `claude --model opus4.8`). Pop runs the supplied
  command as given, exactly as it runs a **Custom agent command**; the sole
  difference is recognition. Because the first token names a known agent, the
  **Agent adapter** appends the flags Pop owns — the output protocol governed by
  **Agent output mode** — after the user's arguments, then delivers the generated
  prompt per-adapter: as the final positional argument for most presets, as the
  `-p` flag value for `kimi` (which has no positional-prompt form). Pop-owned
  flags come last among flags, making them authoritative: a user value for an
  owned flag is overridden, not rejected. **This governs headless invocation
  only.** An attended session takes the preset *name* from the same list but
  none of its arguments: those come solely from the **Agents config root**, where
  **Attended argument defaults** are defaults the user replaces rather than flags
  pop asserts. Recognition is what lets Pop parse the structured stream and keep
  every adapter capability; augmenting a recognized preset this way is distinct
  from replacing the invocation with a Custom agent command.
  was: A headless agent the task executor recognizes — `claude`, `opencode`,
    `cursor`, `codex`, `pi`, or `kimi` — selected by name and optionally augmented
    with extra invocation arguments (e.g. `claude --model opus4.8`). Pop runs the
    supplied command as given, exactly as it runs a **Custom agent command**; the
    sole difference is recognition. Because the first token names a known agent,
    the **Agent adapter** appends the flags Pop owns — the output protocol governed
    by **Agent output mode** — after the user's arguments, then delivers the
    generated prompt per-adapter: as the final positional argument for most
    presets, as the `-p` flag value for `kimi` (which has no positional-prompt
    form). Pop-owned flags come last among flags, making them authoritative: a user
    value for an owned flag is overridden, not rejected. Recognition is what lets
    Pop parse the structured stream and keep every adapter capability; augmenting a
    recognized preset this way is distinct from replacing the invocation with a
    Custom agent command.

~ Agent adapter
  The preset-specific bridge between Pop and a supported agent, declaring every
  **Adapter capability** explicitly — there is no capability an adapter simply
  omits. Attended assistance launches the preset's own interactive binary and is
  owned by the adapter rather than the HITL gate prompt, including that binary's
  **Attended argument defaults**: the adapter is where the per-preset knowledge
  of which flag means auto-approval lives, since the answer differs per agent and
  two of them have no such flag. An adapter reports assistance Unavailable only
  when it has no usable interactive command at all (e.g. custom headless
  `--agent-cmd`).
  was: The preset-specific bridge between Pop and a supported agent, declaring
    every **Adapter capability** explicitly — there is no capability an adapter
    simply omits. Attended assistance launches the preset's own interactive binary
    and is owned by the adapter rather than the HITL gate prompt. An adapter
    reports assistance Unavailable only when it has no usable interactive command
    at all (e.g. custom headless `--agent-cmd`).

~ History
  The persisted record of checkouts a human has been put into, with timestamps —
  a row per path in the **Execution-state store**, upserted in one transaction
  (the standalone `history.json` is folded in on first read and then ignored).
  Recorded whenever an act hands the human a session or pane: a **Switch**, a
  window open, a cd-to-pane landing, every **Handoff verb** on the **Work
  dashboard** — including a manually launched drain, verify, fold, **Assist
  session** or **Runtime shell**, recorded at the set's **Runtime path** —
  `pop map open`/`assist`/`next`, recorded at the Map's **Trunk worktree**, and
  the **Routine refinement session** spawn. Never recorded for what the **Work
  daemon** spawns unattended: the record answers "where have I been", not "where
  has pop opened something", so overnight machine work cannot reorder the
  **Project picker**. A picker selection abandoned before the handoff (e.g. Esc at
  the Workbench prompt) leaves no entry.
  was: The persisted record of projects you've switched to, with timestamps.
    Recorded only when a Switch (or window open / cd-to-pane landing) actually
    happens — a picker selection abandoned before that point (e.g. Esc at the
    Workbench prompt) leaves no entry.

~ Switch
  Attaching to — or creating, then attaching to — the session for a path,
  recording it in **History**. The non-picker entry point
  (`pop project switch <dir>`), used by external tooling (e.g. worktree-creation
  scripts) so out-of-band paths still land in **History**. It is one of several
  acts that record — no longer the defining one, since any handoff of a pane to
  the human records too.
  was: Attaching to — or creating, then attaching to — the session for a path,
    recording it in **History**. The non-picker entry point
    (`pop project switch <dir>`), used by external tooling (e.g.
    worktree-creation scripts) so out-of-band paths still land in **History**.

~ Session nesting
  The **Project picker**'s display-only grouping of a project's non-trunk
  live sessions as a second level under the project row, opt-in via
  `[project] worktree_display = "flat" | "nested"` (default `flat`,
  permanently). No tmux session is ever renamed and no path changes — only the
  rendering, which drops the `<project>/` prefix on a nested row and trails a
  project holding nested sessions with `▸`/`▾`. The two modes deliberately
  list different rows: flat shows every worktree, nested only those with a live
  session. Membership is sessions, not checkouts — a **Map session** nests
  alongside the worktrees — so the level answers "what can I attach to under
  this project". **Expanding moves the cursor to the group's last child**, which
  scrolls the whole group into view and gives every child a quick-access digit;
  the parent may scroll off the top of a group taller than the viewport, and
  `left` is the way back to it. Collapsing keeps the rows below the group on
  their screen lines, landing the parent where its last visible child sat.
  avoid: worktree nesting, worktree tree, session grouping, nested picker
  was: the same entry as minted in `.grill-context/CONTEXT.0017.4E1B7A62.md`,
    before expansion had a defined scroll behaviour.
