---
status: deferred
---

# Adapters declare a proceed verdict, and effort tiers skip to the next model

> ⚠️ **DEFERRED — intended design, not shipped behavior.** Generalizes ADR-0153 (agent unavailability) by adding a *model* scope beneath its preset scope; consumes the ladder tail that ADR-0032 reserved and ADR-0049 carried forward; folds in the **Plan gate** of ADR-0164; leaves ADR-0043's preset ordering and ADR-0099's retry schedule intact.

## Context

An **Effort ladder** tier is an ordered array of `{ model, reasoning }` bundles, but only `bundles[0]` has ever been read (`tasks/agent.go:967`). ADR-0032 reserved the tail as a runtime fallback chain and ADR-0049 kept the shape; nothing consumes it, so a tier is in practice a single model wearing an array's clothes.

That tail becomes load-bearing the moment a preset can reach models it does not itself own. `cursor-agent` brokers other vendors' models — the catalog carries Anthropic, OpenAI, Google, xAI, Moonshot and Z.ai tokens beside cursor's own `composer-2.5` — and gates the brokered ones on a per-vendor allowance separate from cursor-native capacity. When that allowance is spent, today's pop has exactly one move: treat the preset as unavailable and hand the task to the next preset (ADR-0153), even though the same `cursor-agent`, same login, would happily run `composer-2.5` on the very next line of the tier.

The same shape already shipped once, at the wrong scope. ADR-0164's **Plan gate** is kimi answering HTTP 401 `does not have access to …` for a subscription-gated model — a statement about a *model*, answered by advancing the *preset*. It is not a second mechanism; it is this one with nowhere to go.

The mechanism the codebase reaches for by reflex would be a second orchestrator-side parser: teach the runner another provider string. That does not scale — every adapter's refusals are shaped differently, and the orchestrator has no business knowing what a cursor allowance message looks like.

## Decision

### The adapter answers, the orchestrator dispatches

Every **Agent adapter** reports an **Agent proceed verdict** on a result shape shared by all adapters: whether it can carry on, and if not, at what scope, what would heal it, and whether the attempt counts. The orchestrator never reads provider text. Fields:

- `Scope` — `Model` or `Preset`. *Preset* is the entry in the **Agent fallback** list: one adapter, one CLI, one login. *Model* is the `--model` token the tier resolved. `Scope=Preset` means this CLI can run nothing; `Scope=Model` means the CLI is healthy and this token is not.
- `Recovery` — `Time`, `Human`, or `Permanent`, as in ADR-0153.
- `ResetAt` — optional; present only when the adapter parsed a real instant.
- `ConsumesAttempt` — whether the **Task retry cap** is charged.

ADR-0153's `AgentUnavailability` becomes the `Scope=Preset` case of this verdict rather than a sibling type, for the reason ADR-0153 itself gave for preferring a parent over siblings: two types would state the dispatch rule twice and let them drift.

### Model scope skips inside the tier

A `Scope=Model` verdict triggers an **Effort model skip**: the skipped model is recorded, resolution re-runs, and the attempt **restarts on the next tier entry without consuming a try**. Nothing failed — the preset is fine and its remaining capability is untouched, so charging the **Task retry cap** would spend the task's budget on the environment's problem.

The skip list is the loop guard. Resolution filters recorded models out of the tier, so every restart strictly shortens the candidate list; a tier with no candidates left escalates to `Scope=Preset` and **Agent fallback** advances the preset as it does today. No separate restart counter exists, because a counter could only ever be a weaker restatement of "the list got shorter".

The ordering is inner-before-outer: a preset's own tier exhausts before its neighbours are asked. A tier is that preset's declared capability, and jumping presets while the current one still has a runnable model discards a configured resource for nothing.

### The skip list is durable with an expiry

Skips persist in a new SQLite table `agent_model_cooldowns(preset, model, until)`, machine-global — a vendor allowance is account-wide, not per-checkout or per-set. `until` comes from `ResetAt` when the adapter parsed one and from a **1 hour** default otherwise, reusing the parsed-instant-else-fallback policy `agentQuotaCooldownUntil` (`tasks/cooldown_store.go:141`) already implements for preset cooldowns. A `Permanent` recovery writes no expiry.

A separate table, not a column on `agent_cooldowns`: `blockedItemsFromAgentCooldowns` (`queue/run_output.go:337`) renders that map as blocked presets, and a spent model surfacing there would claim the preset is paused when it is running fine.

### Verify walks the same class

`[tasks.verify]` presets take the same verdict with the same dispatch. The Verifier's own asymmetry with implement (ADR-0153: it also advances on retry exhaustion) is untouched — this ADR adds a scope, not a policy.

### Surfaces

- A skipped attempt persists a **Captured run** with a `model_skipped` outcome. A run that leaves no row is a gap `pop tasks stream` cannot explain, which is the failure ADR-0153 called out for `agent_unusable`.
- `pop tasks agents` marks skipped ladder entries with their remaining time.
- The drain prints the existing dim `trying next` line, naming the model rather than the preset.
- The Work dashboard carries a footer one-liner, hidden when empty: `skipped: cursor/claude-opus-5-thinking-high 47m · kimi/k2.7-code-highspeed ∞`.

### Unchanged

A hand-pinned `--model` in `--agent` args still skips the whole bundle (ADR-0049), and therefore skips this too: a pin steps outside the ladder, so its exhaustion is a preset-scoped stop with no tier to walk.

## Considered options

- **A vendor-group skip key** — the adapter returns `cursor:anthropic` and resolution drops every matching entry, so one refusal retires the whole vendor. Rejected: it needs a token-to-vendor taxonomy inside each adapter, which is exactly the pinned-id rot ADR-0049 already names as a maintenance cost, and it buys little. A rejected model errors in well under a second with no agent tokens spent, and a skip consumes no attempt, so walking a tier's Anthropic entries one at a time costs a second or two and self-corrects into the skip list immediately. Reachable later without changing the verdict shape if a tier ever grows several same-vendor entries.
- **Keep the orchestrator parsing provider strings**, adding a case per agent. Rejected: it puts vendor-specific knowledge in the one place that must stay adapter-agnostic, and every new agent edits the runner.
- **In-run memo instead of a durable list** (the ADR-0153 posture for auth). Rejected: an allowance is time-healing and account-wide, so the next process would rediscover it by re-erroring on every fresh drain. Auth is different precisely because a human can fix it at any instant.
- **Durable with no expiry plus a manual clear verb.** Rejected: cursor's diagnostics carry no parseable reset (`quotaReset: CapabilityBlind`), so with no default TTL one exhausted afternoon pins the ladder to its tail indefinitely and waits on the operator to notice.
- **Charge the retry cap for a skip.** Rejected for ADR-0153's reason: nothing about the task failed, and the disposition belongs to the environment.
- **Keep Plan gate as its own concept.** Rejected: it is a model-scoped verdict with `Recovery=Permanent`, and one vocabulary for one verdict is the point.
- **Split into two ADRs** — accept the verdict generalization now, defer the skip. Rejected: half-built, the vocabulary straddles two models, which is the state ADR-0153 refused for the same reason.

## Consequences

- `Scope=Preset` reaches the existing preset cooldown store and recovery wait untouched; `Scope=Model` reaches neither. The recovery wait stays impossible to enter without a reset instant.
- **Plan gate** retires as a term once this ships; kimi's 401 becomes a `Scope=Model`, `Recovery=Permanent` verdict, and kimi's light tier gains a tail to fall into instead of surrendering the preset.
- Cursor's spent-allowance wire shape is **unconfirmed** — no captured sample exists yet, which is what the operator's all-Anthropic `[effort.cursor]` heads are staged to produce. As in ADR-0153, an unrecognised refusal degrades to exactly today's behaviour, so shipping ahead of the sample is safe but the cursor detector stays a stub until one lands.
- The ladder tail stops being decorative. Existing configs whose tails were written as documentation now execute, which is the intended reading of ADR-0032 but a behaviour change for anyone who wrote a tail expecting inertness.
