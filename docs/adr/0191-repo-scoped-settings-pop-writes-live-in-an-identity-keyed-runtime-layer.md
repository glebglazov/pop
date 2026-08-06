---
status: accepted
relates: "extends the two-home repo scope of [ADR-0083](0083-repo-config-is-one-shared-schema-for-pop-toml-and-repo-blocks.md) without widening its shared schema, obeys the hand-authored/pop-written split of [ADR-0150](0150-the-config-dir-holds-only-hand-authored-files.md), gives [ADR-0190](0190-a-turn-cap-bounds-one-implementation-attempt-and-only-claude-can-enforce-it.md) its configuration surface, and adds a route to the audit of [ADR-0160](0160-spend-is-a-cross-set-lens-and-usage-extraction-is-per-adapter.md)"
---

# Repo-scoped settings pop writes live in an identity-keyed runtime layer

## Context

A **Turn cap** varies by repository, not by machine and not by agent. A monorepo
where one task touches six packages needs a larger bound than a small tool repo,
and that variance is bigger than the variance between two agents. So the number
needs a per-repository home — and pop has no per-repository home for behaviour
today.

What exists is a *curated* repo scope with two homes and one schema (ADR-0083):
`RepoScopeConfig` holds exactly `workbenches` and `preferred_workbench`, its legal
key set is generated from the struct by reflection (`repoScopeLegalKeys`), and one
field definition makes a key legal in both the in-repo `.pop/config.toml` and the
central `[repo."<path>"]` block. ADR-0083 keeps that set small on purpose: repo
scope is "not a mirror of global config". No `[agents.*]` or `[tasks.*]` setting
has ever been overridable per repository.

There is already one key that lives in `[repo."<path>"]` *without* being in the
shared schema: `trunk`, on `RepoOverrideConfig`. And there is already one place
pop writes repo-shaped state — `config.runtime.toml`, per ADR-0150, which also
holds the repo trunk. Between them, `trunk` is the precedent for both halves of
what this decision needs.

## Decision

1. **The cap is a `[repo."<path>"]`-only key, on `RepoOverrideConfig`** beside
   `trunk` — deliberately *not* on the shared `RepoScopeConfig`. Adding it to the
   shared schema would make it legal inside `.pop/config.toml` automatically, and
   pop artifacts should not have to be committed into a repository to bound that
   repository's drains. The in-repo home stays available for a later decision if
   sharing a bound with teammates ever becomes the point.
2. **One flat scalar, not a per-preset map.** The repository says how much work a
   task is worth; the **Agent adapter** says how to express that to its agent
   (ADR-0190). Repo scope is a flat curated key set today with no nesting, and
   under ADR-0190 a per-preset repo key would have exactly one meaningful entry.
3. **Pop writes the value to a new identity-keyed layer of `config.runtime.toml`,
   never to the user's `config.toml`** — ADR-0150's split, unamended. The
   hand-authored `[repo."<path>"]` block therefore always beats the pop-written
   value, which is the same ordering the six-layer `preferred_workbench` ladder
   already uses.
4. **Keyed by repository identity, not by exact checkout.** Every worktree of a
   repository reads one bound. This diverges from the runtime layer's existing
   keys — `[workbench.preferred]` and the repo trunk both key by exact checkout —
   and the divergence is the point: those describe a checkout, this describes a
   repository.
5. **`pop config repo set` is the only writer, and its settable keys are derived
   from the config schema by reflection**, as `repoScopeLegalKeys` already derives
   the readable ones. A curated set, not a general TOML editor: pop's config
   already carries an include whitelist, a reflection-generated repo-scope legal
   set, and findings-based validation of unknown keys, and a general setter would
   become a fourth answer to "which keys are real" that can drift from the other
   three.
6. **`repo`, not `project`, in the verb.** Pop's **Project** is a directory on
   disk it knows about; a monorepo with five worktrees is five projects and one
   repository, and this value follows the repository. `pop config project set`
   would be named for the wrong noun.
7. **`spend-audit` gains a config route, and recommends rather than runs it.** Its
   step-4 routing table sends fixes to repo instructions, attempt prompts, task
   sizing, or adapter capabilities — there is no route to a config change at all
   today, which is why an audit can currently only ever say "rewrite your prompt".
   The skill is already `disable-model-invocation: true`; a step that changes how
   much money every future drain may spend stays on the human's side of that line.

## Consequences

- Repo scope now has **two shapes**: keys shared by both homes
  (`RepoScopeConfig`) and keys legal only centrally (`RepoOverrideConfig`). That
  distinction existed for `trunk` alone and read as a one-off; it is now a
  deliberate pattern, and the reflection that generates legal key sets has to
  respect both.
- A bound set on one machine does not travel with a clone. That is the trade
  decision 1 makes on purpose, and the point at which someone will want the
  in-repo home.
- The runtime file now carries two keying conventions. Anyone reading
  `config.runtime.toml` must know which section keys by checkout and which by
  identity; the identity keying is also lossy in the way ADR-0083's already is —
  two config keys canonicalizing to one identity resolve non-deterministically.
- A checkout-keyed sibling of this layer was raised and deferred, not rejected:
  if a setting turns out to describe one worktree rather than the repository, it
  gets its own section rather than bending this one.
