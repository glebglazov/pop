# CLEANUP — deprecation removal (backward-incompatible)

Single-phase removal of every deprecated alias, key, and migration. Planned for the
week of 2026-06-08, after beta-tester sign-off.

## Decisions (agreed 2026-06-06)

1. **Gate: beta-tester sign-off, not a version.** The repo has no tags or release
   scheme; "remove after v1.0" / "next major release" comments were promises with no
   trigger. Removal happens when every beta tester completes the sign-off checklist
   below. Per-tester confirmation is tracked in this file.
2. **Removed config keys become hard errors, not silent no-ops.** TOML drops unknown
   keys silently — a stale config would otherwise lose settings without any signal.
   After removal, an old key in the config makes load fail with a message naming the
   replacement. The tombstone detection (struct field + check) stays one cycle and is
   deleted in a later pass.
3. **Single phase — data migrations are removed too.** Beta testers update
   frequently; stranded data fails loudly enough (missing task sets) to diagnose.
   This makes the sign-off checklist's per-repo verification mandatory.
4. **`[workload]` config section is renamed *and restructured* to `[tasks]`
   (ADR-0092).** Supersedes the earlier assumption of a flat `[workload]`→`[tasks]`
   rename: the same move re-parents the internals into verb-named phase sub-tables
   (`[tasks.implement].agents`, `[tasks.verify]`), renames `[workload.git]`→`[tasks.git]`
   and the per-preset map `[workload.agents.<name>]`→`[tasks.presets.<name>]`. Unlike the
   original "never deprecated → straight to hard-error" plan, `[tasks]` ships first with
   the whole `[workload]` tree kept as an **honored read-compat alias + load-time warning**
   (matching every other renamed key); this cleanup then flips `[workload]` to a hard-error
   tombstone. One breaking cycle at removal, not at introduction.
5. **Stale installed integration artifacts are the tester's migration step.**
   Pre-rename artifacts send old status names; the opencode plugin swallows errors
   (`.catch(() => {})` in `cmd/extensions/opencode/pop-status-sync.ts:22`), so a
   stale hook fails *silently* after alias removal. Sign-off therefore includes
   re-running `pop integrate` for every agent.

## Removal inventory

### A. CLI aliases (cobra errors loudly after removal — no tombstones needed)

| Item | Location | Action |
|---|---|---|
| `pop select` hidden alias | `cmd/project.go:52-57` (`Hidden: true`) | Delete command + registration |
| `pop select` compat path note | `cmd/project.go:35` | Delete comment/path |
| Top-level `dashboard` hidden alias | `cmd/dashboard.go:29-35` | Delete; canonical is `pop monitor dashboard` |
| Top-level `unread` hidden alias | `cmd/unread.go` (whole file) | Delete file + registration |
| Worktree compat path | `cmd/worktree.go:25` | Delete deprecated path |

### B. Config keys (remove read-compat; add hard-error tombstone per decision 2)

| Old key | New key | Location |
|---|---|---|
| `[select]` section | `[project]` | `config/config.go:131` (`Select *ProjectConfig`), alias resolution ~`config.go:268,289`, warning `config.go:359` |
| `exclude_current_dir` | `exclude_current_session` | `config/config.go:125` |
| `dismiss_attention_in_active_pane` | `dismiss_unread_in_active_pane` | `config/config.go:41-43`, warning `config.go:367` |
| `attention_notifications_enabled` (in `[worktree]`, `[project]`, `[select]`) | `unread_notifications_enabled` | `config/config.go:81,90`, warnings `config.go:374-380` |
| `current_pane_always_under_cursor` | `cursor_position` | `DashboardConfig`, resolution ~`config.go:223` |
| sort value `pane_last_visit_at` | `pane_last_active_at` | `config/config.go:69` (`SortByPaneLastVisitAt`) |
| `[workload]` section (whole tree) | `[tasks]` | `config/config.go` (`Task *TaskConfig \`toml:"workload"\``) — see restructure rows below (ADR-0092) |
| `[workload] default_agents` | `[tasks.implement].agents` | `TaskConfig.DefaultAgents`; resolution `ResolveDefaultAgentPresets` (`tasks/agent.go:551`); includes-merge `config.go:1857` |
| `[workload.verify]` | `[tasks.verify]` | `TaskConfig.Verify`; includes-merge `config.go:1890` |
| `[workload.git]` | `[tasks.git]` | `TaskConfig.Git`; includes-merge `config.go:1880` |
| `[workload.agents.<name>]` (per-preset map) | `[tasks.presets.<name>]` | `TaskConfig.Agents`; includes-merge `config.go:1867` |
| `[queue]` section (whole table) | `[work.daemon]` (`poll_interval`, `agent_quota_retry_after`, `crash_retry_delays`) | `retiredQueueSectionFindings` in `config/config.go` — the finding that reports a leftover `[queue]` as an unknown section (and still points `agents` at `[tasks.implement].agents`). A **hard cut, not an alias**: nothing reads `[queue]`, so removal here means dropping the finding, after which the table is silently ignored. |
| includes whitelist enumerates `workload` | accept both `workload` (deprecated) + `tasks` | `config/config.go:1840` |

Tombstone behavior: presence of any old key → config load fails with
`"<old key> was removed; use <new key>"`. Implementation keeps the old struct
field solely for detection; mark each with a comment `// Tombstone: delete after
<date/condition>` so the second-phase delete is greppable.

Note: `[tasks]` and its restructured sub-tables ship with `[workload]` kept as an
honored read-compat alias first (ADR-0092); this cleanup flips `[workload]` to the
hard-error tombstone. README must document `[tasks.implement].agents`,
`[tasks.verify]`, and `[tasks.presets.<name>]` from the alias-removal commit.

### C. Status aliases (sent by integration hooks)

| Old | New | Location |
|---|---|---|
| `needs_attention` | `unread` | `monitor/monitor.go:25-35` (`legacyStatusNeedsAttention`), normalization ~`monitor.go:174-175`; help text `cmd/pane.go:440-441` |
| `idle`, `read` | `clear` | same locations (`legacyStatusIdle`, `legacyStatusRead`) |

After removal, `pop pane set-status` with an old name must exit non-zero with a
message naming the new status (loud-failure preference). Current embedded templates
(`cmd/extensions/{opencode,pi}/pop-status-sync.ts`) already send new names — only
*installed* stale artifacts are affected; covered by sign-off re-integrate step.

### D. Data migrations (removed per decision 3)

| Item | Location | Notes |
|---|---|---|
| In-tree migration `thoughts/issues/` → Task storage | `tasks/migrate.go` (+ `migrate_test.go`), `RenderMigrate` in `tasks/notices.go:9` | Also remove the command that invokes it |
| Storage-layout auto-migration `workloads/` → `repos/`, `issues/` → `tasks/`, global state → per-repo | `tasks/migrate_layout.go` (+ `migrate_layout_test.go`) | Auto-runs per-repository on first touch — see sign-off item 3 |
| Legacy global state path | `tasks/state.go:40-55` (`DefaultStatePath`, `DefaultStatePathWith` → `workloads-state.json`) | Only consumer is `MigrateStorageLayout`; delete together |
| `prds/` directory — full retirement | pending the PRD co-location feature (ADR-0088) | Co-location moves PRDs to `tasks/<set>/prd.md` and ships a `prds/<slug>.md` → set-folder migration. This cleanup **fully retires the `prds/` directory**: remove the sibling `prds/` read-path, the to-prd/to-tasks fallbacks, and the migration itself, once every repo's PRDs have moved. Verify no `<data-dir>/pop/**/prds/` remain (mirror of the `workloads/` storage check). Blocked on ADR-0088 landing first. |
| Map manifest fold + `wayfinder/` → `maps/` storage rename | `wayfinder/fold.go` (+ `fold_test.go`); the legacy-name probe in `tasks/storage_doctor.go` (`storageHasDashboardWork`) | Auto-runs per Map on the first scan that finds no `index.json`: mints the manifest from the retired `Status:` / `Type:` / `Blocked by:` header lines, strips those lines from each ticket markdown, and renames the storage directory. Removing it also deletes `StripTicketHeaders`, the header walk it shares with `ParseTicketMarkdown`, and the ticket-status/type/blocking parsing in `wayfinder/parse.go` — after removal a Map without a manifest is simply BROKEN. Sign-off check: item 6 below. |
| `wayfinder-archive.json` → registry `archived` bit | `foldLegacyArchiveState` in `wayfinder/archive.go` (+ `archive_test.go`) | Auto-runs per repository on the first Map scan that finds the side-file: registers each id it names and sets the registry's `archived` bit, then deletes the file. Removing it also deletes `legacyArchiveState` and the `legacyArchiveStateFile` constant — after removal a leftover file is ignored and its Maps come back visible. Sign-off check: item 6 below. |
| Tombstoned `sets` table — drop it | migration list in `store/store.go` (#8/#9 create it, #28 copies it out) | Read-dead and write-dead from #28, which copies every row onto the **Work container registry** (`work_containers` + `task_set_registrations`) and never dual-writes. Kept only so a pre-cut binary still boots: its migrate loop is bounded by its own migration count, so the newer `user_version` is a no-op and it reads its own frozen rows. Dropping it is a `DROP TABLE sets` appended as a new migration (never an edit to a shipped one) plus deleting `legacySetRows` in `store/sets_test.go` and the two `sets`-seeding test helpers. After the drop, rolling back to a pre-cut binary loses every registration — so this row goes last, once no tester needs the rollback. Sign-off check: item 7 below. |
| Pre-cut supervisor lock path check — **delete one release after the cut** | `LegacySupervisorLockPath` + `refuseIfLegacySupervisorLive` in `supervisor/supervisor_lock.go` (+ `TestAcquireSupervisorLockRefusesLivePreCutDaemon`) | The supervisor lock moved from `<data>/pop/queue/supervisor.lock` to `<data>/pop/work/supervisor.lock` with the `pop queue` → `pop work` cut. Unlike the **Work journal** (a view rebuilt from SQLite), a running pre-cut daemon holds the old path and nothing else, so a post-cut binary reading only its own path would double-supervise. Startup therefore reads both and refuses if either is live, naming the file held. One release later no pre-cut daemon can still be running: delete the legacy path helper, its check and its test, leaving the single-path acquire. Nothing migrates or is deleted on disk — the old file belongs to its owner. Sign-off check: item 8 below. |
| Managed-worktree root move `queue/worktrees` → `work/worktrees` | `tasks/binding/worktree_root_move.go` (+ `worktree_root_move_test.go`), `LegacyManagedWorktreesRoot`/`ManagedWorktreeRoots` in `tasks/binding/binding.go`, `store.RewriteBindingRuntimePathPrefix`, `doctorManagedWorktreeRootCheck` in `cmd/doctor.go`, `drain.LegacyQueueDataDir` | Auto-runs on the first binding read or write that finds a `queue/worktrees` directory: relocates each managed worktree, repoints every `bindings.runtime_path` in one transaction, runs `git worktree repair` per repository, then deletes the emptied legacy root. **Gated** — a dirty worktree, a live drain or an occupied destination refuses the whole move and names itself, so removal is only safe once every machine's move has actually run, not merely once the code shipped. Removing it also collapses the two-root reads back to one: `ManagedWorktreeRoots` → `ManagedWorktreesRoot` in `checkoutUnderManagedRoot`, the drain-target picker's exclusion (`pathUnderAny`) and `cmd/project.go`'s `ManagedWorktrees`, plus the doctor check and its seam. Sign-off check: item 9 below. |
| Legacy `bindings.json` → store migration | `migrateLegacyBindingsFile` (moving to `tasks/binding` in the store-seam refactor; currently `tasks/bindings_store.go:101`) | One-time fold of the retired standalone binding file into the execution-state store (ADR-0055). Every machine that ran a post-ADR-0055 build has migrated; sign-off check: no `<data-dir>/pop/bindings.json` remains. |

### D2. Internal code aliases (compile-time only, no user impact — remove in a quiet pass)

| Item | Location | Notes |
|---|---|---|
| `queue.WorktreeBinding` type alias | `queue/state.go:15` (`= binding.Binding`; becomes `= store.Binding` after the store-seam refactor) | Kept only to avoid churning 30+ test files in the refactor that deleted the mirror types. Flip call sites to `store.Binding` and delete the alias. |
| `work.SetStatus` + the `tasks.TaskSetStatus` alias | `work/cell.go`, `tasks/status.go` | The Work container carries the Task-set cells the dashboard columns show, including `RawStatus`, so the seam must name that status type without importing the kind that owns it (ADR-0173). Deleted when those cells become the Task-set kind's own columns: move the type back into `tasks` as a plain `type TaskSetStatus string`. |
| Task-set cells on `work.Container` | `work/kind.go` (the Task-set cell block), `work/cell.go` (`DestKind`), `work/derive.go`, `tasks/work_row.go` | `Row` and `SetRef` are gone — a row is the container — but the container still carries the cells the Task-set columns show (the verified-at pair, `Worktree`/`DestKind`, `RawStatus`, the Map tallies) because the dashboard has one column set and every kind fills it. A page now takes its *headers* from its primary kind, and each page projects a container onto its own cells queue-side; these fields go when a kind authors the cell *values* too, so the projection stops reading container fields another kind leaves blank. |
| Legacy-row fallbacks in the queue-side kind resolver | `queue/work_rows.go` (`workKinds.kindFor`), `queue/dashboard.go` (the shell verb's `RuntimePath` fallback) | A row built by a pre-seam builder names no kind and carries no `Checkout`, so the resolver defaults it to the Task-set kind and the shell verb falls back to the Task-set binding. Both go when every row comes from `Kind.Load`. |
| Queue-side composition of the Task-set Work kind | `queue/advance.go` (`Deps.TaskSetKind`, `taskSetKind`) | The read half is `tasks/setkind`, the advance half is here, because the drain pipeline it drives (scan, deferrals, routing, spawn) is in `queue` (ADR-0176). Folds into one adapter beside the kind when the queue package split moves that pipeline. |
| Two Work-kind wiring lists | `tasks/drain/kinds.go` (`Deps.WorkKinds`, `Deps.RoutinePageKinds`) | Page B is wired separately because `WorkKinds` also feeds the supervisor's advancers and page A's table, neither of which wants a Routine folded in. `pop work status` now prints one table per list, so the surface that wanted them apart no longer does: collapse to one list — and drop the supervisor's appended routine advancer with it — when the dashboard's two pages can select their own kinds out of it. |
| `spawnedSetsReporter` hook | `queue/advance.go`, `queue/supervisor.go` | The Task-set run-output diff needs the sets a tick spawned, and the diff is deliberately kind-local (ADR-0176), so the supervisor type-asserts for it instead of the seam growing a snapshot type. Revisit when structured per-advance events carry the same fact. |

### E. Cross-references that go stale (fix in the same change)

| Item | Location | Action |
|---|---|---|
| Doctor "auto-migrated on next tasks command" message | `cmd/doctor.go:391-414` | Message becomes a lie once migration is gone. Repurpose: detect leftover `workloads/` dirs and warn "stranded pre-rename storage; migrate by hand" — or delete the check |
| README documents `[workload.agents.claude]` | `README.md:27` | Rewrite to `[tasks.presets.claude]` (per-preset map renamed, ADR-0092) |
| Smoke script carries retired name | `scripts/live-workload-agent-smoke.sh` (referenced at `README.md:81`) | Rename script + README reference |
| CONTEXT.md "Deprecated aliases" section | `CONTEXT.md:412` | Prune removed aliases after cleanup lands — glossary describes current language; git history keeps the past |
| CONTEXT.md `pop select` entry says "remove at the next major release" | `CONTEXT.md:12` | Already updated to point here |
| Dangling version-gate comments | `config/config.go:125` ("after v1.0"), `config.go:131` ("next major release") | Deleted along with the fields |

### F. Out of scope (checked, intentionally untouched)

- Hidden `monitor-start` / `monitor-stop` / `monitor-status` / `pane set-status`
  commands — internals, not deprecations (`cmd/monitor.go:44-318`, `cmd/pane.go:470`).
- Embedded skills — old names (`to-issues`, `run-one`) already absent from
  `cmd/skills/`; only installed copies matter (sign-off re-integrate).
- `config.example.toml` — grep found no deprecated names.

## Beta-tester sign-off checklist

Per tester, before removal lands:

1. **Config migrated** — no `[select]`, `exclude_current_dir`,
   `dismiss_attention_in_active_pane`, `attention_notifications_enabled`,
   `current_pane_always_under_cursor`, `pane_last_visit_at` in
   `~/.config/pop/config.toml`. (`pop` currently prints warnings for these on load —
   "no warnings at startup" is the check.)
2. **`[workload]` → `[tasks]`** — once the new key ships, rename the section.
3. **Task storage migrated in *every* repo** — auto-migration runs per-repository on
   first touch. Run `pop tasks status` in each repo that has task sets. Verify: no
   directories left under `<data-dir>/pop/workloads/` and no
   `<data-dir>/pop/workloads-state.json`. (`pop doctor` reports leftover pre-rename
   storage.)
4. **Re-integrated every agent** — re-run `pop integrate <agent>` for each installed
   integration (claude / opencode / pi / codex) so installed hooks send
   `unread`/`clear`/`working`, not `needs_attention`/`idle`/`read`.
5. **No scripts call old CLI names** — `pop select`, top-level `pop dashboard`,
   `pop unread`, old status names in any personal automation.
6. **Every Map folded** — the folds run per Map on first scan. Run `pop map status`
   in each repo that has Maps. Verify: no `<data-dir>/pop/repos/*/wayfinder/`
   directory and no `<data-dir>/pop/repos/*/wayfinder-archive.json` remains, every
   Map folder carries `index.json`, and no ticket markdown still carries a
   `Status:` / `Type:` / `Blocked by:` header. A Map the fold declined (a blocker
   naming no ticket, a ticket with no `Type:` line) needs fixing by hand before
   sign-off — after removal it reads as BROKEN. An archived Map must also show
   `[archived]` under `pop map status --all`, proving its bit reached the registry
   before the side-file went. `Status: done` in a `map.md` is a **hard cut with no
   fold**: such a Map shows as `BROKEN` with the fix on its row, and the line must
   be edited to `arrived` (or the Map arrived through `pop map arrive`) by hand.
7. **Every Task set on the registry, and no rollback pending** — the copy runs once
   per machine, when pop.db reaches migration #28. Run `pop tasks status` in each repo
   that has task sets and confirm every set still shows with its priority, its
   auto-drain mark and its worktree binding, and that `pop tasks status --archived`
   still lists the archived ones. A set id registered under two repositories collapses
   to one registry row (the registry keys `(kind, id)`, with no def_path): check for a
   set that vanished from one repo's status table and re-register it under a distinct
   id. Then confirm you have no reason left to run a pre-#28 binary — dropping `sets`
   removes the frozen snapshot that made rolling back survivable.

8. **No pre-cut daemon, and `[queue]` gone from config** — stop any `pop queue run`
   daemon started by a pre-cut binary, then start `pop work daemon` once and confirm it
   acquires the lock rather than refusing (a refusal names the pre-cut lock file it
   found live). Verify no `<data-dir>/pop/queue/supervisor.lock` remains and that
   `~/.config/pop/config.toml` has no `[queue]` table — the keys moved to
   `[work.daemon]` and a leftover table is reported as an unknown section, never read.
   A live `pop-queue` tmux window at upgrade time is expected to be orphaned: drains
   are ephemeral, so close it rather than migrating it.

9. **Managed worktrees moved off `queue/worktrees`** — the move runs on the first
   binding touch per machine, but it *refuses* rather than half-completing, so a
   machine can sit un-migrated indefinitely. Run `pop doctor` and confirm the
   `managed worktree root` check is OK. If it is Degraded it names each worktree
   holding the move up: commit or discard the uncommitted changes, or let the live
   drain finish, then run `pop tasks status` and re-check. Verify no
   `<data-dir>/pop/queue/worktrees/` remains and that `pop tasks status` in each
   repo still shows every bound set with its worktree — the recorded paths moved
   with the directories. Removing the fold before this is signed off strands the
   worktrees: the new binary would classify them as adopted checkouts and never
   tear them down.

| Tester | 1 config | 2 [tasks] | 3 storage | 4 re-integrate | 5 scripts | 6 maps | 7 registry | 8 work cut | 9 worktree root | Signed off |
|---|---|---|---|---|---|---|---|---|---|---|
| _(fill in)_ | | | | | | | | | | |

## Questions for beta testers (before cleanup)

- Do you have automation/scripts calling pop outside the installed integrations?
- Which repos have task sets? (Drives checklist item 3.)
- Any config keys you set that aren't in `config.example.toml`?
- Anything you'd want renamed *now* while we're breaking things anyway? (One
  breaking window — batch it.)

## Execution order (for task-set generation)

1. Introduce `[tasks]` config section (alias `[workload]` still read) + README update
   — **ships before sign-off**, testers need it for checklist item 2.
2. Collect sign-offs (checklist above).
3. Remove CLI aliases (inventory A).
4. Remove config read-compat, add hard-error tombstones (B) — including `[workload]`.
5. Remove status aliases; old names exit non-zero (C).
6. Remove migrations + legacy state path (D).
7. Fix cross-references: doctor message, smoke script, CONTEXT.md prune (E).
8. Later pass (separate, no date): delete tombstones — grep `Tombstone:`.

Each step compiles and passes `make test` independently; steps 3–7 can be one
commit series next week.
