# `pop config show` mirrors effective config, not a checkout predicate

`pop config show` prints a full mirror of pop's **effective configuration**, resolved from the current working directory: `includes` merged, `~` and symlinks canonicalized to absolute realpaths, folder-local overrides (`.pop.toml` and the current `[repo]` block) collapsed into effective values, and the current repo's resolved trunk — config-declared via `trunk = true` *or* git-derived (a non-bare repo's main worktree, which no file names) — surfaced as an effective `trunk`/`bare`. It emits **effective values only**, no per-value provenance. TOML by default (round-trippable), `--json` for machine consumers. Its boundary is config + git; it never reads the task-binding store. It is the value counterpart to `pop config keys` (the accepted schema): keys = what you may set, show = what is in effect.

The motivating consumer is the `to-tasks-here-and-now` guard, whose trunk check (`git rev-parse --git-dir == --git-common-dir`) misses a bare repo's linked config-trunk worktree. The correct trunk is config-driven (a global `[repo."<path>"] trunk = true` marker), and a skill cannot reliably read it in shell because of symlinked paths, `includes`, and scope rules. `config show` reuses pop's own resolver so the guard reduces to `realpath "$PWD"` compared against the resolved `trunk`.

## Considered Options

- **Full effective-config mirror (chosen):** one honest, independently-useful read-only command. The skill does a trivial `realpath`+compare rather than pop answering a predicate. Contains what the guard needs (the resolved trunk) as a byproduct of showing everything in effect.
- **Narrow checkout predicate (e.g. `pop config trunk` / `pop tasks probe-here`):** pop answers "is this checkout the trunk / adoptable?" directly with an exit code, removing the shell compare entirely. Rejected: the residual shell work under the mirror is only `realpath "$PWD"` — never the fragile part — so a bespoke predicate command earns little over a general command that debugging also wants.
- **Effective values with provenance annotation:** each value tagged with the winning layer (which include, `.pop.toml`, runtime, embedded default). Rejected for now: richer for debugging surprises, but the output stops being clean round-trippable TOML and the driver needs none of it. Can be added later behind a flag.

## Consequences

- `config show` reaches past literal config into git to derive a non-bare repo's trunk. This is deliberate — it mirrors *pop's effective view*, and pop's effective trunk includes the git default — but it means "show config" is a resolved view, not a file echo.
- The `to-tasks-here-and-now` guard keeps three checks: #1 trunk now consumes `pop config show --json`; #2 managed (a `queue/worktrees/` path-prefix test) and #3 bound (via `pop tasks status`) are unchanged and stay in the skill.
- The engine's directive-adoption path (`resolveNamedWorktree`) still adopts the trunk by basename, contradicting [ADR-0113](0113-to-tasks-here-and-now-binds-current-worktree-at-the-skill-layer.md)'s exclusion wording. That gap is deliberately left to a separate follow-up; `config show` is read-only and does not touch it.
