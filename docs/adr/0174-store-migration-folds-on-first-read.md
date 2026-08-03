---
status: accepted
---

# Store migration folds on first read, and `sets` is tombstoned rather than dropped

## Context

Tickets 01, 03 and 07 of the *generalize-work* map change where Work state
lives. Ticket 03 replaces today's per-kind task-set registration with one
registry keyed `(kind, id)`; ticket 01 gives Maps a manifest, a registry row and
a pop.db-held claim; ticket 07 renames the supervisor's lock directory and
config section. Every one of those lands on machines that already hold live
data: registered Task sets with bindings and verify verdicts, Maps mid-
wayfinding, managed worktrees with uncommitted work in them.

Ticket 07 carved off its own share of the migration — the lock-dir handover and
the `[queue]` → `[work.daemon]` config cut. This decision covers the store
proper.

pop already has two migration idioms and uses both:

- **Silent fold on first read.** `foldLegacyStateFile`, `migrateLegacyBindingsFile`,
  `migrateLegacyAgentCooldownFile` and `MigrateStorageLayout` all run
  unconditionally on an ordinary read path, move the data, and delete the legacy
  artifact.
- **Explicit one-shot verb, surfaced by doctor.** `pop tasks migrate` and
  `pop routine migrate-manifests`.

pop.db schema changes are a third, separate mechanism: a forward-only
append-only `migrations` list gated by `PRAGMA user_version`, applied inside one
immediate transaction on every `store.Open` (currently at 25).

The deployment reality matters to the trade-off: pop is a personal tool running
on a small number of the author's own machines. A documented wipe-and-
re-register was a live option.

## Decision

**1. Fold on first read. No `pop work migrate` verb, no wipe.**

Not because the data is precious, but because a fold is the idiom this codebase
already pays for four times over, while a verb costs discovery, a doctor
finding, and documentation. Wipe-and-re-register is the wrong end of the
trade: re-registering loses `bindings`, `verify_verdicts` and auto-drain bits
that no human can reconstruct from disk.

**2. A new `work_containers` table, and `sets` is tombstoned, not dropped.**

Migration #26 creates `work_containers`, keyed `(kind, id)`, and copies every
`sets` row into it as `kind='task-set'`. Adding a `kind` column to `sets` was
rejected: a table named for one kind cannot be the cross-kind registry without
lying, and ticket 07 is already paying to delete lying names.

`sets` is **not dropped in the same migration**. It becomes read-dead and
write-dead the moment #26 lands — no code path touches it again — and a
CLEANUP.md entry records the drop for the existing beta-tester-sign-off gate.
Keeping it costs nothing and buys one real property: an older binary still boots
afterwards, because its migrate loop is `for version < len(migrations)` with
`len == 25`, so a `user_version` of 26 is a no-op and it reads its own rows.
Data written post-cut is invisible to it, but it does not crash.

Dual-writing the two tables is explicitly rejected — two sources of truth that
diverge silently is the failure `work_containers` exists to end.

**3. Column split: `archived` is cross-kind; the rest are kind-local.**

`work_containers` carries `archived`, which Maps now need too (see 4).
`priority`, `auto_drain`, `worktree_managed` and `worktree_name` move to a
task-set-side table keyed by the registry id. A per-kind JSON blob on the
registry was rejected: `auto_drain` is a daemon candidate filter, and JSON in
SQLite turns a column read into a table scan plus a parse.

**4. `wayfinder-archive.json` is absorbed into the registry.**

Maps have exactly one state file today — a JSON list of archived map ids in the
repository's storage dir. It folds into the registry's `archived` bit and the
file is deleted, in the shape of `migrateLegacyBindingsFile`. Once Maps
register (ticket 01 §6), a separate archive side-file is indefensible.

**5. Existing Maps get their manifest by fold, and their ticket headers are
stripped.**

`wayfinder/scan.go` gains a fold guarded by "no `index.json` in this map dir":
mint the manifest from the `Status:` / `Type:` / `Blocked by:` lines the parser
already reads, write the registry row, and **remove those header lines from each
ticket markdown**. This is the one fold that edits the human's content files
rather than pop's own side-files. Leaving the headers as dead text was rejected:
two sources for one status is exactly the drift ticket 01 §3 charged validation
with policing.

A ticket sitting at `Status: claimed` when the fold runs **drops to open**.
Ticket 01 §5 moves claims to pop.db and the file format records no owner, so a
synthesized claim would be an unattributable lock with a ~4h TTL that no session
can release by identity.

**6. Routines migrate nothing.**

Ticket 06 makes routine membership discovered rather than registered, so there
is no registry backfill and no row to hang pause state on. `state.json`
(`bound_directory`, `paused`, `pause_reason`, `created_at`) stays exactly where
it is. Accepted asymmetry: Routines become the one kind whose machine-local
runtime is not in pop.db.

**7. The storage directory renames with the verb: `wayfinder/` → `maps/`.**

Folded on first read, in the shape of `MigrateStorageLayout`'s `issues/` →
`tasks/`. Ticket 02 renamed `pop wayfinder` to `pop map`; leaving the directory
named `wayfinder/` means the skill overlay and the Work-store doc keep saying
"wayfinder" about a path every verb calls a map.

**8. `<data>/queue/worktrees` moves to `<data>/work/worktrees`, gated on
quiescence.**

Ticket 07 renamed the lock directory but not its sibling, which holds every
managed worktree. Three things point into that path: `bindings.runtime_path`
rows (absolute), each worktree's `.git` file, and the repo's
`.git/worktrees/<name>/gitdir` admin files.

The move rewrites matching `bindings.runtime_path` prefixes in one transaction
and runs `git worktree repair` per affected repository. Because this is the only
migration on the map that can destroy uncommitted work if it half-completes, it
**refuses outright if any managed worktree is dirty or has a live drain**,
naming which. That turns the destructive case into a no-op the human resolves,
and pop already knows both facts. With the gate, the fold is safe to run
unconditionally like the others.

Rejected: leaving `queue/worktrees` in place (a directory named `queue` after
`pop queue` ceases to exist — the lying name again), and renaming the constant
for new worktrees only (two worktree roots alive indefinitely, draining only as
old sets are abandoned).

**9. Migration is not a slice set.**

Each piece rides the slice that creates the thing it migrates: #26 and the
`sets` copy ride ticket 03's registry slice; the manifest fold, the `maps/`
rename and the archive absorption ride ticket 01/02's `pop map` set; the
worktree move rides ticket 07's lock-dir slice. Each fold writes its own
CLEANUP.md entry in the same commit, so the tombstone list cannot drift from the
code.

## Consequences

- No user-visible migration step anywhere. An upgraded binary folds on the first
  ordinary read, except the worktree move, which can refuse and name what to fix.
- One irreversible pop.db migration (#26). Rollback to a pre-cut binary keeps
  working but sees a frozen snapshot of the data as of the cut.
- CLEANUP.md gains entries for: dropping `sets`, and each fold's removal.
- Routine runtime state stays split across `state.json` and `routine_runs`,
  unlike every other kind.
- The Map fold rewrites content files, establishing a precedent this codebase did
  not previously have.
- **As landed:** the registry table arrived as migration #26, with the slice that
  made Maps register; the `sets` copy and the task-set-side table
  (`task_set_registrations`, keyed by the registry row's `seq`) arrived as #28.
  Decision 2 is otherwise unchanged — the schema list is append-only, so a
  migration that had already shipped could not grow the copy. Because the registry
  keys `(kind, id)` and carries no `def_path`, one set id registered under two
  repositories collapses to its earliest registration: the same machine-wide
  uniqueness of a set id that `recovery_waiters` has assumed since ADR-0100.
- **As landed (decision 8, the worktree move):** the fold hangs off the funnel every
  keyed binding accessor already goes through, beside the `bindings.json` migration,
  so no verb owns it and the steady state costs one stat. Three details the decision
  did not settle. *Two roots for the length of a refusal:* a worktree that has not
  moved yet is still pop-managed, so the predicates that classify a checkout
  (`Provisioned`, the drain-target picker's exclusion, the Project picker's walk)
  read a two-element root list, not the one provisioning root. Only provisioning is
  single-rooted, which is what "no two roots alive indefinitely" was protecting. *A
  refusal needs a reader:* the fold runs silently, so `pop doctor` gained a
  `managed worktree root` check over a read-only inspection that names the same
  offenders — the fold itself never reports to a human. *Crash between the renames
  and the transaction self-heals:* the recorded-path rewrite is a blanket prefix
  rewrite rather than one per moved directory, so worktrees a killed run had already
  relocated are repointed by the next one. An error, as opposed to a crash, moves
  the directories back before returning.
