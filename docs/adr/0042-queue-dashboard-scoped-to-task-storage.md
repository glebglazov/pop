---
status: accepted
---

# The Queue dashboard discovers its repositories from Task storage, intersected with registered projects

> **Relates:** refines ADR-0035 (scheduling unit) for the read-only dashboard's discovery path

## Context

`pop queue dashboard` rebuilds its snapshot every 2 seconds (`dashboardPollInterval`). Each build called `BuildDashboard`, which expanded **every** registered picker Project from the config globs and resolved git coordinates for all of them — `git rev-parse` per checkout, then `git worktree list` and an uncached `git branch --show-current` per repo group.

On a machine whose config is `~/Dev/*/*`, that expands to **108 git repos** and forks roughly **350 git subprocesses per build** (measured ~2.5s wall-clock). But task sets live only in per-repository **Task storage** (`<pop data>/repos/<name>-<hash>/tasks/`, ADR-0035), and only **4** of those 108 repos had any. ~96% of the per-tick forking resolved repositories that can never contribute a row — every set they produced was filtered out at the end anyway (`dashboardShowRow`).

Two failures compounded: the poll interval (2s) was shorter than a build (2.5s), so reloads never idled and overlapped, pinning CPU; and `RunDashboard` ran the first build synchronously, so opening the dashboard blocked on a ~2.5s blank screen. The cost scaled with *registered projects*, not with *task sets* — the one quantity the dashboard actually renders.

## Decision

`BuildDashboard` discovers its candidate repositories from **Task storage intersected with registered projects**, not from the full project expansion:

1. Enumerate the storage markers under `<pop data>/repos/*/repo.json` (`tasks.ListTaskStorageRepos`) — the repositories that have task sets on disk. Each marker records the repository's canonical git **common directory**.
2. Keep a registered picker Project only when its checkout matches one of those repositories, tested by **canonical filesystem-path nesting** — the project is the repo's working-tree root, nests under it (a worktree), or contains it. This match resolves **no git**: the storage side is a JSON read, the project side is `EvalSymlinks`. The handful of surviving projects then take the existing git-resolution path unchanged.
3. As a companion saving on that hot path, the representative's branch is read from the `git worktree list --porcelain` already fetched (and per-build cached) while resolving the representative, instead of forking `git branch --show-current` per repository.

A repository that has task sets but is **no longer in the config globs is hidden** from the dashboard. The intersection is strict: the dashboard shows queue-actionable sets *for repositories you still register*, and nothing else.

This changes only the read-only dashboard's discovery. The supervisor's `Scan` is unchanged: it still expands all registered projects, because scheduling decisions (which repos are idle, which agents cool) are a property of the whole registered fleet, not only of repos that already hold task sets.

## Considered options

- **Discover from Task storage only, ignore config.** Rejected: a repository would surface on the dashboard purely because it once held task sets, even after you removed it from your project list — surprising, and it resurrects storage for repos you've deliberately stopped tracking. The config glob is the user's statement of what they care about; the dashboard honors it.
- **Show orphans (storage but not in config) with a marker.** Rejected for now: it keeps an in-flight drain in a de-registered repo visible, but at the cost of an extra rendering branch and rows the user didn't ask for. The escape hatch already exists — `pop queue status` and `pop queue log` read from storage and the journal directly, so a de-registered repo's live work is never truly lost, only absent from this view.
- **Keep the full scan, just cache identities/branches across ticks and load asynchronously.** Rejected as the primary fix: it leaves the structural waste (resolving 100+ repos that contribute nothing) in place and only amortizes it. Caching across ticks also trades a point-in-time snapshot for staleness. Scoping the work to what the view can render is the smaller, more honest change.
- **Match project to storage by git identity rather than path nesting.** Rejected: resolving each project's common dir is exactly the per-project `git rev-parse` fork the change exists to eliminate — it would reintroduce the cost. Path nesting over canonical paths is fork-free and covers the real layouts (a repo root registered directly; a bare repo's container whose worktrees expand as children).

## Consequences

- A dashboard build forks git only for repositories with task storage (≈ a handful), not for every registered checkout. On the `~/Dev/*/*` config the build dropped from ~2.5s to ~0.3s; the poll no longer overlaps itself and the initial open is no longer a multi-second blank screen.
- **Behavioral contract:** a repository dropped from the config globs disappears from the dashboard even if it holds task sets or a live drain. This is deliberate (see options); recover visibility either by re-registering the repo or via `pop queue status` / `pop queue log`.
- The residual per-build cost is now the config glob *expansion* (filesystem stats per registered directory), not git forking. If that becomes the next bottleneck it is an independent optimization, untouched here.
- The match is by canonical filesystem path. A storage marker's recorded common directory is canonical at write time; project paths are canonicalized (`EvalSymlinks`) before comparison, so the home-directory symlink layouts (`~/Dev` → `~/private/Dev`) match correctly.
- New read-only `tasks` surface: `tasks.ListTaskStorageRepos` enumerates repositories with task storage from their `repo.json` markers, reusing the marker-reading idiom of `FindOrphanedStorage`.
