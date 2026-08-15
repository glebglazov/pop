---
fragment: 3624F3E2
generation: 0014
branch: master
---

+ Work container creation date
  The one key **Work container**s of different **Work kind**s are ranked against each
  other by: the `YYYY-MM-DD[-HHMM]` prefix of the container's own identifier, parsed
  once and stamped onto the container by the kind's own load — never persisted, never a
  lifecycle instant, and distinct from the registry's registered-at. A **zero** date
  means "no opinion": the container sorts after every dated one and falls through to its
  kind's own comparator. That is how a Routine opts out of cross-kind ranking, having no
  creation date to give, and where a Map registered before the prefix was enforced lands.
  Because it is the only cross-kind key, a Map identifier without the prefix is refused
  at registration rather than silently sinking to the bottom of the page.
  avoid: registered-at, created_at, recency window, creation instant
  under: Language

~ Work surface sort order
  The row ordering shared by `pop work status` and the **Work dashboard**. Precedence:
  (1) a **live-drain** tier, then (2) an **auto-drain** tier, then (3) an **orphaned**
  tier — each floating above everything else, because a live drain is the one row a human
  always needs to see whatever they asked for; then (4) **Work container creation date**,
  descending unless the active **Work view preset** declares `created_asc`; then (5) kind
  precedence — task sets, then Maps, then Routines — as a tiebreak of last resort for the
  rare exact date tie; then (6) the owning kind's own comparator, which is only ever asked
  about containers of its own kind. Rows of different kinds therefore interleave by
  creation date rather than sitting in per-kind blocks. The ADR-0121 status scheme —
  **IN PROGRESS** and **READY** floating cross-project as two leading bands, every
  remaining status grouping by **Project** then by AWAITING-APPROVAL, NEEDS-VERIFY,
  VERIFY-FAILED, FAILED, BLOCKED, DEFERRED, DONE, MISSING/MALFORMED, then identifier
  descending — is no longer the default: it is reached by a preset declaring
  `sort: status`, which restores the per-kind partition wholesale and is the one way back
  to the ordering that predates cross-kind ranking.
  was: "The row ordering shared by `pop work status` and the **Work dashboard** when the
    active **Work view preset** declares no `sort`. Precedence: (1) a **live-drain** tier,
    then (2) an **auto-drain** tier, then (3) an **orphaned** tier — each floating above
    the status scheme; then (4) the status scheme itself. … A preset's `sort` replaces the
    status scheme only — the three membership tiers float above every preset."

~ Work view preset
  Clause-level snapshot; the rest of the effective definition is carried forward verbatim.
  The `sort` field takes `created_desc`, `created_asc` or `status`. Absent `sort` means
  `created_desc`: **Work container creation date** is the default order on every Work read
  surface. `status` is the explicit opt-in back to the ADR-0121 status scheme and the
  per-kind partition that comes with it. Of the shipped roster, `recent-7d` and
  `recent-30d` declare `created_desc`; `active`, `unfolded`, `all` and `muted` declare
  nothing and so take the default; none pins `status`.
  was: "Declares optional `label`, `status`, `unfolded`, `archived`, `muted`,
    `created_within`, `sort`, and one `hide` clause" — with `sort` taking `created_desc`
    or `created_asc` only, and its absence meaning the ADR-0121 status scheme.

~ Work kind
  Clause-level snapshot; the rest of the effective definition is carried forward verbatim.
  Ordering is **Work surface sort order**: membership tier, then **Work container creation
  date**, then kind precedence — task sets, then Maps, then Routines — then that kind's own
  comparator. A kind's comparator is still only ever asked about containers it produced, so
  there is still no shared status vocabulary and pop still never ranks one kind's status
  against another's; what changed is that two kinds are now ranked against each other on a
  key both already carry. Header counts remain each kind's own phrases joined in kind
  precedence order, which no longer describes the row order.
  was: "Ordering is fixed kind precedence — task sets, then Maps, then Routines — then
    that kind's own comparator, and header counts are each kind's own phrases joined in
    that order."

~ Work page
  Clause-level snapshot; the rest of the effective definition is carried forward verbatim.
  Page A is ordered by **Work surface sort order**, so its Task sets and Maps interleave by
  **Work container creation date** rather than sitting in per-kind blocks. Page B is
  single-kind and its Routines stamp no creation date, so they all tie on that key and fall
  through to the Routine comparator — **Routine relevance tier** orders page B exactly as
  before, without page B needing to be special-cased.
  was: "page A is ordered by kind precedence then each kind's comparator, page B is
    single-kind and therefore just the Routine comparator."
