---
fragment: 66eceb2e
generation: 0015
branch: master
---

+ Settings registry
  The single declarative table of leaf-keyed config **Setting descriptor**s that
  is the source of truth for how every config key is validated, resolved, and
  documented. Each descriptor names the key's allowed scopes (a subset of
  global/repo/worktree), its **Merge strategy**, default, description, and
  natural file. It drives P2 (scope-legality validation — the per-file key
  whitelists are generated from it, not hand-maintained) and P1 (resolution
  order), with docs/JSON-Schema emission as a bonus. Keyed by leaf path
  (`dashboard.sort_criteria`), never by table.
  avoid: config schema, settings table, config manifest
  under: Configuration

+ Merge strategy
  How a **Settings registry** descriptor folds a key's values across the config
  locus ladder. A closed enum of exactly three: **Replace** (finest participating
  locus wins the whole value — every scalar and whole-list-replaces key),
  **UnionByName** (accumulate across loci, a finer locus overrides a same-named
  entry, collisions warn — `workbenches`), **MergeByKey** (accumulate by map/key,
  finer locus wins per key — `commands`, `agents`, `effort`). Include-file
  accumulation is within-layer assembly of the global layer, not a merge strategy.
  avoid: merge mode, combine rule, fold policy
  under: Configuration

+ Resolve-via
  A **Setting descriptor** field splitting declaration from execution: `registry`
  keys are folded by the generic typed `resolve[T]` helper using the descriptor's
  scope order and **Merge strategy**; `custom` keys keep a bespoke resolver
  (glob-expanded `projects`, per-checkout `trunk` topology) but are still declared
  in the registry so validation and docs cover them uniformly. Declaration is
  total; generic resolution is opt-in.
  avoid: custom getter flag, resolution mode
  under: Configuration
