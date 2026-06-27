---
status: accepted
superseded:
  - [ADR-0049](0049-effort-resolves-model-and-reasoning-per-agent.md) (cursor-specific encoding only)
---

# Cursor effort uses explicit model names

Cursor-agent rejects the bracketed `--model` token that ADR-0049 prescribed (`composer-2.5[effort=medium]`) and has no `--effort` flag. Its accepted model list is flat tokens whose names already encode capability (`composer-2.5`, `composer-2.5-fast`, `claude-opus-4-8-thinking-high`, etc.). Therefore the cursor adapter selects a full concrete model name per **Effort** tier and does not emit a separate **Reasoning effort** parameter. The built-in cursor ladder now maps `heavy` → `claude-opus-4-8-thinking-high`, `standard` → `composer-2.5`, and `light` → `composer-2.5-fast`.

## Considered Options

- **Keep bracket syntax.** Rejected: the live `cursor-agent` binary lists available models and rejects `composer-2.5[effort=...]` for both composer and claude model families.
- **Add a cursor `--effort` flag.** Rejected: `cursor-agent --help` lists no such flag and the binary reports `unknown option '--effort'`.
- **Derive a suffix from a base model (e.g., `composer-2.5` + `fast` → `composer-2.5-fast`).** Rejected: no reliable suffix table exists (`composer-2.5` has no `high` variant, claude variants mix `thinking-*` and `-*` patterns), so magic suffix rules would be fragile.
- **Explicit full model names per tier.** Chosen: the ladder names exactly the token passed to `--model`, which is transparent and matches how `cursor-agent models` presents choices.

## Consequences

- `cursor` becomes the only supported preset whose **Effort ladder** uses model names alone; its `reasoning` field is accepted in config for consistency but ignored by the adapter.
- `ArgsContainReasoning` still detects an explicit user-bracket in `--model` args so a hand-pinned bracketed model token is preserved unchanged, even though cursor-agent will reject it.
- `pop tasks agents` must not append `[reasoning=...]` to cursor model names; display uses the ladder's model token verbatim.
