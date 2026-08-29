---
fragment: 4EF3D32D
generation: 0039
branch: master
---

+ Cache database
  The machine-local SQLite database at `$XDG_CACHE_HOME/pop/cache.db` holding
  every derived answer pop may recompute rather than lose. It is not the
  **Execution state store**: nothing in it is authoritative, every entry is
  re-validated against the source it was derived from before it is served, and
  deleting the file is always a valid repair. That last property is what earns
  it a home under the cache dir and a name general enough for its second table,
  and what separates it from the deleted glob cache, which was persistence with
  nothing to validate against.
  avoid: pop.db (that is the state store), warm cache, glob cache
  under: Tasks

~ Manifest memo
  The memoization of a Task set's manifest load and validation, keyed on the set
  directory's content — `index.json`'s bytes plus every task markdown's mtime and
  size plus the directory's name set, because an unlisted `.md` flips the set to
  MALFORMED through the orphan check. Load and validation are a pure function of
  those files (no store, no git, no config, no clock), so a content key never
  serves a stale answer. It wraps `LoadManifest` itself, below the impure refresh
  that calls it, so every surface that walks the same definition path serves from
  one answer: the three passes `pop work status` makes over a repo group, both
  **Work dashboard** pages, and each 2s poll after the first. It has two tiers.
  In-process it is LRU-bounded, because the **Work supervisor** holds it for the
  life of the daemon. Beneath that it persists to the **Cache database**, one row
  per set directory carrying the content key as a column — so the table is
  bounded by inventory rather than by edit history, and a fresh process opens
  without re-reading every task markdown on the machine. The persisted tier
  changes what a *miss* costs, never what a *hit* means: the content key is
  computed and compared on every serve either way, so the freshness of the first
  paint is exactly the freshness of the 2s poll.
  was: The process-lifetime memoization of a Task set's manifest load and
  validation, keyed on the set directory's content — `index.json`'s bytes plus
  every task markdown's mtime and size plus the directory's name set, because an
  unlisted `.md` flips the set to MALFORMED through the orphan check. Load and
  validation are a pure function of those files (no store, no git, no config, no
  clock), so a content key never serves a stale answer. It wraps `LoadManifest`
  itself, below the impure refresh that calls it, so every surface that walks the
  same definition path serves from one answer: the three passes `pop work status`
  makes over a repo group, both **Work dashboard** pages, and each 2s poll after
  the first. Unlike the **Git fact memo** its lifetime spans loads — that is the
  point, since a poll that re-validates unchanged manifests pays the open cost
  again — and it is therefore LRU-bounded, because the **Work supervisor** holds
  it for the life of the daemon.
  avoid: manifest cache (it persists, but never authoritatively), set cache

+ Verdict checkout pre-pass
  The concurrent resolution of every distinct runtime checkout that
  **Verified status resolution** is about to need, run once before the verdict
  loop walks the rows. Each distinct checkout costs two git forks — its
  **Repository identity** and its work SHA — and the loop used to pay them one
  after another, which is why a four-checkout dashboard spent ~53ms of its ~85ms
  poll waiting on git. The pre-pass fills the same per-checkout cache the loop
  already consulted, so the loop itself is unchanged and every resolution inside
  it is a hit. It resolves only what renders: a row filtered out, non-terminal,
  or unplaced is never resolved, before or after.
  avoid: verdict fan-out, parallel verify (nothing is verified here)
