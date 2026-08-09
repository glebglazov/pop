---
fragment: CC1E5819
generation: 0005
branch: master
---

+ Config key reach
  What a config key actually touches on this machine, as opposed to what the
  schema says it accepts: a set of per-actor lines, each either the concrete
  shape the key takes for that actor or that actor's own stated reason it takes
  none. Declared against the key rather than against a command, and rendered on
  request by `pop config keys --why` and inline by `pop config repo get`. A
  runtime answer, so unlike the reflected key catalog it differs between
  machines; a key that declares no reach renders as it always did. `turn_cap` is
  the first, registering the Agent adapter's declared turn-cap enforcement
  stances (ADR-0198).
  avoid: capability matrix, agent support table, --why on the agent catalog
  under: Configuration
