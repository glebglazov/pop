---
fragment: FA5479D0
generation: 0033
branch: master
---

~ Work surface sort order
  The row ordering shared by `pop work status` and the **Work dashboard**, one order read
  by both. Whatever the active **Work view preset**'s `sort` asks for is the whole of it:
  nothing is lifted above the requested key. Absent `sort` means `created_desc` — **Work
  container creation date**, newest first, across every **Work kind** on the page, so a Map
  ranks against Task sets rather than below them (ADR-0210); `created_asc` reverses it; then
  kind precedence as a tiebreak of last resort for an exact date tie, then the owning kind's
  own comparator. `sort = "status"` is the opt-in back to the pre-ADR-0210 ordering — the
  page partitions into kind-precedence blocks with Maps trailing, and inside the Task-set
  block the status scheme applies: **IN PROGRESS** and **READY** rows float cross-project as
  two leading bands (each ordered by Project ascending, then Task set identifier
  descending), and every remaining status groups by **Project** first, then by status in the
  order AWAITING-APPROVAL, NEEDS-VERIFY, VERIFY-FAILED, FAILED, BLOCKED, DEFERRED, DONE,
  MISSING/MALFORMED, then Task set identifier descending. The three membership tiers that
  used to float above every preset — live-drain, auto-drain, orphaned — are retired as an
  ordering concept: a live drain, an auto-drain grant and an orphaned binding are said in
  the **Status cell** and move no row. A date view that lifted a row for carrying one of
  them put a container from July above a container from August, which is the one thing a
  newest-first view may not do, and auto-drain is the case that made it constant rather than
  rare: it is a durable registration bit, not a fact about now, so every set that ever
  enabled it floated forever. Scheduling never read this order and still does not: the
  supervisor pins kind precedence itself, so a display setting cannot re-rank its serial
  dispatch.
  avoid: membership tier, live-drain tier, auto-drain tier, orphaned tier, dashboard sort tiers, running tier
  was: "The row ordering shared by `pop work status` and the **Work dashboard**, one order
    read by both. Precedence: (1) a **live-drain** tier, then (2) an **auto-drain** tier,
    then (3) an **orphaned** tier — the three membership tiers, which float above every
    preset because a live drain is the one row a human always needs to see whatever they
    asked for; then (4) whatever the active **Work view preset**'s `sort` asks for." — the
    rest of the effective definition is carried forward above.

~ Work kind
  Clause-level snapshot; the rest of the effective definition is carried forward verbatim.
  Ordering is **Work surface sort order**: **Work container creation date**, then kind
  precedence — task sets, then Maps, then Routines — then that kind's own comparator. A
  kind's comparator is still only ever asked about containers it produced, so there is still
  no shared status vocabulary and pop still never ranks one kind's status against another's.
  Nothing a kind stamps on a container lifts it above that key: `LiveDrain`, `AutoDrain` and
  `Orphaned` are read by the **Status cell** and by no comparator.
  was: "Ordering is **Work surface sort order**: membership tier, then **Work container
    creation date**, then kind precedence — task sets, then Maps, then Routines — then that
    kind's own comparator."
