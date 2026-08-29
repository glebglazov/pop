---
fragment: 1BD1E5C7
generation: 0040
branch: master
---

+ Attribution tier
  How strongly the launching pane belongs to a **Work container**: the strongest pass of
  **Pane work attribution** that named it, and the top-level key of the **Work surface
  sort order** on page A under a lifting **Work view preset**. Three tiers — tag,
  neighbourhood, repository — plus the untiered rows the pane has no relation to at all.
  It is a property of the *relation* between a row and where the human is standing, never
  of the row itself: the same container is tier 3 from one pane and untiered from the
  next, which is what distinguishes it from the membership tiers the **Work surface sort
  order** retired, and it is granted only by a preset's `lift = true`, which those tiers
  never were. A container named by two passes takes the strongest. Within a tier the page
  falls through to its ordinary ordering — kind precedence, then each kind's comparator —
  so the tier partitions the page into *here* and *elsewhere* and shuffles nothing inside
  either half.
  avoid: relevance tier, locality tier, proximity tier, pane rank, attribution rank
  under: Language

~ Pane work attribution
  Which Work containers the pane a read surface was launched in belongs to, and how
  strongly — the question every other derivation asks backwards, from a known container to
  its pane. Three passes, strongest first, and **every one of them contributes**: a pass is
  no longer a winner that silences the ones below it, it is the **Attribution tier** it
  stamps on the containers it names. **Tag**: the pane's own `@pop_*` tag naming a Task
  set, a **Decision ticket** (which resolves to its Map) or a Routine, then the **Work
  session** stamp naming a Map — this pass means "this pane *is* one pop opened for that
  work", which is identity rather than locality, and every kind answers it, concatenated in
  kind precedence order. **Neighbourhood**: where the pane is merely *standing* — the
  checkout containing the pane's directory plus the Task sets bound to it, ordered by
  **Bound-checkout lift order**, whose first term is the **Checkout claim** holder and
  therefore already says what a live **Drain** would have said; the live-drain rung is not a
  tier of its own. **Repository**: every Task set in the containing repository's **Task
  storage**, bound or not, plus that repository's Maps, keyed on the git common directory
  (**Repository identity**), which is what makes a sibling worktree answer with the trunk's
  work. It stays unrestricted now that it fires on every build rather than only when the
  passes above are silent: narrowing it to bound or recent sets would reintroduce the
  "reachable only if somebody happened to bind it" silence it was added to remove, and the
  **Work view preset** is the volume governor — a pane in pop's own checkout names 64
  containers and `active` renders 3 of them. Directories are matched canonically and by
  containment, deepest checkout first. Routines answer the tag pass alone: being
  project-scoped and short-lived, a locality pass would answer with a project's whole
  routine list for any shell in the repo. The ladder is answered kind-side behind the
  **Work seam**, obtained by type assertion the way an advanceable kind is, and resolved
  while the snapshot is being built. The pane's facts (pane id, session, directory, every
  tag, the session stamp, and the containing repository's common directory) are read once
  at launch in one `display-message` round-trip and one `rev-parse`, and carried, never
  re-read — the pane's directory cannot change, so asking git per rebuild would fork every
  poll for an answer already settled. The ladder therefore compares strings and forks
  nothing. Its one consumer is the **Work lift**, and it never announces itself: a pane
  attributed to nothing — or to a row the active view excludes — says nothing at all and
  widens nothing.
  avoid: pane ownership, current work detection, pane-to-set lookup, cursor memory, winning pass
  was: The same three passes, but first-hit: the strongest pass that answered was the whole
    answer and the passes below it never ran, so a pane tagged for one Task set was
    attributed to that set alone even while standing in a checkout two more were bound to.
    Only the repository pass merged, and only across kinds.

~ Work lift
  The Work dashboard ordering every **Work container** its launching pane is attributed to
  above the containers it is not, by **Attribution tier**, and rendering the attributed
  region under a background band. It is an ordering term rather than a block that is moved:
  the tier sits above kind precedence and above the preset's `sort`, so the rows sort into
  place instead of being lifted out and re-inserted, and the block is simply what the
  ordering looks like when the tag tier is the only occupied one. Named for what is lifted
  rather than for what caused it: the rows are Work, and a pane is tmux's word for the thing
  pop merely read the facts from. It is not a mark the human made — **Pinned action menu**
  and **Following** are the manual senses, and "pin" is left to them. Granted only by the
  active **Work view preset**'s `lift = true` — of the shipped roster, `active` alone, with
  `pin` a permanent silent alias for the key — never by roster position; under any other
  preset every row keeps its sorted position, unbanded. **Pane work attribution** itself is
  preset-independent and still computed either way. The band covers the whole attributed
  region at full row width across both lines of every row, in one shade rather than one per
  tier: its job is the *boundary* — where "work that lives here" stops — which is one fact,
  and a shade per tier would need a legend that is not on screen. It renders unconditionally,
  including when it covers the whole page, because a band that vanishes when another
  project's work arrives teaches that its absence means something. A row the human marked
  leaves the band with the rest of it: the **Selection area** moves marked rows to a region
  of its own, and a band inside a region of one kind of thing marks nothing off from
  anything. The `▸` prefix mark survives on the **tag** tier alone, where it says the one
  thing the band cannot — this pane *is* that work, rather than that work merely lives here.
  Re-derived on every rebuild from pane facts read once at launch. `pop work status` builds
  with empty pane facts and never lifts, never bands and never tiers.
  avoid: pane pin, pin, pinned row, pane-seeded cursor, sticky row, follow mode, cursor sync, reveal, jump-to-row, lifted block
  was: The dashboard lifting the attributed rows *out* of the ordered list and rendering
    them as a block above it, marked `▸` in the prefix column on every one of them — a
    post-sort move rather than an ordering term, and driven by whichever single pass of the
    ladder had won.

~ Bound-checkout lift order
  The order of the neighbourhood **Attribution tier**: the **Checkout claim** holder while
  something is live there, then the set drained most recently, then the topmost bound row
  under the active sort. Since every candidate is in the tier this only decides who leads
  it — being wrong costs second place, not a wrong answer — and the leader of the strongest
  occupied tier is what seeds the dashboard cursor at first render. It absorbed the
  live-drain rung that used to sit above it: a live drain and the claim holder are the same
  fact reached two ways, and one ordering question deserves one mechanism. Scoped to the
  neighbourhood tier alone; the repository tier below it needs no rule of its own, ordering
  by kind precedence and then the active sort. "Checkout" is the generic here and
  deliberately not "worktree", which pop reserves for a *linked* checkout — the pass fires
  on a **Trunk worktree** at least as often.
  was: The same order, but scoped to a rung that only fired when the tag rung was silent,
    and sitting below a separate live-drain rung that answered first with the set of the
    drain whose checkout contained the pane.

~ Work surface sort order
  The row ordering shared by `pop work status` and the **Work dashboard**, one order read by
  both. On page A under a preset declaring `lift = true`, the **Attribution tier** is the
  first key — attributed work above unattributed, strongest tier first — and everything
  below is unchanged: whatever the active **Work view preset**'s `sort` asks for, absent
  `sort` meaning `created_desc` (**Work container creation date**, newest first, across every
  **Work kind** on the page, so a Map ranks against Task sets rather than below them,
  ADR-0210), `created_asc` reversing it, then kind precedence as a tiebreak of last resort
  for an exact date tie, then the owning kind's own comparator. `sort = "status"` is the
  opt-in back to the pre-ADR-0210 ordering — the page partitions into kind-precedence blocks
  with Maps trailing, and inside the Task-set block the status scheme applies: **IN
  PROGRESS** and **READY** rows float cross-project as two leading bands (each ordered by
  Project ascending, then Task set identifier descending), and every remaining status groups
  by **Project** first, then by status in the order AWAITING-APPROVAL, NEEDS-VERIFY,
  VERIFY-FAILED, FAILED, BLOCKED, DEFERRED, DONE, MISSING/MALFORMED, then Task set identifier
  descending. The three membership tiers that once floated above every preset — live-drain,
  auto-drain, orphaned — stay retired, and the attribution tier is not their return: they
  were durable properties of a row, so a set that ever enabled auto-drain floated forever on
  every machine and in every pane, which is what made the July-above-August violation
  constant; a tier that depends on where the human is standing changes with the pane, and it
  is gated by a preset key those tiers never had, so the archival date views stay in pure
  date order. Scheduling never reads this order: the supervisor pins kind precedence itself,
  so a display setting cannot re-rank its serial dispatch.
  avoid: membership tier, live-drain tier, auto-drain tier, orphaned tier, dashboard sort tiers, running tier
  was: "Whatever the active **Work view preset**'s `sort` asks for is the whole of it:
    nothing is lifted above the requested key." — the rest of the effective definition is
    carried forward above.
