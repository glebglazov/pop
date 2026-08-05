---
fragment: 33848375
generation: 0020
branch: master
---

+ Manifest memo
  The process-lifetime memoization of a Task set's manifest load and validation,
  keyed on the set directory's content — `index.json`'s bytes plus every task
  markdown's mtime and size plus the directory's name set, because an unlisted
  `.md` flips the set to MALFORMED through the orphan check. Load and validation
  are a pure function of those files (no store, no git, no config, no clock), so
  a content key never serves a stale answer. It wraps `LoadManifest` itself,
  below the impure refresh that calls it, so every surface that walks the same
  definition path serves from one answer: the three passes `pop work status`
  makes over a repo group, both **Work dashboard** pages, and each 2s poll after
  the first. Unlike the **Git fact memo** its lifetime spans loads — that is the
  point, since a poll that re-validates unchanged manifests pays the open cost
  again — and it is therefore LRU-bounded, because the **Work supervisor** holds
  it for the life of the daemon.
  avoid: manifest cache (nothing persists across processes), set cache, warm cache
  under: Task sets
