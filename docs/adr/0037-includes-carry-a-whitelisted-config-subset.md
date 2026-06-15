# `includes` carry a whitelisted config subset with parent-first precedence

Status: accepted

## Context

`config.toml` already supports an `includes` directive, but it merges **only** `projects`
from included files — every other section is silently dropped (locked in by a test,
`config_test.go` "included file scalar fields are ignored"). The motivating need is privacy:
a user keeps the directories they work on out of the main config, because both the `projects`
list and `[repo."<path>"]` **Repo override** blocks (whose keys *are* checkout paths) reveal
them. Repo overrides are exactly what includes does not carry today, so the feature as shipped
does not solve the problem it looks like it solves.

## Decision

An **Include** carries a fixed whitelist and resolves by a single precedence rule.

- **Whitelist, not deep-merge.** An include contributes only `projects` and `[repo."<path>"]`
  blocks. Every other section in an include file is ignored — and now **warned about** rather
  than silently dropped, since silent-drop is the confusion this feature exists to remove.

- **One precedence rule: parent first, then includes in listed order, first definition wins.**
  `projects` is a list, so includes append and existing expansion dedups. `Repo` is a map keyed
  by checkout path, so the same key can appear in multiple sources; the **first** source to
  define a repo key wins (parent beats any include; earlier include beats later), whole-block,
  and a collision emits a warning. This is deliberately the **opposite** of TOML's native
  duplicate-table last-wins and is whole-block rather than the field-level merge that
  `ResolveRepoConfig` uses for global-override → `.pop.toml` → default — the asymmetry is the
  surprising part worth recording.

- **Flat, one level.** An `includes` key inside an included file is ignored and warned about;
  no recursion, so no cycle detection or precedence tree.

- **Malformed is fatal; missing is a warning.** A parse error in an include fails `Load` loudly
  with the filename, matching how a malformed main `config.toml` already behaves. A missing
  include file stays a warn-and-skip, because that is the recoverable "moved my private file
  between machines" case.

- **Literal paths only.** `~` expansion and parent-dir-relative resolution as today; no globbing.
  With first-wins precedence now load-bearing, globs would make ordering depend on filesystem
  sort. Additive later if a `conf.d` need appears.

## Considered options

- **Full deep merge (include is a normal config, every field merges).** Rejected — invites
  precedence questions for scalars (`quick_access_modifier` in two files) with no concrete need,
  and a far larger blast radius than the privacy problem requires.
- **Field-level merge of colliding `[repo]` blocks.** Rejected — having `worktree_ready` from one
  file and `auto_merge_clean` from another for the *same* checkout is precisely the "where did this
  value come from" confusion. Whole-block + warning keeps provenance traceable.
- **Last-wins (match TOML / make includes override the parent).** Rejected — the parent
  `config.toml` is the file the user reads and controls first; an include should not silently
  override it. One first-wins rule for both parent-vs-include and include-vs-include is easier to
  state and predict than two directions.
- **Recursive includes.** Rejected — the use case is a few named sidecar files, one level deep;
  recursion adds cycle detection and a precedence tree for no need.

## Consequences

- **Glossary:** adds **Include** (under Configuration), relating it to **Project** and **Repo
  override** and distinguishing it from `.pop.toml`. Recorded live in a CONTEXT fragment.
- **Behavior change:** included files now contribute `[repo."<path>"]` blocks they previously
  dropped; the "scalar fields are ignored" test is superseded by per-section warning behavior.
- **No nested-include / no-glob constraints are deliberate scope cuts, additive if needed later.**
