---
fragment: 59e97b1a
generation: 0013
branch: master
---

+ Workbench order
  A global `[workbench] order` list that fixes the display sequence of the
  interactive Workbench lists (the `pick_on_create` create prompt and the
  Preferred-workbench picker). Tokens are the literal on-screen labels:
  **Workbench** names plus the special options `<empty>` and `<reset>`. One flat
  rule: tokens named in `order` front-load in that sequence; everything unnamed
  follows in default order — `<empty>` leads the tail, Workbenches in resolution
  order, `<reset>` trails. An unresolvable name is ignored (same tolerance as a
  stale Preferred workbench). Global-only for now; per-repo ordering is deferred.
  avoid: workbench sort, pick order, default workbench (that is Preferred workbench)
  under: Workbench

+ Empty (Workbench option)
  The `<empty>` entry in the interactive Workbench lists: in the create prompt it
  starts a plain workbench-less Session; in the Preferred-workbench picker it
  writes an explicit-none preference (opt this checkout out of any inherited or
  repo default). Angle brackets mark it as a special, non-Workbench option. The
  Preferred picker also offers `<reset>` — delete this checkout's entry and fall
  back to inheriting down the chain (distinct from `<empty>`, which is an active
  "none", not a "forget my choice").
  avoid: no workbench, no workbench (here), reset to default, none
  under: Workbench
