---
status: accepted
---

# Queue identity and integration target resolve from markers and config, not live git

> **Relates:** extends [ADR-0042](0042-queue-dashboard-scoped-to-task-storage.md) (dashboard discovery) and revisits its "the surviving projects take the existing git-resolution path unchanged" clause for both the dashboard and `Scan`.

## Context

ADR-0042 scoped dashboard discovery to repositories with **Task storage** intersected with config, but the surviving repositories still took the original git-resolution path, and `Scan` (powering `pop queue status` and the daemon) still expanded the whole registered fleet with a `git rev-parse` per project.

Measured on the `~/Dev/*/*` config (158 projects, 4 with task storage):

- `pop queue dashboard` cold build ≈ **470ms**, of which ≈ **350ms is git** in `resolveRepoStatic` (`rev-parse` per surviving candidate + `worktree list` per repo group), recomputed on every fresh launch (the cache is in-memory only). The per-tick overlay adds ≈110ms, dominated not by queries but by **reopening `pop.db` ~5× per row**: `bindingForSet` reloads the entire bindings table and is called three times per row; `mergeabilityForSet`/`readLock` reopen per row.
- `pop queue status` ≈ **3.3s**, almost entirely `git rev-parse` × 158.

The waste is structural. A repository's identity (key, definition/state paths, basename, bare-ness) is fully derivable from its canonical git **common directory** — `identityFromCommonDir` is a sha256 plus path ops, no git — and the common directory is already persisted in each repo's `repo.json` marker. Forking `rev-parse` re-derives what is already on disk.

The only thing that ever genuinely needed git was finding the **integration target** (where a set merges, and where the queue drains an unbound set headless) for a *bare* repository with no configured trunk — enumerating its worktrees. But the integration target is otherwise derivable with no git at all, and a bare repo without a configured trunk has no answer anyway. So once a bare repo is required to declare its trunk in config, **no case needs git for the static side**.

## Decision

The queue resolves its **static** coordinates from persisted markers and config, never from live git, on both read paths:

1. **Identity & paths from the marker.** Repo key, definition path, state path, basename, and bare-ness derive from the marker's recorded common directory — no `rev-parse`. ADR-0042's candidate match already pairs each config project to its repo by fork-free path nesting; that pairing is carried through instead of re-resolved with git.
2. **Integration target derived, not stored.** It is computed fork-free on every read: a **non-bare** repo's target is its main worktree, the **parent of the common directory** (`…/repo/.git` → `…/repo`); a **bare** repo's target is its **config trunk** (`[repo."…"] trunk = true`). No marker field, no staleness, no background refresher. A bare repo with no configured trunk surfaces a config-class error on its sets (ADR-0059's invariant), not a git fork.
3. **One database snapshot per build.** Each build opens `pop.db` once and reads `AllBindings`/`AllIntegrations`/`AllMergeability`/`RunningDrains` once into maps; per-row lookups become map accesses. This replaces the ~5-reopens-per-row churn and yields a consistent point-in-time view.
4. **Branch column without a fork.** A bound set's branch is already stored in its binding row. For an unbound set shown before its first drain, the branch is read from the integration target's `HEAD` as a file, or omitted — never `git branch --show-current`.
5. **`Scan` partitions fork-free.** A config project matching no storage marker is classified `idle / no tasks` from its name alone; only repositories with task storage take the marker-based path. `status`'s full-fleet listing is preserved; git is forked for none of it.

Mergeability for Done sets keeps its SHA-gated git in reconcile (ADR-0051/0055) — that is genuine merge math and off the common path.

## Considered options

- **Persist a representative checkout at registration + refresh it in a background goroutine** (this ADR's own first draft). Rejected: once a bare repo must declare its trunk in config, the integration target is derivable fork-free in *every* case (non-bare = parent of common dir; bare = config), so there is nothing expensive left to persist or refresh. The marker field and the refresher were solving a problem that derivation already solves.
- **Keep live git, cache harder across ticks** (ADR-0042's rejected option). Rejected again: amortizes but leaves the re-derivation in place and pays full cost on every cold launch.
- **Drop the config intersection, discover purely from storage.** Rejected: reopens ADR-0042's behavioral contract (de-registered repos reappear) for ≈13ms; the win is in eliminating git, not the cheap intersection.
- **Make `status` distinguish "registered but not a git repo."** Dropped: that verdict currently costs a `rev-parse` per project; a config entry without task storage is simply `idle / no tasks`. Misconfigured globs surface via `pop doctor`.

## Consequences

- Synchronous dashboard build and `status` drop from ~470ms / ~3.3s toward a file read plus one DB pass; git becomes a background-only, SHA-gated cost (mergeability) for repos with task storage.
- **Bare repos must declare `trunk` in config** to be queue-actionable; without it their sets show a config-class error rather than silently resolving. Non-bare repos need no trunk config — their target is derived.
- The marker stays a pure identity record (`repository_path` + `created_at`); this ADR adds no field to it.
- `status` no longer distinguishes a non-git config entry from an idle repo with no tasks; the full-fleet idle listing is otherwise unchanged.
- **Unrelated finding surfaced while measuring:** a test (`TestAbandonSuccessfulPreservesTaskStatus…`) writes into the real `pop.db` (28 of 32 set rows are temp-dir pollution). A test-isolation bug, tracked separately.
