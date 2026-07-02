---
status: accepted
supersedes: [ADR-0077, ADR-0084, ADR-0085]
---

> **Relates:** amends [ADR-0078](0078-preferred-workbench-is-a-per-worktree-personal-setting.md); replaces the scope-first precedence law of [ADR-0077](0077-config-precedence-is-scope-first-config-breaks-equal-scope-ties.md); preserves the integrations behaviour of [ADR-0065](0065-integrate-preferences-are-three-layer-config-merge.md).

# Repo config is one shared schema, resolved user-first, hand-authored over runtime

## Context

`RepoConfig` (the `.pop.toml` schema) and `RepoOverrideConfig` (the global
`[repo."<path>"]` schema) had drifted, and precedence had grown two competing
stories (ADR-0077 scope-first; ADR-0078 worktree-runtime-beats-repo-default). We
want (1) repo-scoped settings authored **either** in the repo (`.pop.toml`, flat)
**or** centrally (`[repo."<path>"]`), from one key set; and (2) a single
precedence law where **pop is user-driven** — the user's own hand-authored config
is supreme and pop's runtime-generated scratch never overrides it.

This session explored a larger design (fuller in-tree surface, a committed base +
gitignored `.pop.local.toml` override, a general settings registry) and narrowed
away from it. This ADR records the narrow result and the new precedence law, and
supersedes the ADRs written for the abandoned pieces (0084, 0085) and the old
scope-first law (0077).

## Decision

### One shared repo-scope schema

`.pop.toml` and `[repo."<path>"]` decode the **same key set**. `.pop.toml` is the
flat in-repo form; `[repo."<path>"]` is the central, path-keyed form. Adding a
repo-scope key defines it **once** and both loci accept it. The sole exception is
`trunk` — per-checkout machine topology, central `[repo]`-only, never valid in
`.pop.toml`. Repo scope is a **curated** set of genuinely repo-specific keys
(today `workbenches`, `preferred_workbench`), **not** a mirror of global config:
`projects`, `queue`, daemon knobs, and other machine/global keys stay global-only
and are rejected at repo scope. `preferred_workbench` becoming repo-legal amends
ADR-0078, which had barred it from `.pop.toml`; committing the file vs gitignoring
it is the author's git choice — pop never inspects tracking.

### Precedence law (replaces ADR-0077)

Two rules, in order:

1. **Hand-authored (user-written) beats runtime-generated — at any scope.**
2. **The user's central `config.toml` beats the repo's in-tree `.pop.toml`.**

pop is user-driven: the user's explicit config is supreme; runtime is a
gap-filler. Full ladder, highest → lowest:

```
1  config.toml  [repo."<path>"]        user central · repo-specific
2  config.toml  (global keys)          user central · universal
3  ./.pop.toml                         repo in-tree (this worktree)
4  <trunk>/.pop.toml  (→ id-root)      repo in-tree (inherited)
5  config.runtime.toml[<wt-path>]      runtime · CLI-scratch (ctrl+w)
6  config.runtime.toml[<trunk-path>]   runtime · CLI-scratch (inherited)
7  config.runtime.toml integrations    runtime · CLI-scratch (global)
8  embedded default
```

Within `config.toml`, `[repo]` (specific) beats global keys. Everything
hand-authored (1–4) beats everything runtime (5–7). Consequences the team
accepts: a **global** `config.toml` value shadows every repo's committed value
for that key (repo defaults apply only where the user set no global one); and
runtime (`ctrl+w`, integrate) applies **only** where nothing hand-authored sets
the key — to diverge one worktree over a repo default, hand-author
`config.toml [repo."<exact-worktree-path>"]`. This is why ADR-0078's
worktree-runtime-beats-repo-default ordering is reversed. The ADR-0065
integrations behaviour is preserved: hand-authored `config.toml` skills still beat
runtime skills (tier 1 over tier 3).

### Inheritance: two anchors, presence decides

pop resolves repo-scope in-tree config at two anchors — this worktree (worktree
position) and the **Trunk worktree** (inherited position), the trunk read falling
back to the **Repository identity** root for a bare repo. A worktree with its own
`.pop.toml` overrides the inherited trunk one; a worktree without inherits
trunk's. Reuses ADR-0078's `Deps.Trunk` resolver, its no-trunk fallthrough, and
its this-is-trunk read-once guard.

## Considered options

- **Scope-first (ADR-0077): most-specific scope wins, finer runtime beats coarser
  hand-authored.** Rejected: it let a repo's committed value, or a runtime
  scratch entry, override the user's explicit central config — the opposite of
  "pop is user-driven."
- **Milder variant: `[repo]` above `.pop.toml`, but a repo's `.pop.toml` beats the
  user's *global* config.toml.** Rejected: keeps a repo able to override the
  user's explicit global choice; the user chose full "config.toml on top."
- **Fuller in-tree surface + `.pop.local.toml` + general settings registry.**
  Dropped: only ~2 keys are genuinely repo-specific, so the engine and the
  gitignored personal file were over-built. Personal per-repo override is served
  by central `[repo."<path>"]`; quick per-worktree override by `ctrl+w`→runtime
  (gap-filler); co-located repo content by committed `.pop.toml`.
