---
status: accepted
relates: "generalizes [0065](0065-integrate-preferences-are-three-layer-config-merge.md)"
---

# Config precedence is scope-first; config.toml breaks equal-scope ties

## Context

[ADR-0065](0065-integrate-preferences-are-three-layer-config-merge.md) established a
three-layer merge for `[integrations]`: embedded defaults < `config.runtime.toml` <
user `config.toml` (user wins). Adding a **Preferred workbench** ([ADR-0078](0078-preferred-workbench-is-a-per-worktree-personal-setting.md))
needed the opposite-looking behavior — a per-worktree value written to
`config.runtime.toml` must **win** over a per-repo default in `config.toml`. Taken
naively ("which file wins?") the two features contradict: integrations says
`config.toml` wins, workbench says runtime wins. We needed one law that covers both.

## Decision

There is a single precedence law, and it is about **scope**, not file:

> **Most-specific scope wins. When the most-specific scope is set in more than one
> file, hand-authored `config.toml` beats CLI-written `config.runtime.toml`.**

Scopes, finest → coarsest: **per-worktree > per-repo (`[repo."<path>"]`) > global.**

- `config.runtime.toml` is a **storage location, not a precedence tier.** "Runtime
  always wins" and "runtime always loses" are both wrong.
- **Integrations** (`[integrations] skills`) is the *equal-scope* case: the key
  exists only at global scope, set in both files, so specificity can't decide and
  the tie-break fires → `config.toml` wins. ADR-0065's behavior is preserved
  exactly, now derived from the law rather than stated ad hoc.
- **Preferred workbench** is the *finer-scope* case: a per-worktree runtime entry is
  strictly more specific than a per-repo `config.toml` default, so it wins on
  specificity — no tie, the tie-break never applies.
- The two features **never share a scope** (worktree-scope vs global-scope keys), so
  they cannot actually collide; the law only makes the non-collision legible.

## Considered options

- **Model B — "imperative CLI (runtime) is freshest intent, so runtime beats config
  everywhere."** A clean one-liner, and it matches the gut feeling that "the thing I
  just set via CLI should win." Rejected: it reverses ADR-0065 (a committed `skills`
  list would lose to a stray local `--no-*`), reintroducing the chezmoi-fighting /
  machine-divergence footgun ADR-0065 deliberately avoided — and it buys the
  workbench feature nothing, since per-worktree data can't ride the generic
  whole-key merge regardless of direction.
- **Two disjoint resolution systems** (integrations keeps its ladder; workbench gets
  a bespoke one, never reconciled). Rejected: they don't contradict, but leaving two
  unexplained rules invites a future reader to "fix" one into the other. One stated
  law is cheaper to maintain than two coincidences.
