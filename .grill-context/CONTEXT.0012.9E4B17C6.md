- Grilling window

+ Grilling pane
  One Decision ticket's pane inside a **Map session**, tagged with the ticket id
  and titled after the ticket file's stem, running the interactive agent on the
  wayfinding skill in work mode. Every ticket agent is a pane in the session's
  single `map` window under a `tiled` layout, so one window shows the whole
  frontier in flight. Spawned by `pop map next` and by **Frontier fan-out**,
  neither of which moves the caller unless asked (`--focus`, and the uppercase
  dashboard keys). A pane whose agent is still alive is a jump target and is
  never sent work again (ADR-0158); an idle one (bare shell) is respawned. The
  other writes (`register`, `claim`, `resolve`, `out-of-scope`) run **in place**
  and spawn nothing: an agent resolving a ticket from a Task-set pane must not
  relocate its human.
  avoid: grilling window, map window, ticket window

+ Frontier fan-out
  `pop map fan-out <map-id>` — spawning one **Grilling pane** for every ticket
  on a Map's **Frontier** in one act, so a whole wayfinding sitting is walked in
  parallel. Defined as looped `pop map next`, not a second spawn path: each
  iteration claims atomically, so a ticket a parallel session takes mid-fan-out
  simply yields one fewer pane. HITL tickets included, using the configured
  interactive agent in skip-permissions mode. Idempotent — a re-run reuses live
  panes and tops up whatever the frontier has since released.
  avoid: fanout, spawn all, batch grill

~ Map session
  `pop-map-<map-id>` — the **Work session** one Map's **Grilling pane**s live
  in, created by `pop map open` or by the first write that needs it. Its single
  `map` window holds one tiled pane per ticket being grilled; there is no
  overview pane, and `pop map status <map-id>` is a verb the human types. Rooted
  at the **Trunk worktree**, resolved exactly as a managed Task-set registration
  resolves it, with `--trunk <path>` the escape hatch when it cannot be — a Map
  has no checkout of its own, so the Trunk is where the code under study
  actually lives. `pop map arrive` tears the session down.
  was: `pop-map-<map-id>` — the **Work session** one Map's windows live in,
  created by `pop map open` or by the first write that needs it. Window 1 runs
  `pop map status <map-id>`, so attaching opens on what the Map is deciding;
  every later window is a **Grilling window**. Rooted at the **Trunk worktree**,
  resolved exactly as a managed Task-set registration resolves it, with
  `--trunk <path>` the escape hatch when it cannot be — a Map has no checkout of
  its own, so the Trunk is where the code under study actually lives. `pop map
  arrive` tears the session down. Distinct from a **Pane topic**-style shared
  drain window: a Map's layout is derived from live Map state, which is why it is
  a session rather than a Workbench.

~ Map verb family
  `pop map` — the one command family that reads and mutates Maps: `status`,
  `register`, `next`, `fan-out`, `claim`, `resolve`, `out-of-scope`, `spawned`,
  `arrive`, `open`, `archive`, `unarchive`. Renamed from `pop wayfinder` as a
  hard cut with **no alias** (same discipline as the `pop queue` cut): kind
  nouns everywhere, and "wayfinder" survives only as the *skill's* name. Reads
  never create state; a Map's metadata is never hand-edited once the family owns
  it. Every write **auto-opens, never refuses**: it ensures the **Map session**
  exists and reports where it is, rather than erroring because it was run from
  somewhere else. Spawning writes (`next`, `fan-out`) stay put unless `--focus`
  asks otherwise.
  was: `pop map` — the one command family that reads and mutates Maps:
  `status`, `show`, `register`, `next`, `claim`, `resolve`, `out-of-scope`,
  `spawned`, `arrive`, `open`, `archive`, `unarchive`. Renamed from
  `pop wayfinder` as a hard cut with **no alias** (same discipline as the
  `pop queue` cut): kind nouns everywhere, and "wayfinder" survives only as the
  *skill's* name. Reads never create state; a Map's metadata is never
  hand-edited once the family owns it. Every write **auto-opens, never
  refuses**: it ensures the **Map session** exists and reports where it is,
  rather than erroring because it was run from somewhere else.
