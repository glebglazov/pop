# Dashboard history matching uses approximate session names for speed

The dashboard's `sessionAccessTime` function derives session names from history entry paths using `sanitizeSessionName(filepath.Base(path))` instead of `project.SessionName(path)`. The latter calls git commands (`rev-parse`, `config`, walking `.git` directories) to determine whether a path is a bare-repo worktree, a regular-repo worktree, or a plain directory. For a typical history of ~20 entries and a dashboard with ~10 panes, the git-based approach triggers 200+ subprocess invocations on every dashboard open, making the picker feel sluggish (≈1.8s).

## Why

`project.SessionName` is the single source of truth for session names — it correctly handles bare repos (`repoName/worktreeName`), regular repos, and non-git paths. However, it was designed for one-shot use (when a user actually selects a project), not for bulk derivation over every history entry on every dashboard render. The dashboard only needs session names for a fuzzy heuristic: "has this session been visited recently?" An approximate match is acceptable here; an exact match is required when creating or attaching to a session.

## The Approximation

`sanitizeSessionName(filepath.Base(path))` returns the directory base name with dots and colons replaced. For regular repos and non-git paths, this is identical to `project.SessionName`. For bare-repo worktrees, the exact session name is `repoName/worktreeName`, while the approximation yields only `worktreeName`. The dashboard falls back to last-component matching anyway (`lastComponent == worktreeName`), so the approximation only affects exact-match short-circuiting for bare-repo worktrees. In practice this means two bare-repo worktrees with the same worktree folder name but different repo names may share a session-last-visit timestamp in dashboard sorting. This is a minor ordering imprecision, not a functional bug.

## Considered Options

- **Keep exact `project.SessionName` (rejected).** Correct but too slow for a frequently-opened popup. Caching (`historyEntrySessionName` with a mutex-backed map plus parallel warm) brought the time from 1.8s to ~0.24s, but still slower than the pre-session-module ~0.05s.
- **Store `SessionName` in `history.Entry` at `Record()` time (deferred).** The cleanest fix — compute the name once when the user selects a project, then read it forever. Rejected for now because it requires a JSON schema migration and changes the `Record()` API (history cannot import `project` without a cycle). Revisit when the history format next changes.
- **Approximate string-only derivation in dashboard only (chosen).** Fast (`≈0.02s`), no schema changes, no new dependencies. The imprecision is isolated to dashboard sorting and documented here.

## Consequences

Dashboard pane ordering for bare-repo worktrees may be slightly less precise when multiple repos share a worktree folder name. The project picker and worktree picker continue to use exact `project.SessionName` for session creation, attachment, and kill — those are unaffected. If the history format ever gains a `SessionName` field, this ADR becomes obsolete and the dashboard should switch to the stored value.
