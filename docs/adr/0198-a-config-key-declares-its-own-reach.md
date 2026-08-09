---
status: accepted
relates: "gives the turn cap of [ADR-0190](0190-a-turn-cap-bounds-one-implementation-attempt-and-only-claude-can-enforce-it.md) a legibility surface and amends its consequence naming `pop tasks agents`; renders alongside the settable keys of [ADR-0191](0191-repo-scoped-settings-pop-writes-live-in-an-identity-keyed-runtime-layer.md); reads the declared capabilities of [ADR-0166](0166-invocation-shape-capabilities-move-onto-the-preset-spec.md); extends the reflected key catalog of [ADR-0083](0083-repo-config-is-one-shared-schema-for-pop-toml-and-repo-blocks.md) without widening it"
---

# A config key declares its own reach

## Context

Some settings do not reach everything they name. `turn_cap` bounds one
implementation attempt, but only claude can be told about it (ADR-0190): five of
six presets declare enforcement Blind, each carrying a sentence about why. Set a
cap against a repository whose drains run cursor and the number is accepted,
stored, resolved — and silently does nothing. The failure mode is that there is
nothing to read.

Every adapter already carries the answer. `AgentTurnCapEnforcementCapability` is
either Supported with the argv it emits, or Blind with a stated reason
("cursor-agent has no turn cap of any kind"). Those sentences were source-only.

ADR-0190 named `pop tasks agents` as the surface that should make this legible,
reasoning that ADR-0187 had left the same lens without an `attended_args`
column — "the same gap now twice over". That reasoning has since expired.
ADR-0195 retired `attended_args` and `attended_model`: an attended entry owns its
whole invocation, so there is no per-preset attended setting left for a column to
report. Only the turn-cap half remains, and an agent lens is the wrong place to
ask it. A human asks "what does this actually reach?" where the setting is set,
not in a per-preset matrix opened for other reasons.

`pop config keys` is the surface where settings are learned, but it is pure
reflection over struct tags — identical on every machine. Reach is the opposite:
it depends on which presets exist and what each declares. The two answers belong
side by side without being the same kind of answer.

## Decision

1. **A config key may declare a reach.** Reach is a runtime answer about one
   key — a set of per-actor support lines, each either the concrete shape the
   key takes for that actor or the actor's own stated reason it takes none. It is
   declared against the key, not against a command.

2. **Reach is separate from the reflected schema and never replaces it.** The
   key catalog stays static, machine-independent reflection. Reach is layered
   over it on request, and a key that declares none renders exactly as it does
   today.

3. **`turn_cap` registers the adapter capability flattening as its reach.**
   Supported presets contribute the argv shape with the bound left as `N`; Blind
   presets contribute the sentence the adapter already carries. There is one
   source of these sentences and it stays the capability declarations.

4. **Two surfaces render it, both at config level.** `pop config keys --why`
   shows reach for keys that declare it, in any scope. `pop config repo get`
   shows it inline beside the effective value, because that command already
   answers "what is in force here".

5. **`pop tasks agents` gains no capability columns and no `--why`.** The lens
   keeps reporting what it reports: recognition, PATH, assistance, configured
   group entries, effort ladders.

6. **`pop config keys --scope repo` marks which keys pop can write itself**,
   from the same reflection that backs `pop config repo set`. Reach says what a
   key touches; the marker says who may set it. Both belong on the key.

## Considered Options

- **A `--why` flag on `pop tasks agents`.** Rejected. It asks the question at a
  surface a human opens for other reasons, and the answer is per-preset when the
  question is per-key. The second setting that needs explaining would need a
  second flag on a third command.
- **Reach only on `pop config repo get`.** Rejected. Reach is a property of a
  key, not of the pop-written repo layer that happens to hold the first one.
  Global and `.pop/config.toml` keys can have reach too.
- **A prose sentence in each key's `desc` tag.** Rejected. `desc` is static, and
  a reason that depends on which presets are installed cannot be a struct tag.

## Consequences

- **One key uses the indirection today.** Accepted deliberately. A per-command
  flag would be fewer lines now and would have to be undone by the second key
  that needs it, which is precisely the shape this record exists to defend.
- **`pop config keys --why` output is machine-dependent**, unlike the rest of
  the catalog. That is inherent — it is the point of the flag — but it means the
  `--why` output is not a stable thing to pin in a doc or a test golden.
- **Nothing warns at set time.** `pop config repo set turn_cap 40` in a
  cursor-only repository still succeeds silently; the reach is one command away,
  not in the reply. Left open rather than guessed at, because the presets a
  repository will actually drain with are not known at set time.
