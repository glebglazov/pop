---
status: accepted
---

# Issue sets live in pop's data dir, keyed per repository

> Supersedes [ADR 0003](./0003-personal-workloads-live-in-a-designated-worktree.md) and [ADR 0004](./0004-workload-target-references-resolve-from-cwd.md).

Issue sets move out of the repository tree into **Workload storage** in pop's data directory: `$XDG_DATA_HOME/pop/workloads/<repo-basename>-<short-hash>/issues/<id>/`, where the hash derives from the canonical git common directory path and a `repo.json` marker records the reverse mapping. All worktrees of one repository share one Workload storage; the runtime path remains per-checkout, so the definition/runtime two-path split survives with definition now per-repository and derived rather than configured. `pop workload show-path` prints the storage location and creates it on demand, serving humans (`cd`, `$EDITOR`) and planning skills alike. **Workload target references** become bare Issue set identifiers and Issue-set-relative file references (`<issue-set>/<file>.md`); all path forms are removed. The legacy `thoughts/issues/` location is retired by a one-shot, current-worktree-only `pop workload migrate`; the workload-gitignore integrate component and the Doctor ignore-coverage probe are deleted.

Pop has no gitignore surface at all: it never reads, writes, or verifies ignore configuration anywhere, because no pop-owned artifact lives inside a repository tree. Pop still respects git's own ignore semantics where git applies them (status, stash) — it just never manages them.

## Why

The in-tree location existed only behind an ignore guarantee: a per-machine git configuration step, an integrate component to install it, and a Doctor probe to verify it — all managing the single risk that planning artifacts leak into implementation commits. Storing artifacts in pop's own data directory deletes the entire failure mode instead of monitoring it: nothing can leak because nothing lives in the tree. The executor's stage-everything implementation commit becomes safe by construction rather than by ignore configuration.

Keying by repository rather than worktree matches the mental model — issues describe work on a project; a worktree is just where execution happens — and survives worktree churn, which is pop's core workflow. Hashing the canonical git common directory path follows the existing runtime-lock pattern and writes nothing into `.git/`.

With artifacts out of the tree, CWD-relative path targeting loses its anchor; editor-copied tree paths no longer exist. Bare identifiers with shell completion replace paths as the targeting vocabulary — completion makes the typing cost negligible, and one canonical issue-reference form (`<issue-set>/<file>.md`) serves arguments, completion, and status-table copy-paste hints uniformly.

## Considered Options

- **Keep `thoughts/` in-tree with ignore coverage (status quo).** Rejected: permanent configuration surface and probe machinery to manage a risk that relocation removes outright.
- **Key storage per worktree.** Rejected: deleting a worktree would silently orphan its issue sets; per-repository keying decouples planning artifacts from checkout lifecycle.
- **Identity file inside the git common dir.** Rejected: writes into git internals to survive the rare repo-move case; path hashing plus orphan reporting covers it without a new write surface.
- **Dual-mode discovery (data dir and `thoughts/`).** Rejected: a permanent two-resolution-path tax in every command, completion, and Doctor explanation — paid forever to avoid a one-time migration; same-ID collisions between locations have no good winner.
- **Explicit `init` before `path` works.** Rejected: an error-then-init dance for every consumer with nothing to protect — creating directories in pop's own data dir is harmless and idempotent.
- **Automatic GC of orphaned storage.** Rejected: a repository on an unmounted volume or moved path must not cost its issue history; Doctor reports orphans, deletion stays manual.

## Consequences

Planning skills resolve their write location via `pop workload show-path` instead of assuming `thoughts/issues/` under the CWD; agents writing outside their working directory may face sandbox approval, and the explicit path output makes that destination visible and approvable.

`pop workload migrate` moves sets from the current worktree only, rekeys workload-state entries preserving priority and registration order, errors-and-skips on ID collisions, removes `thoughts/` only when left empty, and never edits global ignore files. Repos with sets in several worktrees run it once per worktree; Doctor flags remaining legacy sets.

Storage directories are self-describing on disk (`repo.json`, readable directory names), so a lost or reset workload-state file is recoverable by walking the data dir. A moved repository or fresh clone is a new identity with empty storage — accepted; Doctor's orphan report covers relinking by hand.

Doctor replaces the ignore-coverage probe with a data-dir writability check, legacy `thoughts/issues/` detection, and orphaned-storage reporting.
