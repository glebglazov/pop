---
status: accepted
---

# The kimi adapter ships hardcoded moonshot-ai aliases, env-var reasoning, and a plan-gate fall-through trigger

## Context

pop gains a sixth **Agent preset**, `kimi`, for the kimi-code CLI — full adapter parity (structured output, live rendering, quota detection, attended assistance) plus monitor integration. kimi-code's shape breaks three assumptions the existing five presets share, and each break forced a choice:

1. **Model aliases are exact, user-config-defined keys.** `--model` resolves by literal config key only — no bare model names, no suffix matching — and the key prefix varies per installation (`moonshot-ai/kimi-k3` on a standard OAuth login vs `kimi-code/k3` in the managed-service docs). A built-in **Effort ladder** must name *something*.
2. **There is no reasoning CLI flag.** Thinking effort is settable per-invocation only via the `KIMI_MODEL_THINKING_EFFORT` environment variable (works headless, honored by the standard managed provider, and deliberately bypasses kimi's client-side `support_efforts` validation — an unsupported value is a server 400, verified live: `medium` is rejected for `kimi-k3`, whose only rungs are `low`/`high`/`max`). Every other adapter renders **Reasoning effort** as arguments.
3. **The light tier's natural model is subscription-gated.** `kimi-k2.7-code-highspeed` 401s with `does not have access to …` on plans that lack it — a permanent, deterministic incapacity that was neither an **Agent quota pause** nor a missing binary, the only two fall-through triggers **Agent fallback** had.

Also shaping the adapter: kimi's prompt rides as the `-p` flag *value* (no positional form), `-p` is auto-permission by design and rejects `--yolo`/`--auto`, its stream-json carries only assistant/tool/meta-retry lines (no init/model/result events, no thinking — so **Actual model** is absent and failure is exit-code-plus-stderr), and its interactive TUI accepts no initial prompt.

## Decision

- **Built-in ladder with hardcoded `moonshot-ai/` aliases**: heavy `moonshot-ai/kimi-k3`@high, standard `moonshot-ai/kimi-k3`@low, light `moonshot-ai/kimi-k2.7-code-highspeed` (model-only). Installs whose alias keys differ override wholesale through `[effort.kimi]` — the existing replace-the-built-in mechanism is the escape hatch.
- **Reasoning effort rides the environment.** `AgentInvocation` gains env support; kimi's ladder reasoning renders as `KIMI_MODEL_THINKING_EFFORT=<level>`. A value already set in pop's own environment counts as hand-set and wins, mirroring the hand-set-reasoning rule for args. `KIMI_CODE_NO_AUTO_UPDATE=1` rides the same channel. Ladder entries pair only efforts the model declares, because the env channel bypasses kimi's validation.
- **A plan gate is a third Agent fallback trigger.** kimi's `does not have access to …` 401 falls through to the next preset like a quota pause but records no cooldown — the gate is deterministic per account+model, so re-probing costs one failed attempt and reset-time semantics don't apply.
- **Assistance delivers the briefing by clipboard.** kimi's attended launch is the bare binary; the generated prompt is copied (tmux buffer / OSC 52) for the human to paste.
- **Integration wires `[[hooks]]` into `~/.kimi-code/config.toml`** (SessionStart / Stop / Notification / PostToolUse, same event vocabulary as claude's wiring); skills install to `$KIMI_CODE_HOME/skills/`.
- **Quota detection matches three stderr substrings** — `usage limit for this period` (1h backoff), `usage limit for this billing cycle` (1 day), `monthly usage limit` (7 days), each plus the **Quota assurance offset** — and deliberately ignores transient overload/concurrency 429s, which kimi retries internally first.

## Considered Options

- **Wire-id ladder with live alias resolution** (read `kimi provider list --json` at resolution time, match on the wire `model` field). The robust answer to per-installation aliases; rejected as a new resolution mechanic no other adapter has — a subprocess call on the task-dispatch path with its own failure modes — and because that output leaks API keys in plaintext, so every consumer must parse it defensively. The hardcoded form fails loudly and the config override already exists.
- **Config-only ladder** (like opencode). Rejected: effort would be dead out of the box on the standard login, where the baked aliases are exactly right.
- **Model-only kimi ladder** (no reasoning channel). Rejected: it would make half the ADR-0049 bundle inert for kimi — an **Effort** tier would pick the model but not how hard it thinks.
- **Treating the plan-gate 401 as an ordinary failure.** Rejected: with `agents = [kimi, claude]`, every light-tier task on a gated plan would burn the retry loop and park at the Failed gate while a working preset sits unused.
- **Shipping a kimi plugin** (`kimi.plugin.json` contributing hooks + skills) for the integration. Rejected: a distribution mechanism pop produces for no other agent; hook entries in `config.toml` match how pop already merges claude's `settings.json`.
- **All-k3 ladder** (heavy@max, standard@high, light@low). Superseded during design review in favour of lighter usage: `@high` is heavy enough for the top tier, and `medium` does not exist on k3's wire (verified against the live provider).

## Consequences

- `AgentInvocation` grows an env carrier and the runner must merge it — the first adapter capability that arguments cannot express. Other adapters are unaffected; env is empty for them.
- **Agent fallback**'s contract widens beyond quota/missing-binary for the first time; the **Plan gate** glossary term pins the narrow shape (agent-reported, deterministic, no cooldown) so future triggers don't ride this precedent casually.
- kimi's baked aliases are install-dependent like codex/cursor/pi's — surfaced as such in `pop tasks agents`; `kimi provider list --json` is never consumed by pop.
- The `-p` stay-alive semantics (`print_background_mode = "steer"`) mean a kimi attempt can linger past its main turn when the agent leaves background tasks pending; pop's attempt timeout is the backstop, and no per-invocation override exists to do better.
