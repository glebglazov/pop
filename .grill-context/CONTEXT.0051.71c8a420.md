---
fragment: 71c8a420
generation: 0051
branch: master
---

+ Agent unavailability
  A condition detected from one agent invocation meaning this **Agent preset**
  cannot do the work at all, as distinct from an attempt that merely failed.
  **Agent quota pause** is one flavour; an unauthenticated agent is another. Every
  flavour skips the remaining **Task retry cap** for that preset and hands the
  turn to the next preset in the **Agent fallback** list; the flavours differ only
  in what heals them.
  avoid: agent error, attempt failure, agent down
  under: Agent integrations

+ Agent unavailability recovery
  What would make an unavailable **Agent preset** usable again, carried on the
  unavailability verdict. Time-healing carries a reset instant and drives **Agent
  quota recovery wait**; human-healing carries no instant and must never enter
  that wait, because polling cannot resolve it.
  avoid: retry policy, backoff kind
  under: Agent integrations

+ Agent authentication failure
  The human-healing **Agent unavailability** flavour: the agent CLI refuses to
  run because its session is absent or expired and asks the operator to log in.
  Confirmed shape for `cursor` (2026-07-29): the message arrives as plain text on
  stderr with an empty stdout — no structured stream at all — and the process
  exits 1.
  avoid: agent quota pause, unauthorized error, API key error
  under: Agent integrations
