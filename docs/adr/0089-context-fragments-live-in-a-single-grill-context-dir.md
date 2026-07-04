# Context fragments live in a single `.grill-context/` dir

Session glossary fragments (formerly `CONTEXT.<counter>.<uuid>.md` written beside each base `CONTEXT.md`) now live in one hidden `.grill-context/` directory at the repository root. The base `CONTEXT.md` stays visible where it is; only the noisy per-session shards relocate, decluttering working directories. In a multi-context repo (`CONTEXT-MAP.md`), all contexts share that **one** dir — a fragment names its context by slug prefix (`<slug>.<counter>.<uuid>.md`, slug = slugified `CONTEXT-MAP.md` link text) rather than by living in the context's own directory, so a 3-context map still produces a single fragment dir instead of three.

## Considered Options

- **Colocated (old):** fragments beside each base. Simple to resolve context (by directory) but clutters every context dir and, in multi-context repos, scatters shards across many dirs.
- **One dir, subdir per context:** `.grill-context/<slug>/...`. Reintroduces the per-context nesting we wanted gone.
- **One dir, filename-encoded context (chosen):** flat, human-scannable, read-glob groups by slug prefix. Truly one dir.

The canonical `CONTEXT-MAP.md` format is fixed to the bullet-link style (`- [Name](path) — …`) so the slug is deterministically derivable from link text.

## Consequences

- Writers only write `.grill-context/`. Readers **union both** `.grill-context/` and legacy colocated `CONTEXT.*.md`, so existing repos keep working with no forced migration; `grill-consolidate` drains legacy shards into the base over time.
- The per-context generation counter must scan **both** locations for that context (`max+1`) or a new shard could reuse a live generation.
- Context link text must be unique after slugifying, since the slug is the only thing separating contexts' fragments in the shared dir.
