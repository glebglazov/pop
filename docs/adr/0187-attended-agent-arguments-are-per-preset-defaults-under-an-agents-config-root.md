---
status: accepted
relates: "carves an attended-only exception out of the flags-come-last rule of [ADR-0017](0017-agent-preset-augmentation-rides-on-agent-flag.md), adds a capability in the shape [ADR-0166](0166-invocation-shape-capabilities-move-onto-the-preset-spec.md) established, and picks a config root beside the verb sub-tables of [ADR-0092](0092-task-config-parented-under-tasks-with-verb-named-sub-tables.md)"
---

# Attended agent arguments are per-preset defaults under an `[agents]` config root

## Context

Pop's headless invocations all run auto-approved: every preset's `headlessPrefix`
carries its own flag — claude `--dangerously-skip-permissions`, cursor
`--force --trust`, codex `--dangerously-bypass-approvals-and-sandbox`, and kimi's
`-p`, which *is* its auto-permission and which rejects `--yolo`/`--auto`
outright. The attended shape has none: every preset's `assistance.Command` is the
bare binary with empty `Args`, so a **HITL assistance session**, an **Assist
session**, a **Map assist** pane, map grilling and a **Routine refinement
session** all launch an agent that stops to ask permission for every edit — in a
session pop opened precisely so a human could get work done fast.

There was also an accident. `ResolveAgentAssistanceInvocation` parses the preset
spec from `[tasks.implement].agents[0]` and passes its extra arguments into the
attended command, so a `--model` chosen to tune *unattended drains* silently
steered every interactive gate session too. Nobody decided that.

No config key anywhere selects agent flags. The two per-agent tables that exist
(`[effort.<agent>]`, `[tasks.presets.<name>].output`) sit under different roots
and neither fits: attended sessions are launched by Maps and Routines as well as
Task sets, so `[tasks.…]` is the wrong parent for them.

## Decision

1. **Each adapter declares an attended argument default**, alongside its other
   declared capabilities, and auto-approval is what those defaults contain.
   opencode and kimi declare an empty list; with no flag of their own they launch
   unchanged rather than refusing, so the setting means the same thing to a user
   whichever preset they run.
2. **Those defaults are defaults, not pop-owned flags.** `[agents.<preset>]
   .attended_args` **replaces** the declared list wholesale. This is a deliberate
   exception to ADR-0017's rule that pop-owned flags come last and win: that rule
   exists to protect the *output protocol* pop must parse, and an attended session
   has no protocol to protect. If the human wants a different permission posture
   in a terminal they are sitting in front of, theirs is the last word.
3. **A new top-level `[agents.<preset>]` config root** holds `attended_args` and
   `attended_model`. It is includable (`include:"fields"`, and `agents` joins the
   hand-written include whitelist), merged map-first-wins per preset with
   per-field merge inside, so a machine-local include can set one preset's
   arguments without erasing another's. `[tasks.presets.<name>]` keeps `output`.
4. **Attended sessions take the preset name from `[tasks.implement].agents[0]`
   and nothing else from it.** The inherited-extra-args path is removed. An
   attended session names a model only if `attended_model` is set; otherwise pop
   passes no model flag and the agent's own configuration decides.
5. **One policy at one chokepoint.** All of it lands in
   `ResolveAgentAssistanceInvocation`, through which all twelve attended call
   sites already pass — the two assist verbs, map grilling, every HITL,
   verify-failed, failed, interrupt and fold-conflict gate, and routine
   authoring. Per-call-site permission modes were rejected: twelve places to
   forget is not a design.

## Consequences

- **Auto-approval becomes the default for interactive sessions pop opens.** A
  gate session will now edit files without asking. That is the point, and it is
  the same posture the drains beside it have always had — but it is a real change
  in what an attended agent may do unprompted, which is why it is written down
  here rather than left as a flag someone finds in a table.
- Anyone who set `[tasks.implement].agents = ["claude --model X"]` loses that
  model in their gate sessions. They get it back with
  `[agents.claude].attended_model = "X"`, and until they do, their own `claude`
  configuration decides — which is the behaviour a human at a terminal expects.
- The flags-come-last rule now has an audience-dependent exception. A reader of
  ADR-0017 alone would get the attended case wrong, so the **Agent preset**
  glossary entry carries the split explicitly: headless asserts, attended
  defaults.
- Two presets ignore the setting silently. `pop tasks agents` is where that
  should be visible; reporting it is not in this decision.
- The `spawn-agent` skill hardcodes `claude --dangerously-skip-permissions` in
  Markdown outside this repository, so the flag still lives in two places. Not
  addressed here — noted so the duplication is known rather than discovered.
