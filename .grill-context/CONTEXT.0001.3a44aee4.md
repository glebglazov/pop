---
fragment: 3a44aee4
generation: 0001
branch: master
---

+ Attended launch-time skip
  The one budget-awareness an attended session has: at launch pop takes the first
  entry of the attended agent list whose preset has no active quota cooldown and
  whose binary is on PATH, and names in the launch line what it skipped and why.
  Unlike **Agent fallback** it cannot switch mid-session — an attended agent's
  quota exhaustion is reported inside its own TUI, which pop never parses — so
  this is a pre-flight read of the cooldown rows drains write, not a fall-through.
  avoid: attended fallback, attended agent rotation
  under: Agents

+ Work agent group
  One kind of work's own ordered agent list, under `[work.<group>].agents`:
  `implement`, `verify` and `routine` each name the agents that kind of
  unattended work walks, and `attended` names the agents every human-facing
  session shares — gate assistance, an **Assist session**, **Map assist**, map
  grilling, a **Routine refinement session**. Groups are kind-scoped by design:
  a setting only one kind reads lives in that kind's table rather than at the
  `[work]` root, so `max_tries` and the attempt-retry schedule are declared once
  per kind that retries (implement, verify) and the routine group carries only
  its list. Replaces the pre-cut `[tasks.implement]`, `[tasks.verify]` and
  `[routines]` lists.
  avoid: agent role, attended role, headless group, [tasks.implement].agents
  under: Agents

+ Attended agent entry
  One row of a **Work agent group**: `{ display_name, cmd }`, where `cmd` is the
  whole invocation the entry stands for (`claude --model opus`) and
  `display_name` is what a picker and a log line call it. A bare string is sugar
  for `{ cmd = "<string>" }`, the same string-or-table decoding
  `[pane_monitoring].topic_agents` already accepts. The entry is where a model
  is named: pop parses `--model` out of `cmd` so a picker can render it, passes
  every other argument through uninterpreted, and appends the preset's
  **Attended argument defaults** only where `cmd` does not already name that
  flag. There is no separate per-preset attended model or argument key.
  avoid: agent spec, preset spec, attended preset, extra_args
  under: Agents

~ Agent fallback
  The unattended agent-choosing policy of a **Work agent group**: each kind
  takes an ordered list — repeated `--agent` flags, else its group's
  `[work.<group>].agents`, else the built-in `claude` — and runs on the first
  live entry, falling through on any preset-scoped **Agent proceed verdict**: a
  quota pause, an **Agent authentication failure**, or a missing binary. Never
  on ordinary task failure. A machine-global cooldown store records quota pauses
  per preset; a human-healing verdict writes no cooldown. When every entry is
  human-healing unavailable, implement exits `ExitSetup` and the task stays
  Open. Verify's list falls through on one more class — a preset's exhausted
  retry loop — an asymmetry with implement that is deliberate and unresolved.
  Attended sessions have no such policy: they cannot switch mid-session and get
  an **Attended launch-time skip** instead.
  was: The same policy, but keyed to `[tasks.implement].agents` and
    `[tasks.verify].agents` as the two lists, described as owned by `pop tasks
    implement` rather than by a group, and ending "When attended Integration
    conflict assistance needs an agent, it uses only the first entry of the
    implement list" — the coupling this session removed.

+ Agent override
  A session-lived promotion of one **Attended agent entry** to the head of its
  **Work agent group**, opened with `alt+a` from any gate menu or dashboard page
  and applied for one OS process — a dashboard, an **Assist session**, a drain —
  never persisted. It promotes rather than pins: the configured remainder of the
  group stays behind the picked entry, so an unattended group keeps the ordering
  its **Agent fallback** depends on. The picker is two numeric levels, group then
  entry, `0` back and `Enter` exiting unchanged, and it is inert inside another
  picker because `alt` is already the quick-access modifier there.
  avoid: agent pin, model switch, session agent, per-row agent
  under: Agents

~ Interactive agent preset
  A named attended-assistance command known to an Agent adapter, separate from
  an **Agent preset** because assisting a human is an attended conversation
  rather than a headless attempt. Its invocation comes from an **Attended agent
  entry**'s `cmd`, plus the preset's **Attended argument defaults** appended only
  where that `cmd` does not already name the same flag, plus the generated
  briefing as the final positional argument. kimi's interactive mode accepts no
  initial-prompt argument, so its launch is the bare binary and the briefing is
  delivered on the clipboard for the human to paste.
  was: The same term, but with the invocation sourced from the preset name alone
    plus `[agents.<preset>].attended_args` (replacing the declared list
    wholesale) and `.attended_model` (the only way a model could be named) —
    both retired with the entry's cmd.

~ Attended argument defaults
  The per-**Agent preset** argument list pop adds to an attended agent session,
  each preset declaring the least-restrictive posture that agent offers: claude
  `--permission-mode auto`, whose classifier allows ordinary in-repo work and
  asks about the rest; cursor `--force --trust` and codex
  `--dangerously-bypass-approvals-and-sandbox`, which bypass outright; opencode
  and kimi none. The posture is deliberately not uniform — only claude can
  mediate rather than bypass, and pop prefers mediation where it exists. They
  are defaults, not pop-owned flags: an **Attended agent entry** overrides one by
  naming that flag in its `cmd`, which is how the human at the terminal keeps
  the last word on their own permission posture without a config key for it.
  was: The same list, but overridden by replacing it wholesale through the
    Agents config root's `attended_args`.

~ Agents config root
  `[agents.<preset>]` — the home for agent settings keyed by *preset* rather
  than by kind of work, as against the kind-keyed **Work agent group**s beside
  it. It holds `output`, the **Agent output mode** governing how pop parses that
  preset's stream, which means the same thing to every kind that runs the agent.
  avoid: [tasks.presets], per-verb agent flags, attended settings
