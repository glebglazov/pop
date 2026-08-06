---
status: accepted
relates: "adds two capabilities in the shape [ADR-0166](0166-invocation-shape-capabilities-move-onto-the-preset-spec.md) and [ADR-0165](0165-stream-shape-capabilities-are-declared-and-fixture-backed.md) established, carves a second exception out of the flags-come-last rule of [ADR-0017](0017-agent-preset-augmentation-rides-on-agent-flag.md) after [ADR-0187](0187-attended-agent-arguments-are-per-preset-defaults-under-an-agents-config-root.md), and is configured through [ADR-0191](0191-repo-scoped-settings-pop-writes-live-in-an-identity-keyed-runtime-layer.md)"
---

# A turn cap bounds one implementation attempt, and only claude can enforce it

## Context

An implementation attempt is bounded by wall clock only: `--timeout`, default
45m, enforced pop-side as `time.After` plus a process-group SIGKILL
(`tasks/attempts.go:585`). Wall clock is a poor bound on runaway iteration,
because how much a runaway costs depends on the model, not on the minute count —
a fast model can burn two hundred model calls in twelve minutes while a slow one
manages fifteen in forty-five.

Pop already counts the right quantity. **Turn** is a shipped glossary term and a
declared per-adapter capability: "one model message in a **Captured run** — a
single call to the model, regardless of how many tool invocations it carries",
with **Turn-blind run** for adapters that cannot report it. The **Spend lens**
and its `suspect:` markers are built on it.

**"Turn" does not generalize across agent CLIs, but the quantity does.** Measured
2026-08-06 against installed binaries where available:

| preset | mechanism | unit | on exhaustion |
|---|---|---|---|
| claude | `--max-turns`, **print mode only** | one assistant inference pass; tool results are free | error, exit 1, `subtype: error_max_turns`, `terminal_reason: max_turns` |
| kimi | `loop_control.max_steps_per_turn`, **config file only** | one LLM call | exit 1, `loop.max_steps_exceeded` on **stderr**; nothing in `stream-json` |
| opencode | `steps`, **config file only**, per-agent | agentic iteration | graceful: forced text-only summary, success exit |
| cursor | none | — | — |
| codex | none; a "turn" there is one HTTP request containing many iterations | — | — |
| pi | none, and no token or dollar budget either | — | — |

Two facts from that table did the deciding. First, claude's "assistant inference
pass" and kimi's "step" are the *same unit*, and it is verbatim pop's **Turn** —
so the number pop would set is the number pop already measures. Second, only
claude accepts it in argv. The `--max-steps-per-turn` flag documented for kimi
belongs to `kimi-cli`, a legacy product; the installed `kimi-code` 0.33.0 has no
such flag, and its config key was confirmed to work only by pointing the binary
at a synthetic `KIMI_CODE_HOME`.

## Decision

1. **A Turn cap bounds turns within one implementation attempt.** Not attempts
   (`max_tries` already does that), not a **Drain**, not an **Implement run**. Of
   pop's four nested loops it binds the innermost, because that is the only one
   an agent's own flag can bind.
2. **Implement only.** The **Verifier** runs uncapped even in a repository that
   declares a bound, though it is a claude headless run that would accept the
   flag. A Verifier reads and judges where an Implementer builds, and one number
   cannot mean both; a second per-verb number was rejected as surface ahead of
   need.
3. **Two capabilities, not one.** *Enforcement* (can this adapter be told to cap
   turns, and with which flag) is invocation-shape and carries no fixture, per
   ADR-0166. *Exhaustion recognition* (can pop tell that a run ended at its cap)
   is stream-shape and is fixture-backed, per ADR-0165. They are separate because
   the answers differ per adapter — kimi would accept a cap whose exhaustion
   appears only on stderr, which is neither capability's seam.
4. **Argv only. Pop never writes another agent's configuration file.** claude
   declares enforcement Supported; opencode and kimi declare it Blind with a
   reason naming their config key, so a human can set it themselves; cursor,
   codex and pi declare it Blind for want of any cap. Reaching kimi's key would
   mean pop synthesizing a whole `KIMI_CODE_HOME` — copying the user's providers,
   models and hooks in order to set one integer, and owning every drift between
   the copy and the original.
5. **A hand-set flag wins and pop then emits nothing.** `--agent "claude
   --max-turns 5"` is honoured as written, detect-and-defer exactly as
   `ArgsContainReasoning` already defers to a hand-set reasoning flag. This is a
   second, narrower exception to ADR-0017's flags-come-last rule: that rule
   protects the output protocol pop must parse, and a budget is not that.
6. **Exhaustion is its own attempt outcome, consuming a try and entering the
   retry carry-forward digest** — the inverse of **Effort model skip**, which
   consumes no attempt and stays out of the digest. An attempt cut short is
   exactly what the next attempt needs told. Recognition keys off claude's
   `subtype: error_max_turns` and its non-zero exit, never off a turn count.
7. **Turn counting is untouched, and the two numbers are not reconciled.**
   claude reports `num_turns: limit+1` on exhaustion — an undocumented off-by-one
   measured here. Pop keeps its own dedup count, because a **Captured run**'s
   Turn is pop's measurement.

## Considered Options

- **A dollar budget instead.** claude ships `--max-budget-usd`, print mode only,
  and it is the direct instrument for cost where a turn cap is a proxy. Rejected
  for now, and it is the honest next step: no other preset has any budget knob,
  so spend is *less* portable than turns, and turns are what pop already
  measures and reports. A wider "run bound" capability carrying either was
  rejected as abstraction ahead of need.
- **Pop enforces the cap itself**, counting turns live off the stream and killing
  the attempt. Rejected: pop's turn extraction runs post-hoc over a materialized
  event slice, so this would mean a new live-parsing kill path in which a
  mis-parse destroys good work.
- **Withhold the flag until every adapter can be recognised.** Rejected as the
  tail wagging the dog — under decision 4 pop sets no cap it cannot recognise
  anyway, since claude is both the only enforcer and fully recognisable.
- **A per-verb or per-effort-tier number.** Rejected; see consequences.

## Consequences

- **A cheap model on a lower Effort ladder tier is squeezed harder** than an
  expensive one, because one number applies to whichever model the tier resolves
  to. Accepted knowingly rather than dialled away.
- **Work already committed by a cap-exhausted attempt stays on disk**, confirmed
  by observation. That matches the timeout path, which also kills mid-flight.
- Five of six presets declare enforcement Blind. `pop tasks agents` is where that
  should be legible, and it is still the surface ADR-0187 left without an
  `attended_args` column — the same gap now twice over.
- The word "ralph" stays out of pop's vocabulary. It names a self-repeating outer
  loop in kimi's sense, which is the loop this decision does *not* cap, so
  importing it would name the wrong thing.
