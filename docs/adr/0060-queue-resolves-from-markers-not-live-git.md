# Queue identity and representative resolve from persisted markers, not live git

Status: accepted — extends [ADR-0042](0042-queue-dashboard-scoped-to-task-storage.md) (dashboard discovery) and revisits its "the surviving projects take the existing git-resolution path unchanged" clause for both the dashboard and `Scan`.

## Context

ADR-0042 stopped the queue dashboard from forking git for every registered checkout by scoping discovery to repositories with **Task storage** intersected with config. But the handful of surviving repositories still took the original git-resolution path, and `Scan` (which powers `pop queue status` and the daemon) was left expanding the **whole** registered fleet with a `git rev-parse` per project.

Measured on the `~/Dev/*/*` config (158 projects, 4 with task storage):

- `pop queue dashboard` cold build ≈ **470ms**, of which ≈ **350ms is git** in `resolveRepoStatic` (`rev-parse` per surviving candidate + `worktree list` per repo group), recomputed on every fresh launch (the cache is in-memory only). The per-tick overlay is another ≈110ms, dominated not by queries but by **reopening `pop.db` ~5× per row** — `bindingForSet` reloads the entire bindings table and is called three times per row; `mergeabilityForSet`/`readLock` reopen per row.
- `pop queue status` ≈ **3.3s**, almost entirely `git rev-parse` × 158.

The waste is structural: a repository's identity (key, definition/state paths, basename, bare-ness) is fully derivable from its canonical git **common directory** with no git at all (`identityFromCommonDir` is a sha256 plus path ops), and the common directory is already persisted in each repo's `repo.json` marker. Forking `rev-parse` re-derives what is already on disk. The only static fact that genuinely needs git is the **representative** checkout for an *unconfigured bare* repository; everything else is either derivable for free or is live state (branch, locks) read from elsewhere.

## Decision

The queue resolves its **static** coordinates from persisted markers, not live git, on both read paths:

1. **Identity & paths from the marker.** Repo key, definition path, state path, basename, and bare-ness derive from the marker's recorded common directory — no `rev-parse`. The ADR-0042 candidate match already pairs each config project to its repo by fork-free path nesting; that pairing is carried through instead of re-resolved with git.
2. **Representative persisted at registration.** The representative checkout is resolved once when a repo's Task storage is first created and stored in the marker. The synchronous build reads it; it never forks git to find it.
3. **Git moves to an async background refresher.** A periodic background check re-resolves the representative and the displayed branch and writes them back to the marker / in-memory cache. The synchronous build — cold start and every poll — forks **zero** git and reads last-known-good. Drawing never blocks on git.
4. **One database snapshot per build.** Each build opens `pop.db` once and reads `AllBindings`/`AllIntegrations`/`AllMergeability`/`RunningDrains` once into maps; per-row lookups become map accesses. This replaces the ~5-reopens-per-row churn and yields a consistent point-in-time view.
5. **`Scan` partitions fork-free.** A config project that matches no storage marker is classified `idle / no tasks` from its name alone (no git); only repositories with task storage take the marker-based path. The full-fleet listing `pop queue status` shows is preserved, but git is forked only for the repos that can actually contribute scheduling work.

Mergeability for Done sets keeps its SHA-gated git in reconcile (ADR-0051/0055) — that is genuine git (merge math) and off the common path.

## Considered options

- **Keep live git, just cache harder (ADR-0042's rejected "cache across ticks").** Rejected again: caching amortizes but leaves the structural re-derivation in place and still pays full cost on every cold launch; the marker already holds the truth.
- **Derive the representative on read, persist only for the unconfigured-bare case.** Viable and minimal, but persisting it uniformly at registration keeps the read path single-shaped (always read the marker) at the cost of a staleness story — accepted, handled by the background refresher.
- **Drop the config intersection and discover purely from storage.** Rejected: that reopens ADR-0042's behavioral contract (de-registered repos with tasks would reappear) for ≈13ms — the win is in eliminating git, not in dropping the cheap intersection.
- **Make `status` distinguish "registered but not a git repo."** Dropped for now: that verdict currently costs a `rev-parse` per project; a config entry without task storage is simply `idle / no tasks` whether or not it is a git repo. Misconfigured globs surface via `pop doctor`.

## Consequences

- **Staleness window.** The representative and branch are last-known-good between background refreshes; a just-moved trunk or just-switched branch shows stale until the next refresh, never blocking the draw. The background check is the only thing that re-syncs them.
- **Marker format grows.** `repo.json` gains the persisted representative; existing markers (written before this change) lack it and are backfilled lazily/on first resolve. The marker becomes a small cache, not just an identity record.
- **`status` behavior.** A config entry that is not a git repo is no longer distinguished from an idle repo with no tasks. The full-fleet idle listing is otherwise unchanged.
- **Targets.** Synchronous dashboard build and `status` drop from ~470ms / ~3.3s toward a file read plus one DB pass; git becomes a background-only cost incurred for repos with task storage.
- **Unrelated finding surfaced while measuring:** a test (`TestAbandonSuccessfulPreservesTaskStatus…`) is writing into the real `pop.db` (28 of 32 set rows are temp-dir pollution). A test-isolation bug, tracked separately.
