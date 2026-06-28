---
status: accepted
---

# Agent cooldown ends at the agent's own reported reset time

> **Relates:** refines **Queue agent fallback** of ADR 0027: recovery moves from a blind fixed interval to a reset-aware deadline; rides the **Agent quota detection** signal without reopening the **Agent quota reporting** deferral

When an **Agent quota pause** is detected, the **Queue daemon** cools that preset down for a blind fixed interval (`agent_quota_retry_after`, default 30m) and re-probes by re-attempting. But the pause signal already carries the agent's real reset time — codex emits `…try again at 2:28 AM.`, claude emits `… · resets Mon 12:00am` — and we throw it away, so the queue re-attempts an agent that is still exhausted (reset is further out) or leaves it cold long after it recovered (reset was minutes away). The reset time is right there in the stream we already trust for detection.

Decision: cooldown becomes **reset-aware and best-effort**. Each preset's pause-reason parser resolves *its own* reset string to an absolute instant (local tz, AM/PM, bare-time → next occurrence within 24h, weekday → next occurrence) and carries it on the **Agent quota pause** through the drain-outcome record to the supervisor. The supervisor sets `cooldown = ResetAt + 2m` (a fixed skew buffer); a missing, unparseable, or past reset falls back to the existing fixed interval, and an absurd one (>8 days out) is treated as garbage and also falls back. A claude **weekly** reset that lands days away is honored as written — cooling that one preset for days is correct, and the rotation keeps draining on the others meanwhile.

This is deliberately *not* a reopening of **Agent quota reporting** (ADR-deferred: no token-total parsing, no auth-file access, no undocumented endpoints, no terminal scraping). The distinction that makes it safe: the reset time is read from the **same headless pause signal already parsed for detection**, *after* exhaustion — it is never queried ahead of the wall, and there is still no quota-remaining interface in the loop. Extracting the trailing reset clause from a stream we already consume is not a new data source.

## Considered options

- **Keep the blind fixed interval.** Rejected, but kept as the fallback. It is immune to wording drift and trivial, but it is wrong in both directions: it re-probes still-exhausted agents and strands recovered ones. The reset time we already capture makes the deadline correct for free.
- **Surface the parsed reset on the `pop tasks implement` path too.** Rejected as scope creep. That path already prints the raw pause Reason, which contains the human-readable reset verbatim, so a human already sees it. Only the queue needs the *structured* instant, to drive cooldown.
- **Resolve the reset string inside the cooldown policy (supervisor).** Rejected. The string formats are preset-specific; keeping that knowledge in the supervisor would couple cooldown policy to every agent's phrasing. The parser lives in tasks/ next to detection and emits an unambiguous absolute instant; the supervisor owns only the buffer, the clamp, and the fallback.

## Consequences

- **Agent quota pause** gains a structured `ResetAt` that rides the drain-outcome record across the process boundary; a zero value preserves today's exact behavior, so the change is a strict superset and fully reversible by ignoring the field.
- The string→instant parse is time-relative (next-occurrence), so it is a pure function taking an injected `now` — deterministic under test with the verbatim-captured limit strings, mirroring the queue's existing `now`-injected cooldown tests.
- Wording drift in an agent's limit message degrades to the fixed-interval fallback, never to a broken cooldown — the same graceful-degradation contract **Agent quota detection** already lives under (a missed signal is a clean miss, not a fault).
