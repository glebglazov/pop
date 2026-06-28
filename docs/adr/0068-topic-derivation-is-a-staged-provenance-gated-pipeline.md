---
status: accepted
---

# Topic derivation is a staged, provenance-gated pipeline

`topic_agents` becomes an ordered list of typed steps instead of a flat list of agent reference strings. Each step is either a `truncate` step (cheap, local prompt-truncation — writes a provisional **seed**) or an `agent` step (a curated agent-CLI **Topic recipe** — writes a **final** Topic), and carries its own `set_if` guard (`empty` | `empty_or_seed` | `always`), optional appended `args`, and per-step `timeout`. A pane's Topic now carries **provenance** (`seed` | `final`) in a second tmux user-option `@pop_topic_kind` alongside `@pop_topic`; `set_if` is checked against it. The `truncate` step runs **synchronously** from the prompt hook for instant feedback; agent steps run **asynchronously**, queued on the pop daemon with per-pane single-flight (a newer prompt supersedes an in-flight derivation). The default config (when `topic_agents` is unset) is a single `truncate` / `set_if="empty"` step, matching today's truncation behaviour; there is **no hidden truncation fallback**, so the seed is the floor that agent steps only ever upgrade. A **Note** suppresses agent runs — it outranks the Topic in display, so re-deriving it would be invisible work.

This **supersedes [ADR-0025](0025-topic-command-derives-once-per-pane.md)** (once-per-pane): a Topic is no longer frozen after the first derive — a seed is explicitly overwritable, and `set_if="always"` re-derives every prompt to track conversation drift. It **amends [ADR-0057](0057-topic-generation-is-pop-owned-via-curated-agent-recipes.md)**: truncation moves from a hardwired last-resort fallback to an explicit `truncate` step, and recipes gain a per-step type, appended args, and timeout. It **extends [ADR-0058](0058-topic-lives-in-the-pop-topic-tmux-user-option.md)** with the `@pop_topic_kind` provenance option, kept separate so `@pop_topic` stays a directly-displayable slug.

## Considered Options

- **A single `only_when_empty` boolean per entry.** Rejected: a two-value flag can't express the three states (empty / seed / final) the seed-then-refine flow needs, and it can't express regeneration at all. `set_if` with three values does both.
- **Fold provenance into `@pop_topic` as a prefix** (e.g. `seed:refactor-auth`). Rejected: pollutes every tmux surface that displays the slug — the single-source-for-display property ADR-0058 exists to protect. A separate option costs one extra tmux write, which is free.
- **Run agent steps synchronously in the hook.** Rejected: `set_if="always"` would block the prompt submit on a model call every prompt, killing the instant-seed feedback the algorithm step exists to give. Async via the daemon also yields the per-pane single-flight a fork-per-hook can't.
- **Raw-argv command override for custom args.** Rejected: throws away the curated recipe's parse step (e.g. claude's result-JSON extraction). Args are instead appended to the curated argv; the `cmd:` escape hatch remains for full control.

## Consequences

- `prev_topic` in the recipe payload regains signal — it is the seed an agent step is overwriting — reversing a documented consequence of ADR-0025. A new additive `prev_topic_kind` field rides alongside it (additive-only contract, ADR-0024).
- A config of only agent steps with no `truncate` step can leave a pane with no Topic when every agent fails. Accepted: it is now an explicit, visible choice in the user's list rather than a hidden guarantee.
- `set_if="always"` is a per-prompt model call; the daemon single-flight bounds it to one in-flight derivation per pane, superseding stale ones.
- Deferred: an `agent` step always writes `final`, so agent-refines-agent chains aren't expressible. If wanted, a per-step `writes = "seed" | "final"` would replace the `type → kind` derivation.
