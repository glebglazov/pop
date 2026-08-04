~ Map session
  `pop-map-<map-id>` — the **Work session** one Map's **Grilling pane**s live
  in, created by `pop map open` or by the first write that needs it. Its single
  `map` window holds one tiled pane per ticket being grilled; there is no
  overview pane, and `pop map status <map-id>` is a verb the human types. Rooted
  at the **Trunk worktree**, resolved exactly as a managed Task-set registration
  resolves it, with `--trunk <path>` the escape hatch when it cannot be — a Map
  has no checkout of its own, so the Trunk is where the code under study
  actually lives. Being Trunk-rooted is what gives it a project: under **Session
  nesting** it is a nested row under the project whose tree it sits in, rendered
  `<project>/<map-id>` flat and `<map-id>` nested, attributed from tmux's
  `#{session_path}` matched to a project *group* and falling back to a top-level
  row when that resolves to no configured project. `pop map arrive` tears the
  session down.
  was: `pop-map-<map-id>` — the **Work session** one Map's **Grilling pane**s
    live in, created by `pop map open` or by the first write that needs it. Its
    single `map` window holds one tiled pane per ticket being grilled; there is
    no overview pane, and `pop map status <map-id>` is a verb the human types.
    Rooted at the **Trunk worktree**, resolved exactly as a managed Task-set
    registration resolves it, with `--trunk <path>` the escape hatch when it
    cannot be — a Map has no checkout of its own, so the Trunk is where the code
    under study actually lives. `pop map arrive` tears the session down.

<!-- Minted by the map-session-nesting slice of the
2026-08-03-worktree-session-locality set, from ADR-0185. Chained off the pending
`~ Map session` op in CONTEXT.0012 rather than off CONTEXT.md, which the earlier
fragment has not been folded into yet.

`Session nesting`, which this op references, is defined in CONTEXT.0017: the two
mint together or the term dangles. The draft's `pop/<map-id>` flat form is written
as `<project>/<map-id>` — `pop` was the example project, not a literal. -->
