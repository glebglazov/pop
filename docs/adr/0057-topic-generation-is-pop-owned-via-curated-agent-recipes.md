---
status: accepted
---

# Topic generation is pop-owned via curated agent recipes

pop now derives a pane's **Topic** itself, by orchestrating curated **Topic recipes** — named invocations of agent CLIs (e.g. `claude -p --output-format json`, a local `ollama` model) configured as an ordered list like `topic_agents = ["claude", "ollama:llama3.2"]`. pop tries each recipe in order and uses the first non-empty result, builds the model prompt, and normalizes the output to a lowercase kebab slug (≤5 words, one `topic_words` knob). This **supersedes the pipe model of [ADR-0024](0024-pop-stays-a-pipe-for-topic-derivation.md)**: a config-free `topic_command` that every user had to author by hand turned out to be too complex to configure, so the feature went unused. pop now owns the recipes, the prompt, and the normalization — but still links **no model SDK and holds no API keys**: each recipe shells out to a CLI the user has already authenticated, so credentials and cost stay outside pop (the principle ADR-0024 got right is preserved; only "the user writes the command string" is dropped). This mirrors how pop already ships curated model aliases per preset ([ADR-0019](0019-pop-ships-curated-model-aliases-per-preset.md)).

## Considered Options

- **Keep ADR-0024's pure pipe + ship a recipe users paste in.** Rejected: still leaves an opaque shell string the user must trust and maintain, and the fallback chain is their own `||` plumbing — the exact friction that killed adoption.
- **Script builder that generates a `topic_command` into config.** Rejected: the user still ends up owning a script they didn't write; pop owning curated recipes is simpler to reason about and to extend.
- **Link a model SDK and manage keys for a turnkey call.** Rejected: couples pop to a provider, forces key management into a tool that makes zero model calls of its own, and excludes local-model users — the same reasons ADR-0024 cited.
- **Reason-aware fallback with a cross-pane usage-limit cooldown** (reusing the [ADR-0034](0034-reset-aware-agent-cooldown.md) cooldown pattern). Deferred: Claude's `-p` JSON exposes no documented, stable signal that distinguishes an account usage-limit from a generic error, so the cooldown would hang on an undocumented, per-agent shape. Fallback is **reason-blind** — any nonzero exit / error / empty / timeout → next recipe. Structured JSON is used only for clean extraction of the result text, not for branching on the error; the signal sits in the payload for the day a stable cooldown is worth building.

## Consequences

- pop makes no outbound network calls of its own and stores no credentials; all model cost and auth live in the CLIs the recipes invoke.
- Recipes carry per-agent output-format knowledge (the JSON shape to parse) inside pop — acceptable now that pop owns the recipe, but each new agent recipe is a small maintenance surface.
- If every recipe fails and there is no prior Topic, pop falls back to truncating the prompt into a slug — a Topic always resolves, never blocks the agent.
- The Topic format (kebab slug) is now a pop-owned contract applied uniformly to recipe output **and** pre-seeded sources (see [ADR-0058](0058-topic-lives-in-the-pop-topic-tmux-user-option.md)).
