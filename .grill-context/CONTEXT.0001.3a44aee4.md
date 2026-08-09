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
