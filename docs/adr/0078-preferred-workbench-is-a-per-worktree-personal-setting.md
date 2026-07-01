---
status: accepted
---

# Preferred workbench is a per-worktree personal setting that auto-applies

## Context

[ADR-0075](0075-workbench-apply-reconciles-by-pane-identity.md) added the opt-in
`[workbench] pick_on_create` prompt: on a new session with ≥1 resolved **Workbench**,
pop shows a quick-search list to pick one. Users want to skip the choosing — set a
**Preferred workbench** once and have it open automatically — and want that choice to
differ per worktree, with a sensible default inherited from the repo's trunk.

## Decision

A **Preferred workbench** is a personal, per-checkout choice of which Workbench
auto-applies when a session is born for that checkout.

- **Two homes, both personal; never `.pop.toml`.** Per-worktree values live in
  **Integration runtime config** (`config.runtime.toml`, `[workbench.preferred]`,
  **keyed by exact worktree path**), written by the picker keybinding or
  `pop workbench prefer`. A coarser per-repo default lives on a global
  `[repo."<path>"]` block as `preferred_workbench` (a `[repo]`-only key, not part of
  the `.pop.toml` RepoConfig schema). It is deliberately kept out of `.pop.toml`
  because session shape is personal taste, not committed team config.
- **Dynamic resolution, finest-first** ([ADR-0077](0077-config-precedence-is-scope-first-config-breaks-equal-scope-ties.md)):
  this worktree's runtime entry → the **Trunk worktree**'s runtime entry
  (inheritance) → the repo `preferred_workbench` default → none. Inheritance is
  resolved *at open*, not snapshotted at create: a child worktree with no entry of
  its own reflects trunk's *current* choice, so re-pointing trunk follows through to
  un-overridden children. Trunk is the existing Trunk worktree (non-bare git main
  worktree, or bare `trunk = true`); an unconfigured bare repo simply has no
  inheritance anchor and falls through.
- **Three-valued per worktree.** *unset* (inherit down the chain), *a name* (use it),
  or *explicit none* (flat/prompt here, overriding any inherited/repo default). This
  makes "opt one worktree out of the repo default" expressible without editing config
  — the runtime switch can always express use-X, use-nothing, or defer.
- **Auto-apply suppresses the prompt, orthogonal to `pick_on_create`.** A resolved
  preferred workbench applies silently on the create-path (project picker and
  worktree `ctrl+a` alike), whether or not `pick_on_create` is set. `pick_on_create`
  degrades to "when nothing is preferred, ask anyway." A one-off different shape is
  `pop workbench apply <other>` after the session is up — there is no open-time
  escape hatch.
- **Stale names skip and continue.** A stored name that no longer resolves to a real
  Workbench is skipped with a non-fatal warning (ADR-0054 style) and resolution
  continues down the chain — a broken preference never blocks getting into a session
  and never silently vanishes.
- **Surfaces.** `ctrl+w` in the project and worktree pickers opens a Workbench
  picker for the selected row's checkout and writes the runtime entry (sets the
  preference only; never applies to a live session). `pop workbench prefer`
  (`wb prefer`) is the standalone door into the same picker for the current checkout,
  with `<name>`/`--clear` non-interactive forms and shell completion over the
  resolved Workbench names. Deleting a worktree drops its `[workbench.preferred]`
  entry, matching how deletion already drops the History entry.

## Considered options

- **Repo-level single value only** (one preferred per repo, no divergence).
  Rejected: it can't express "worktree A prefers minimal, worktree B prefers
  full-dev," and it makes the picker keybinding pointless — a static config key would
  suffice.
- **Snapshot trunk's preference into the child at create time** instead of dynamic
  inheritance. Rejected: adds a create-time write and awkward edge cases (forking
  from a non-trunk base, or a bare `origin/x` ref with no worktree), and freezes
  children so re-pointing trunk no longer propagates. Dynamic resolution is one
  lookup chain reused everywhere a session opens.
- **A committed `.pop.toml` key.** Rejected: forces one person's session-shape taste
  onto the whole team.
- **A separate "Default worktree" interactive trunk-setter** (the sibling idea).
  Dropped: trunk stays config-only. Auto-detecting a bare repo's trunk was declined
  for the same reason [ADR-0035](0035-queue-schedules-one-representative-checkout-per-repository.md)
  rejected it — the `symbolic-ref HEAD` / folder-name heuristics misfire on real
  repos (no worktree on the default branch) and trunk carries the dangerous
  integration role, so pop keeps refusing to guess.
