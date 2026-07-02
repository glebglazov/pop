---
status: accepted
relates: "provides the engine for [0083](0083-per-project-config-is-a-committed-in-tree-base-plus-a-gitignored-personal-override.md)/[0084](0084-in-tree-repo-config-resolves-git-agnostically-from-two-anchors.md); generalizes the precedence law of [0077](0077-config-precedence-is-scope-first-config-breaks-equal-scope-ties.md)"
---

# Config resolution is driven by a leaf-keyed settings registry

## Context

Config today is ~30 hand-written typed getters (each hard-coding its own
default, each reading only global `Config`), merging happens in three unrelated
places with three mechanisms (`applyConfigLayerMerge`, include-merge,
`ResolveRepoConfig`/`ResolveWorkbenchesWith`), and validation is four separately
hand-maintained whitelists driven off `md.Undecoded()` (`validRepoKeys`, the
includes whitelist, `effortConfigFindings`, `repoBlockWarnings`). Extending the
config surface to per-project in-tree files (ADR-0083/0084) with a fuller set of
keys would multiply this drift. We want one declarative source of truth for
which key is legal where, how it folds across scopes, and what it means.

## Decision

A **settings registry** — a table of leaf-keyed **Setting descriptor**s — is the
single source of truth. Each descriptor carries: allowed scopes (a subset of
`{global, repo, worktree}`), **Merge strategy**, default, description, natural
file, and **Resolve-via**.

- **Hybrid representation, not a generic engine.** The typed `Config` decode
  (and its tolerant `UnmarshalTOML` / ADR-0054 finding logic) stays. A getter
  calls `resolve[T](c, key, extractᵀ, mergeᵀ)`; the helper reads the descriptor
  for the scope order (data-driven precedence), the caller supplies the type and
  merge fn (typed execution). Fully-declarative resolution is unreachable in Go
  once a strategy needs a type-specific key extractor, so we do not chase it —
  the descriptor owns *policy*, typed code owns *execution*.
- **Leaf keys, closed 3-strategy enum.** Descriptors key on leaf paths
  (`dashboard.sort_criteria`), never whole tables — which erases table-level
  field-merge. **Merge strategy** is exactly `{Replace, UnionByName, MergeByKey}`.
  A fourth strategy is a deliberate future ADR, not a day-one escape hatch;
  `resolveVia: custom` handles anything genuinely bespoke instead.
- **Abstract-scope gating over a fixed locus ladder.** The engine knows the
  fixed finest→coarsest locus ladder (ADR-0077/0083/0084) once; a descriptor
  only names the abstract scopes it participates in, and the engine expands each
  to its loci. Nature (committed vs personal) is **not** gated — a repo-scope key
  is legal in every repo locus; `naturalFile` is advisory (docs only), never a
  rejection (ADR-0083). The CLI-scratch runtime locus is always in the ladder and
  simply empty for keys pop never writes, so it needs no descriptor field.
- **Register everything; split declaration from resolution.** Every key —
  behavioral, global-only, and structural — is declared, so P2 validation and P3
  docs cover 100%. **Resolve-via** = `registry` (generic fold) or `custom`
  (bespoke resolver: glob-expanded `projects`, per-checkout `trunk`; still
  declared). The four whitelists become *generated* from the registry.
- **Incremental migration, whitelists first.** Generate the whitelists from the
  registry as the first step (immediate P2 win, before any getter moves to
  `resolve[T]`); then migrate multi-scope keys (unblocking the ADR-0083 in-tree
  surface), then sweep global-only leaves and delete the hand-maintained
  validators. This ordering is a recommendation, reversible during
  implementation without touching the architecture.

## Considered options

- **Generic engine over `map[string]any`.** Fully declarative but discards the
  typed decode and the ADR-0054 tolerant-finding machinery; stringly-typed;
  biggest rewrite. Rejected.
- **Reflection over struct tags.** Can't carry a merge function, so
  union-by-name still needs bespoke per-type code; hard to debug for no
  declarativeness win over the hybrid. Rejected.
- **Register only multi-scope keys.** Cheap and unblocks the in-tree surface, but
  the whitelists survive, docs are partial, and the registry becomes a bolt-on
  beside the old validation rather than the source of truth — forfeiting P2's
  main win. Rejected.

## Consequences

`resolve[T]` (Go generics) and the closed strategy set are the load-bearing
mechanisms; the descriptor is real data (enum + scope set) except for the typed
extractor/merge fns the caller supplies. UnionByName/MergeByKey each need one
`func(T) string` extractor per key. Adding a config key becomes: author one
descriptor, pick one of three strategies, choose `registry`/`custom`.
