---
fragment: 7B9D3C7A
generation: 0021
branch: master
---

~ Attended argument defaults
  The per-**Agent preset** argument list pop passes to an attended agent session —
  a **HITL assistance session**, an **Assist session**, **Map assist**, map
  grilling, a **Routine refinement session**, and every gate that launches an
  agent. Each preset declares its own, and what they contain is the
  least-restrictive posture that agent offers: claude `--permission-mode auto`,
  whose classifier allows ordinary in-repo work and asks about the rest; cursor
  `--force --trust` and codex `--dangerously-bypass-approvals-and-sandbox`, which
  bypass permission checks outright; opencode and kimi none (kimi's
  auto-permission *is* its headless `-p`, and it rejects `--yolo`/`--auto`). The
  posture is therefore *not* uniform across presets — only claude can mediate
  rather than bypass, and pop prefers mediation where it exists. They are
  **defaults, not pop-owned flags**: `[agents.<preset>].attended_args` replaces
  the list wholesale instead of being overridden by it, the one deliberate
  exception to the flags-come-last rule of **Agent preset**. A preset with no such
  argument passes nothing rather than refusing, so the setting is offered
  uniformly even where the posture is not.
  avoid: yolo mode, skip-permissions flag, assist args
  was: The per-**Agent preset** argument list pop passes to an attended agent
    session — a **HITL assistance session**, an **Assist session**, **Map assist**,
    map grilling, a **Routine refinement session**, and every gate that launches an
    agent. Each preset declares its own, and auto-approval is the default: claude
    `--dangerously-skip-permissions`, cursor `--force --trust`, codex
    `--dangerously-bypass-approvals-and-sandbox`, opencode and kimi none (kimi's
    auto-permission *is* its headless `-p`, and it rejects `--yolo`/`--auto`). They
    are **defaults, not pop-owned flags**: `[agents.<preset>].attended_args`
    replaces the list wholesale instead of being overridden by it, the one
    deliberate exception to the flags-come-last rule of **Agent preset**. A preset
    with no auto-approval flag of its own passes nothing rather than refusing, so
    the option is uniform across presets that cannot honour it.
  under: Agents

~ Interactive agent preset
  A named attended-assistance command known to an Agent adapter. It is separate
  from an Agent preset because assisting a human at a HITL gate is an attended
  conversation, not a headless task attempt; custom headless agent commands do not
  imply an interactive preset. Every supported preset launches its own interactive
  binary under its **Attended argument defaults** — claude's mediated
  `--permission-mode auto`, cursor's and codex's outright bypass, nothing for
  opencode, pi and kimi. Those arguments come from the adapter and the user's
  `[agents.<preset>]` block — `attended_args` replaces them wholesale and
  `attended_model` is the only way a model is named — and never from the extra
  arguments of an **Agent preset** spec. kimi's interactive mode accepts no
  initial-prompt argument, so its attended launch is the bare binary and the
  generated briefing is delivered on the clipboard for the human to paste.
  was: A named attended-assistance command known to an Agent adapter. It is
    separate from an Agent preset because assisting a human at a HITL gate is an
    attended conversation, not a headless task attempt; custom headless agent
    commands do not imply an interactive preset. Every supported preset launches
    its own interactive binary, auto-approved by the preset's own declared attended
    arguments (claude `--dangerously-skip-permissions`, cursor `--force --trust`,
    codex `--dangerously-bypass-approvals-and-sandbox`; opencode, pi and kimi have
    none and launch bare). Those arguments come from the adapter and the user's
    `[agents.<preset>]` block — `attended_args` replaces them wholesale and
    `attended_model` is the only way a model is named — and never from the extra
    arguments of an **Agent preset** spec. kimi's interactive mode accepts no
    initial-prompt argument, so its attended launch is the bare binary and the
    generated briefing is delivered on the clipboard for the human to paste.
  under: Agents
