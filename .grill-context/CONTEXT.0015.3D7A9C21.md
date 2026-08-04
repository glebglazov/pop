+ Map assist
  `pop map assist [<map-id>]` — an attended agent session scoped to a whole Map
  rather than to one ticket, for the idea that arrives with no ticket in hand:
  new scope for an existing ticket, a fresh ticket, a patch of fog, or the
  realisation that something sits past the destination. Claims nothing and
  **resolves nothing** — `pop map resolve` belongs to the ticket's own claimed
  session, which is what keeps one non-research ticket per session traceable.
  Writes tickets, a ticket's `## Question`, `blocked_by` edges, `map.md` outside
  the `pop:generated` markers, and `pop map out-of-scope` — that last a scoping
  act, not a resolve. Runs in one reused pane per Map in the **Map session**'s
  `map` window, tagged `@pop_assist`, so a second call lands in the first pane
  rather than racing it, and closes by re-running `pop map register` to work the
  MALFORMED loop. Loads the wayfinding skill in assist mode, its third alongside
  chart and work. Dashboard key `S`, ungated by frontier size — an empty or
  fully-claimed frontier is when it is most needed.
  avoid: map chat, map edit, map shell, ticketless grilling
  under: Wayfinder

~ Map verb family
  `pop map` — the one command family that reads and mutates Maps: `status`,
  `register`, `authoring-guide`, `next`, `fan-out`, `assist`, `claim`,
  `resolve`, `out-of-scope`, `spawned`, `arrive`, `abandon`, `open`, `archive`,
  `unarchive`. Renamed from `pop wayfinder` as a hard cut with **no alias** (same
  discipline as the `pop queue` cut): kind nouns everywhere, and "wayfinder"
  survives only as the *skill's* name. Reads never create state. A Map's
  *metadata* is never hand-edited once the family owns it, but its *prose* — a
  ticket's `## Question`, and `map.md` outside the `pop:generated` markers — is
  written and edited in place by the session; there is no authoring payload, and
  `authoring-guide` is how a session learns the shape it is writing. Every write
  **auto-opens, never refuses**: it ensures the **Map session** exists and
  reports where it is, rather than erroring because it was run from somewhere
  else. Spawning writes (`next`, `fan-out`, `assist`) stay put unless asked
  otherwise.
  was: the same entry as minted in `.grill-context/CONTEXT.0013.6F1D82A4.md`,
  before `assist` joined the family and became the third spawning write.

  <!-- Ticket 12's `~ Authoring guide` op is deliberately not minted here: its
  reconciled final form, already carrying the clause that Map assist's
  never-resolves rule stays in the Work store doc, was minted as
  `.grill-context/CONTEXT.0014.B37C05E9.md` by slice 09. -->
