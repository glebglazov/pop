# Agent preset augmentation rides on `--agent`, not a parsed `--agent-cmd`

Extra agent flags (e.g. `--agent "claude --model opus4.8"`) augment a recognized **Agent preset** on the `--agent` flag. Pop runs the supplied command as given — the same treatment as a **Custom agent command** — then appends the flags it owns (the output protocol governed by **Agent output mode**) followed by the generated prompt. Recognition of the first token as a known preset is the single discriminator that earns structured handling: stream parsing, **Agent quota detection**, and a **Captured attempt stream**. `--agent-cmd` stays the opaque, unrecognized, plain-output escape hatch.

## Why

The alternative — parsing `--agent-cmd` to detect a preset and reconcile its flags — splits one flag into two behaviors discriminated silently by whether word #1 matches a preset name, and forfeits shell features (pipes, `&&`) the instant a command happens to start with `claude`. Keeping augmentation on `--agent` leaves each flag one honest meaning: `--agent` = a recognized agent, structured; `--agent-cmd` = trust-me, plain. Recognition is the premise of `--agent`, not something inferred from a string.

Pop owns its protocol flags by **append-position**, not by policing the user's string. Because owned flags are appended last and CLIs are last-flag-wins, a user value for an owned flag (`--output-format text`) is overridden, not rejected — so "stream-json is exclusively ours" falls out with no dedup or reconciliation code. There is deliberately no way to augment an *unrecognized* agent and still get structured telemetry: Pop cannot parse a stream format it does not know, so that case routes to `--agent-cmd` (plain, not captured).

## Considered Options

- **Parse `--agent-cmd`, recognize the preset, reconcile owned flags.** Rejected: dual-mode flag keyed on the first token; loses shell features; overloads agent-cmd's opaque identity.
- **Reject or dedup user-supplied owned flags.** Rejected: append-last-wins makes owned flags authoritative for free; collision validation is code that buys nothing.
- **Add a separate repeatable `--agent-arg`.** Rejected: verbose, and keeping a pure-name `--agent` was judged cheaper to absorb in the glossary than the ergonomic cost.

## Consequences

Augmented extra arguments ride into native attended HITL assistance for the same agent but are dropped when assistance falls back to a different agent (e.g. cursor → claude), since they were written for the original. Last-flag-wins is not ironclad across every CLI; an agent that rejects duplicate flags instead of overriding is a per-adapter bug, not a reason to add general reconciliation.
