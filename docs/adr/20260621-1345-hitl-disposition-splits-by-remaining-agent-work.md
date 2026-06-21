# HITL disposition splits by remaining agent work (UNVERIFIED vs BLOCKED)

A Task set that stops at a human-in-the-loop gate now derives one of two statuses instead of a single `BLOCKED`. The discriminator is whether any **open AFK task remains**: if none does — only open HITL tasks stand before Done — the set is `UNVERIFIED` (the agents are finished; the only thing left is a human's eyes on the result); if an open AFK task is still gated behind a human task, it stays `BLOCKED` (real agent work remains). The same split rides the durable `unverified` / `blocked` **Drain outcome**, so `pop queue status`, the run baseline, and the journal surface "awaiting your check" as its own bucket rather than burying it inside "blocked".

This refines, without reversing, ADR-0012 (HITL gates offer attended agent assistance): both dispositions still reach the same HITL gate prompt and the same attended-help selection — `UNVERIFIED` is a relabel of disposition, not a new control path. The queue daemon still parks both, since neither has an eligible AFK task to re-drain.

## Why

"Blocked" reads as "stuck on real work." For a terminal verification HITL that misleads: the set is one human glance from Done, not jammed. The misread is worst in the queue, where you scan many projects and a "blocked" line is exactly what makes you skip a set that's actually finished bar a sign-off. Splitting by remaining agent work makes the signal honest end-to-end and matches the two healthy lifecycle roles of a HITL task — **setup at the bottom** (human prepares a harness before agents can run → `BLOCKED` until you act) and **verification at the end** (agents done → `UNVERIFIED` until you confirm).

## Considered Options

- **Keep a single `BLOCKED`.** Rejected: it conflates "needs human setup before agents proceed" with "agents done, needs human sign-off" — operationally and emotionally different states.
- **Rename `BLOCKED` wholesale to `UNVERIFIED`.** Rejected: a setup HITL at the bottom, or a decision HITL mid-flow, genuinely blocks pending agent work — calling that "unverified" would be the opposite misnomer.
- **Discriminate by HITL graph position (is it the terminal node?).** Rejected in favour of the simpler equivalent: count open AFK tasks. A set in this branch with zero open AFK tasks has only HITL work left by construction (all-done is `DONE`, some-skipped-rest-done is `DEFERRED`), so "any open AFK remaining" needs no dependency-graph traversal.

## Consequences

- `unverified` is appended to the **durable, append-only** drain-outcome journal. Once those records exist on disk, every reader must handle the value — the vocabulary cannot be cleanly un-shipped, which is why this is recorded.
- The `to-tasks` planning skill is reframed (descriptively) to name the two HITL lifecycle roles, so planners place HITL at the boundaries by default. A mid-flow HITL stays valid; when chosen, parking at `BLOCKED` mid-drain is the correct signal, not a planning failure.
