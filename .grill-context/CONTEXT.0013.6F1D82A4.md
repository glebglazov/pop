~ Map verb family
  `pop map` — the one command family that reads and mutates Maps: `status`,
  `register`, `authoring-guide`, `next`, `fan-out`, `claim`, `resolve`,
  `out-of-scope`, `spawned`, `arrive`, `open`, `archive`, `unarchive`. Renamed
  from `pop wayfinder` as a hard cut with **no alias** (same discipline as the
  `pop queue` cut): kind nouns everywhere, and "wayfinder" survives only as the
  *skill's* name. Reads never create state. A Map's *metadata* is never
  hand-edited once the family owns it, but its *prose* — a ticket's
  `## Question`, and `map.md` outside the `pop:generated` markers — is written
  and edited in place by the session; there is no authoring payload, and
  `authoring-guide` is how a session learns the shape it is writing. Every
  write **auto-opens, never refuses**: it ensures the **Map session** exists and
  reports where it is, rather than erroring because it was run from somewhere
  else. Spawning writes (`next`, `fan-out`) stay put unless `--focus` asks
  otherwise.
  was: `pop map` — the one command family that reads and mutates Maps: `status`,
  `register`, `next`, `fan-out`, `claim`, `resolve`, `out-of-scope`, `spawned`,
  `arrive`, `open`, `archive`, `unarchive`. Renamed from `pop wayfinder` as a
  hard cut with **no alias** (same discipline as the `pop queue` cut): kind
  nouns everywhere, and "wayfinder" survives only as the *skill's* name. Reads
  never create state; a Map's metadata is never hand-edited once the family owns
  it. Every write **auto-opens, never refuses**: it ensures the **Map session**
  exists and reports where it is, rather than erroring because it was run from
  somewhere else. Spawning writes (`next`, `fan-out`) stay put unless `--focus`
  asks otherwise.

<!-- The `+ Authoring guide` op drafted alongside this one (map ticket 09)
scoped the guide to mechanics only and left behavioural rules in the doc. Ticket
10's fuller definition supersedes it and is minted in CONTEXT.0014 — mint that
entry, not the narrower one. -->
