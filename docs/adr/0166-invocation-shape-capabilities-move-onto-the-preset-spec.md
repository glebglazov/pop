---
status: accepted
---

# Invocation-shape adapter capabilities move onto the preset spec

The per-adapter facts describing how an agent's *CLI* is driven — reasoning arguments, quota-reset reading, availability probe, effort ladder, and executable name — move from scattered `switch preset` statements and package-level maps onto `presetAgentSpec`, under the same unset-is-invalid validation as the stream-shape family ([ADR-0165](0165-stream-shape-capabilities-are-declared-and-fixture-backed.md)). Unlike that family they carry **no fixture requirement**: these are claims about argument strings, and no captured data can confirm or refute them.

The migration is done in one pass with ADR-0165's, because construction-time validation can only be switched on once. Validating half the capabilities and staying silent on the rest reproduces the exact hole the decision exists to close.

## The defects that made this worth doing

Putting the sites side by side surfaced two bugs that neither site shows alone.

**`ReasoningSpecTokens` and `ArgsContainReasoning` are a matched pair that nothing keeps matched.** One emits the reasoning flag for a preset; the other detects a hand-set one so the **Effort ladder** doesn't override it. They live in separate switches at `tasks/agent.go:367` and `:388` with *different case sets* — the detector has a `cursor` arm the emitter does not. Add an adapter, implement one arm and forget the other, and a user's hand-set reasoning is silently overridden — the precise failure the pair exists to prevent. As one capability struct they are a single declaration and cannot drift.

**The executable name is stated twice, in two packages, in two shapes.** `cursor → cursor-agent` appears in `doctorAgentExecutables` (`cmd/doctor.go:819`, a map) and again inside `IsAgentAvailabilityProbeCommand` (`tasks/agent_availability_probe.go:242`, a switch keyed on the *executable* rather than the preset). One adapter fact, two homes, no link between them. It becomes a spec field, and doctor's map is deleted in favour of reading the adapter.

## Considered Options

- **Leave invocation-shape facts as switches; migrate only what the audit needs.** Rejected: the two defects above are in the invocation family, not the stream family. Migrating only the audit's dependencies would have left the reasoning-pair drift in place while claiming to have closed the forget-silently hole.
- **Require fixtures here too, for symmetry.** Rejected: a fixture for "codex takes `-c model_reasoning_effort=`" would be a restatement of the code under test, which is churn dressed as evidence.
- **Keep `doctorAgentExecutables` so `cmd` doesn't depend on `tasks` for this.** Rejected: `cmd` already depends on `tasks` throughout, and the duplication is a live inconsistency risk rather than a decoupling.

## Consequences

`cmd/doctor.go` loses its executable map and reads the adapter instead. `IsAgentAvailabilityProbeCommand` keeps its executable-keyed signature — it answers a question about a spawned process, not about a preset — but resolves through the spec rather than restating names.

An adapter can now be fully described by its spec entry, which is the point: `tasks/agent.go`'s preset table becomes the single readable answer to "what does pop know about this agent," instead of a starting point for grepping six files.
