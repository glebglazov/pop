---
status: accepted
relates: "supersedes [ADR-0187](0187-attended-agent-arguments-are-per-preset-defaults-under-an-agents-config-root.md), which put attended arguments and model under [agents.<preset>]; keeps that ADR's one-chokepoint rule and its attended exception to [ADR-0017](0017-agent-preset-augmentation-rides-on-agent-flag.md); rides on the entry type of [ADR-0194](0194-agent-lists-are-grouped-by-kind-of-work-and-attended-is-one-of-the-groups.md)"
---

# An attended entry owns its whole invocation

## Context

ADR-0187 fixed a real accident — a `--model` tuned for unattended drains was
silently steering every interactive gate session — by cutting attended
resolution down to the *preset name* and moving arguments and model into
`[agents.<preset>].attended_args` / `.attended_model`. That was right when
attended sessions had no list of their own.

[ADR-0194](0194-agent-lists-are-grouped-by-kind-of-work-and-attended-is-one-of-the-groups.md)
gives them one, and the settings no longer fit. `[agents.<preset>]` is keyed by
preset, so it can hold exactly *one* attended configuration per agent: two
entries both naming `claude` — a cheap one and an expensive one, which is the
first thing a human wants — cannot differ. And with a model now nameable in an
entry, `attended_model` would be a second place to set one thing.

## Decision

1. **The entry's `cmd` is the invocation.** `cmd = "claude --model opus"` —
   first token selects the adapter, the remainder is passed through
   uninterpreted. The ADR-0187 cut to the bare preset name is reversed *for
   entries only*: what it protected against was args borrowed from a list built
   for drains, and an attended entry is not borrowed from anywhere.
2. **`--model` is parsed out of `cmd` for display, not for behaviour.** Pop
   reads it so a picker and a launch line can name the model, and passes it
   through unchanged. Every other argument is opaque to pop.
3. **`[agents.<preset>].attended_args` and `.attended_model` are removed** — a
   hard cut, no read-compat. The `[agents]` root survives, holding `output`
   (see ADR-0194 decision 4).
4. **A preset's declared attended arguments are appended only where `cmd` does
   not already name that flag.** Each adapter declares at most two — claude
   `--permission-mode auto`, cursor `--force --trust`, codex
   `--dangerously-bypass-approvals-and-sandbox`, nothing for opencode and kimi —
   so the check is an exact flag-name match, not a heuristic. `cmd = "claude"`
   keeps auto-permission; `cmd = "claude --permission-mode plan"` wins. This
   preserves ADR-0187's principle — the human at the terminal owns their
   permission posture — without a config key dedicated to it.
5. **Argument order at the chokepoint is: binary, `cmd`'s arguments, the
   preset's un-named declared arguments, then the prompt as final positional**
   (clipboard for kimi, which has no positional-prompt form, per ADR-0164).
   A `cmd` must therefore not end in a flag awaiting a value.
6. **An attended launch skips entries it knows are spent.** At launch pop takes
   the first entry whose preset has no active quota cooldown and whose binary is
   on PATH, and says in the launch line what it skipped and why. It reads the
   same machine-global cooldown rows drains write. It cannot do more: an
   attended agent reports quota exhaustion inside its own TUI, which pop never
   parses, so mid-session switching is impossible and is not attempted.
7. **All of it stays at `ResolveAgentAssistanceInvocation`**, the one chokepoint
   every attended call site already passes through. Unchanged from ADR-0187, and
   the reason this ADR is a change of inputs rather than a change of shape.

## Consequences

- Two entries naming the same preset with different models become expressible,
  which is the case that motivated the change.
- Attended argument handling loses its "replace wholesale" escape hatch: you can
  override a declared flag by naming it, but you cannot remove one that takes no
  contrary value. No preset currently declares such a flag; if one appears, this
  decision needs revisiting rather than a new config key.
- Decision 6 means an attended launch reads the cooldown store, a store read
  attended sessions did not previously perform.
- Anyone with `[agents.claude].attended_model` set loses it on upgrade with no
  alias. It reappears as `cmd = "claude --model <value>"` in
  `[work.attended].agents`.
