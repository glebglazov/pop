---
status: accepted
---

# Tasks declare effort; pop resolves it to a model per agent

A task entry in the **Manifest** may carry an optional `effort` key — `light`, `standard`, or `heavy` — that names *how strong a model* the task wants, independent of *which agent* runs it. This is the **model axis**, orthogonal to the agent axis that **Task agent** (ADR-0018) and **Queue agent fallback** already own. pop resolves `effort` to a concrete `--model` for whichever agent was chosen, by consulting that agent's **effort ladder**: a per-tier ordered list of models. pop ships a built-in ladder for `claude` only; every other agent has no built-in ladder and is configured by the user in `config.toml` under `[effort.<agent>]`, which may also override `claude` by full replacement. For the model axis the precedence is: an explicit `--model` (in a `--agent` flag or a **Task agent** pin) wins, then the `effort`-resolved model, then the agent's plain default. `effort` never selects an agent. An absent `effort` key means `standard`; an unknown token is a contract fault that makes the Task set **Malformed**, mirroring ADR-0018. When the chosen agent has no ladder, `effort` is a graceful no-op and the agent runs its own default model.

## Why

The "which model fits this task" judgment splits cleanly into two questions a planner conflates: *what tool* and *how strong*. The tool is already decided elsewhere — a Task agent pin or the Queue's rotating agent default — and depends on quotas, PATH availability, and account access. The strength is a durable, account-independent property of the *task itself*: a heavy refactor wants the strongest model wherever it runs, a mechanical edit wants the cheapest. Encoding strength as an abstract `effort` keeps the Manifest a **durable, replayable definition** in a way a concrete `claude --model opus4.8` cannot — `effort: heavy` never ages, while a pinned model id rots (ADR-0019). The volatile model specifics live in the ladder, not in every task.

Putting the resolution policy in pop rather than in a planning skill gives the opinion one home: every planner (to-tasks, to-prd, a future one) and every hand-written Manifest get the same `effort → model` mapping for free, instead of each skill re-baking a model opinion that drifts.

Built-in for `claude` only because only `claude`'s aliases auto-resolve to the latest model (ADR-0019); every other agent's models are pinned, version-specific, and — critically — **account-specific**: whether an opencode subscription can reach an opus-class model varies per user. pop shipping a universal cross-agent opinion would be inventing a model catalog it cannot stand behind, which ADR-0019 explicitly refuses. So pop commits only to the one durable, account-independent ladder and delegates the rest to user config. This deliberately un-defers the "user-config layer of curated models" ADR-0019 left for later; per-account cross-agent model access is the real need that deferral was waiting for.

Head-of-list resolution, not a pre-flight fallback chain, because ADR-0019 removed any model-availability listing: pop cannot know a *model* is absent before running it (only the *agent binary*'s PATH presence is checkable, and that is the agent axis). The ladder is still an ordered list so a later **runtime** fallback — retry down the list when an attempt fails on an unknown or unavailable model — can be added without a schema change; for now pop uses the head and the tail is reserved.

## Considered Options

- **A planning skill writes a concrete agent string per task** (e.g. `claude --model opus`). Rejected: scatters the model opinion across every planner, bakes rot-prone pinned ids into durable Manifests, and gives a user no single place to override.
- **pop ships a built-in cross-agent opinion** (e.g. `heavy → opencode/kimi`). Rejected: pop cannot honestly opine on per-account non-`claude` model access; contradicts ADR-0019's "never invent a model catalog."
- **`effort` selects the agent as well as the model.** Rejected: the agent axis is already owned by Task agent pins and Queue agent fallback; `effort` is strictly the model axis and composes with whatever agent those choose.
- **One model per tier instead of an ordered list.** Rejected: the ordered list reserves the deferred runtime fallback and lets user config express preference order; head-of-list today, fallback later.
- **A pre-flight presence check with hard-error when no model is present.** Rejected: ADR-0019 removed model listing, so there is no presence signal; head-of-list with a graceful no-op for an unconfigured agent is the honest behaviour.
- **Merge user config with the built-in `claude` ladder tier-by-tier.** Rejected: full replacement keeps one source of truth per configured agent and a resolution that is trivial to read and display.

## Consequences

This amends ADR-0019: `claude`'s **Curated model aliases** gain tier structure to feed its built-in effort ladder, and the curated list keeps its advisory role for filling an explicit `--model` while the effort ladder takes on the new *resolution* role. It relates to ADR-0018: `effort` is the abstract sibling of the concrete `agent` key, and an explicit model always wins over it. The embedded planning skills (ADR-0009) — `to-tasks` first — write `effort` on the default path and stay agent-agnostic, reserving the concrete `agent` key for when a human asks for a specific agent.

The built-in `claude` ladder ships in code and is overridable; users own the maintenance of any other-agent ladders they configure, matching the per-account reality. Runtime fallback down the ladder is a noted future extension; v1 resolves to the head. `pop tasks agents` renders each agent's resolved effort ladder so the otherwise-internal opinion is visible, with built-in-default versus configured provenance.
