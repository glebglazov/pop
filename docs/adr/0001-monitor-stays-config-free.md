---
status: accepted
---

# The Monitor stays config-free; policy is resolved in the command layer

The `monitor` package must not import `config`. Pane-report transition rules (status, following, visit) live in the Monitor next to the pane state they govern, but any config-driven policy is resolved in the command layer and passed to the Monitor's primitives as already-decided values. In practice this is one boolean — the Unread→Clear-on-active downgrade gate — plus the "ignore status from this source" check, which stays in the command layer as a pre-check that runs before any state work.

## Why

This keeps the Monitor's dependency surface small and its primitives testable with nothing more than a temporary state file and a mock tmux — no config loading, no daemon, no command layer. Policy decisions concentrate at the edge (the command layer) where config is already loaded per request; mechanism concentrates in the Monitor.

## Considered Options

- **Import `config` into `monitor` and read policy inside the primitives.** Rejected: it widens the Monitor's dependency surface, drags config loading into what should be a pure state transition, and forces tests to construct config.
- **Resolve policy in the command layer, pass plain values (chosen).** The primitives take the resolved decision; the Monitor never knows config exists.

## Consequences

A future contributor adding a config-driven knob to pane behavior will be tempted to `import config` into `monitor`. The constraint is that the new policy is read in the command layer and handed to the Monitor as a value. If the number of such values grows large enough to be unwieldy, that is the signal to revisit this decision — not to quietly import config.
