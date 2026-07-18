---
status: accepted
---

> **Relates:** executes the semantics of [ADR-0037](0037-includes-carry-a-whitelisted-config-subset-with-parent-first-precedence.md), [ADR-0083](0083-repo-config-is-one-shared-schema-for-pop-toml-and-repo-blocks.md), and [ADR-0092](0092-task-config-is-parented-under-tasks-with-verb-named-phase-sub-tables.md) without changing them; does **not** reopen the [ADR-0085](0085-config-resolution-is-driven-by-a-leaf-keyed-settings-registry.md) tombstone (see Considered options).

# Config merging is one tag-driven walker across all surfaces

## Context

Config→Config merging had grown three hand-threaded surfaces with different,
undocumented per-field granularity:

1. **Layer overlay** (`applyConfigLayerMerge`/`mergeConfigOverlay`): embedded
   defaults → `config.runtime.toml` → user `config.toml`, last-wins, whole-table
   replace per top-level key — except `integrations` (field-level) and `repo`
   (per-map-key).
2. **Include merge** (inline in `LoadWith` + `mergeIncludedTask`/`mergeIncludedEffort`/
   `mergeIncludedWorkbenchOpts`): first-definition-wins with collision warnings,
   whitelist per ADR-0037, granularity varying per section (lists append, maps
   merge per key, `[tasks]` descends field-level but `tasks.verify`/`tasks.git`
   replace whole-block).
3. **Repo-scope resolution** (`ResolveRepoConfig`, `ResolveWorkbenchesWith`,
   `ResolvePreferredWorkbench`): a per-checkout, query-time ladder (ADR-0083),
   with the repo-identity walk, `.pop.toml` reads, and trunk-anchor logic
   **triplicated** — and only `ResolvePreferredWorkbench` implementing the
   two-anchor (worktree → trunk) inheritance ADR-0083 prescribes; the other two
   read the identity root only.

Plus 16 bespoke deep-clone functions. Adding one config field touched ~6 sites;
forgetting the `mergeConfigOverlay` branch silently dropped the field from the
merged config. `config/keys.go` already proves the alternative in-package: a
reflection walk over toml struct tags is the single source of truth for
`pop config keys` and `repoScopeLegalKeys`.

## Decision

One **type-generic reflection walker** in the `config` package replaces all
hand-threaded merge and clone code. It merges same-type pairs only
(`merge(dst, src *T, md, policy)`); it never crosses scope schemas.

- **Per-call policy, shared field table.** The caller picks direction and
  collision behaviour: overlay = last-wins silent; include = first-wins with
  today's exact warning strings; repo-scope = ladder order. WHO wins is
  untouched — ADR-0037/0083/0092 semantics are executed, not redefined.
- **Two struct tags declare granularity.** `merge:"<kind>"` drives overlay and
  repo-scope; `include:"<kind>"` drives the include path. Kinds: `replace`
  (whole value), `fields` (descend), `map` / `map-first-wins` (per key),
  `append`, `list-by-key=<field>` (keyed union with collision warnings, for
  workbenches). Nested quirks are tags on nested fields
  (`TasksConfig.Verify include:"replace"` vs `Implement include:"fields"`).
- **Include whitelist = `include:` tag presence** (ADR-0037 encoded in the
  schema): a field without the tag is warned-and-ignored in includes, so a new
  field can never silently leak into includes.
- **Untagged default = last-wins whole-replace**, so a new field is one edit
  site: its own declaration line.
- **Presence tracking stays `toml.MetaData`.** The walker derives `IsDefined`
  key paths from `toml` tags (the `keys.go` pattern), including nested paths
  (`integrations.skills`). No pointerization of fields.
- **One generic reflect deep-clone** replaces the 16 hand clones.
- **Repo-scope resolution dedups through a shared source enumerator**: one
  function maps a checkout to its ordered sources (identity-matched
  `[repo."<path>"]` block, worktree `.pop.toml`, trunk-anchor `.pop.toml` with
  the read-once guard, runtime entries where legal), doing the identity walk
  once. Three consumers: `ResolveRepoConfig` walker-merges the shared embedded
  `RepoScopeConfig` down the ladder; workbench resolution walker-merges
  `list-by-key=name`; preferred-workbench keeps its bespoke consider-chain
  (resolve-validation, explicit-none short-circuit) but iterates the shared
  sources. `trunk` stays caller-side (exact-checkout-path condition is not a
  merge).
- **Scope legality is not the engine's job.** Per-scope struct schemas plus
  load-time findings (`repoScopeLegalKeys`, `repoBlockWarnings`) keep deciding
  what each surface accepts; the walker merges only what decoded.
- **Bug-for-bug otherwise, with one sanctioned behaviour change**:
  `ResolveRepoConfig` and `ResolveWorkbenchesWith` align to ADR-0083's
  two-anchor law (a worktree-local `.pop.toml` now participates for them, as it
  already does for preferred-workbench), pinned by new tests. Every other quirk
  — including overlay whole-table replace wiping runtime sub-fields, and the
  `tasks.verify` whole-block vs `tasks.implement` field-level include asymmetry
  — is preserved and now visible as a tag. The existing config test suite is
  the behavioural spec and must pass unmodified (bar the two-anchor additions);
  no temporary old-vs-new dual-run harness. `[workload]` read-compat aliases
  and their load-time warnings (CLEANUP.md §B) are ordinary tagged fields and
  keep working.

## Considered options

- **go:generate codegen** instead of runtime reflection. Compile-time safety,
  but adds a build step and generated-file churn per field; pop has no codegen
  precedent, and config load is a cold path where reflection cost is
  irrelevant. Rejected.
- **One engine per surface** (overlay table, include table, repo-scope table).
  Shrinks 6 edit sites to 3, and the tables can drift apart — the original
  disease at smaller scale. Rejected.
- **Unify the two precedence semantics** (make includes last-wins). Changes WHO
  wins, contradicting ADR-0037's deliberate first-wins choice. Rejected
  outright.
- **Mandatory tag on every field.** Forces thought per field but doubles the
  edit sites the engine exists to collapse; the silent default is safe because
  include participation is always explicit. Rejected.
- **Pointerize fields for presence** (nil = absent) instead of `toml.MetaData`.
  Massive churn across every consumer and loses TOML's free defined-but-zero vs
  absent distinction. Rejected.
- **Normalize quirks while migrating** (e.g. uniform field-level `[tasks]`
  include merge). Hides behaviour changes inside a refactor and unpins the
  tests. Deferred to a separate decision if ever wanted.
- **Reopening ADR-0085.** That tombstone rejected a *leaf-keyed settings
  registry* for authoring repo-scope keys when only ~2 keys warranted it — a
  resolution-authoring design. This ADR mechanizes merge *execution* across
  surfaces that today hold two merge paths, a triplicated ladder, and 16
  clones; key authoring, scope curation, and precedence law are exactly as
  ADR-0083 left them.

## Consequences

- Adding a config field collapses from ~6 edit sites to the field declaration
  (plus an `include:` tag when includes should carry it, plus any getter or
  validator it needs).
- ~500 lines of hand-threaded merge/clone code deleted; a drift test walks the
  structs by reflection asserting every field is reachable and every tag value
  legal.
- The granularity zoo becomes declared, greppable schema instead of buried
  branches.
- Card #8's remaining scope shrinks to whatever the shared enumerator hasn't
  already absorbed of the identity-walk triplication.
- Glossary: **Include** definition refreshed to the post-ADR-0092 whitelist
  (recorded in this session's CONTEXT fragment).
