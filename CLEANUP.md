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
4. **`[workload]` config section is renamed in this cleanup.** It was never
   deprecated — it is currently the *only* key for task config. The cleanup
   introduces `[tasks]` and hard-errors on `[workload]`, same treatment as the other
   removed keys. One announcement, one migration, no second breaking cycle.
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
| `[workload]` section | `[tasks]` (NEW — introduce first) | `config/config.go:135-137` (`Task *TaskConfig \`toml:"workload"\``) |

Tombstone behavior: presence of any old key → config load fails with
`"<old key> was removed; use <new key>"`. Implementation keeps the old struct
field solely for detection; mark each with a comment `// Tombstone: delete after
<date/condition>` so the second-phase delete is greppable.

Note: `[workload]` needs the *new* key added (`toml:"tasks"`) before the old one can
hard-error. README must document `[tasks.agents.<name>]` from the same commit.

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

### E. Cross-references that go stale (fix in the same change)

| Item | Location | Action |
|---|---|---|
| Doctor "auto-migrated on next tasks command" message | `cmd/doctor.go:391-414` | Message becomes a lie once migration is gone. Repurpose: detect leftover `workloads/` dirs and warn "stranded pre-rename storage; migrate by hand" — or delete the check |
| README documents `[workload.agents.claude]` | `README.md:27` | Rewrite to `[tasks.agents.claude]` |
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

| Tester | 1 config | 2 [tasks] | 3 storage | 4 re-integrate | 5 scripts | Signed off |
|---|---|---|---|---|---|---|
| _(fill in)_ | | | | | | |

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
