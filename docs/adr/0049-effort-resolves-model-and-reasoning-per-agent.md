---
status: accepted
relates: "amends ADR-0032"
---

# Effort resolves to a (model, reasoning) bundle per agent

**Effort** (ADR-0032) now resolves through an agent's **effort ladder** to a bundle of *both* a `--model` and a **reasoning effort** — the model's thinking level (claude `low…max`, codex `minimal…high`, etc.) — instead of a model alone. It stays a *single* user-facing knob: there is deliberately no second axis. Each ladder tier becomes an ordered array of `{ model, reasoning }` tables (reasoning optional), so the head sets which model runs and how hard it thinks, and each reserved fallback element carries its own reasoning. pop ships built-in ladders for `claude`, `codex`, `cursor`, and `pi` (all `high` reasoning on `claude`; varied per tier elsewhere); `opencode` and any other agent stay config-only. Reasoning is emitted per-adapter — `claude --effort`, `codex -c model_reasoning_effort=`, `cursor` folds `[effort=…]` into the `--model` token, `pi --thinking`, `opencode` none. Application rule: an absent `effort` injects nothing; a hand-pinned `--model` skips the whole bundle; otherwise the ladder's model and reasoning are injected, except a reasoning value the user already set in `--agent` args is kept.

## Why

Model strength and thinking effort are the same judgment from a planner's seat — "how much should this task cost?" — so collapsing them into one `effort` tier keeps the **Manifest** a durable, replayable definition with one strength dial rather than a control panel. A heavy task wants the strongest model *and* its hardest thinking; a light task wants the cheapest model thinking cheaply. The off-diagonal combos (cheap model thinking hard, strong model thinking cheap) are reachable today via the per-element `{ model, reasoning }` pairing in a tier and, if ever needed as a first-class user choice, are exactly what a second axis would add later — but they don't earn that complexity now.

An array of `{ model, reasoning }` pairs, not one bundle per tier, because the ladder tail is already the reserved runtime-fallback chain (ADR-0032): a fallback model may want a different reasoning than the head, so reasoning rides each element.

Reasoning is rendered per-adapter because the wire shape is wildly non-uniform: a flag (`claude`, `pi`), a config override (`codex`), or folded into the model token (`cursor`). There is no generic `--effort`, so each adapter owns both emitting reasoning and detecting a user-set reasoning to defer to.

## Considered Options

- **A separate user-facing reasoning axis** (a second Manifest key). Rejected for now: doubles the knobs to express a cross-product the operator does not yet need; the bundled tier covers the common case and the per-element pairing covers the rest. Reserved as the natural future extension if decoupled control is ever wanted.
- **Apply the tier's reasoning even when the user pins a different model.** Rejected: the tier's reasoning was curated *for the tier's model*; layering it onto a hand-picked model is presumptuous. A model pin steps outside the bundle, so we step back from all of it — hence skip-whole-bundle.
- **One `{ model, reasoning }` bundle per tier instead of an array.** Rejected: would force one reasoning across all fallback models and discard the ordered-fallback invariant ADR-0032 reserved.

## Consequences

This reverses ADR-0032's deliberate **claude-only** built-in stance, which kept non-`claude` agents config-only because their models are pinned, version- and account-specific, so an out-of-box default could reference a model an account cannot reach. We accept that reversal because built-in ladders are **overridable defaults, not commitments**: the pinned ids (`codex` gpt-5.x, `cursor` composer-2.5, `pi` opencode-go/*) are *this* operator's known subscription reality, pop is a personal tool with a known account, and any user whose subscription differs overrides `[effort.<agent>]` or removes it (→ no-op on the agent's own default). The trade is ADR-0032's always-works default for a useful default that may need one config override, with pinned-id rot as the named maintenance cost.

It amends ADR-0019/0032: the `codex`/`cursor`/`pi` curated catalogs now feed built-in effort ladders, not just `claude`'s, while staying advisory for filling an explicit `--model`. `pop tasks agents` continues to render each agent's resolved ladder; the reasoning column rides the same built-in-versus-configured provenance. The `EffortConfig` tier type changes from `[]string` to `[]EffortModel{ Model, Reasoning }`; no existing config uses `[effort.*]`, so there is no migration.
