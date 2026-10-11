# Exploration report — 2026-08-28-work-lift

- Explored: 2026-10-10T12:02:11Z
- Work SHA: a0b86ff539ea
- Explorer: claude --model "opus"

## Report: work lift area at a0b86ff5

### Where things live
- `config/work_view_presets.go`: Work view presets. `presetEntryKeys` (line ~41) is the allowlist of known keys and holds `"lift"` and its silent alias `"pin"`. The `WorkViewPreset.Lift` field (line 84) has `toml:"lift"` and a `desc` tag. The shipped `active` preset sets `Lift: true` (line ~129). `UnmarshalTOML` (line 359) has one arm over `"pin"` then `"lift"` (line ~390, so `lift` wins) that turns a non-bool into a `p.problems` entry. `workViewPresetFindings` (line 537) turns problems into `Finding`s, and the load does not fail.
- `tasks/drain/queue.go:154`: `WorkBuildOptions` maps the preset to the builder: `Lift: d.EffectiveViewPreset().Lift`.
- `work/attribute.go`: `PaneFacts`, `Attribution`, `AttributeOne`, the `PaneAttributor` and `PaneNeighbourhoodAttributor` seams, the `PaneRepositoryAttributor` seam, and `AttributePane` (two first-hit passes over `kindsInPrecedence`, then `attributeRepository`, a third pass that merges every kind's answer when `PaneFacts.RepoCommonDir` is set).
- `work/snapshot.go`: `BuildOptions.Lift` (line 97). `BuildSnapshotForPane` loads every kind, merges the rows, then calls `AttributePane` on every build. `liftAttributed` (line 172) runs only when `Lift` is true. `kindsInPrecedence` is at line 205.
- `work/kind.go:159`: `Container.Lifted`.
- `ui/list.go`: `Opts.Lifted` (line 42), `RowState.Lifted` (line 22), and `renderPrefix` (line 503), which draws `▸`.
- `dashboard/dashboard.go`: `BuildPageSnapshot` (line 1037), the `Lifted:` list option (line 1068), `unfilteredDeps` with `preset.Lift = active.Lift` (line 2803), and `RunDashboard` (line 4434, empty `PaneFacts`).
- `dashboard/panefacts.go`: `LaunchPaneFacts(tmux.Tmux, *tasks.Deps)` makes one `CurrentPaneFacts` call and maps the result into `work.PaneFacts`, then one `ResolveRepositoryIdentity` on the pane's directory for `RepoCommonDir`.
- `dashboardshell/shell.go:~123`: `launchPaneFacts(d *drain.Deps)` is the only caller. It keeps `pane` on `Shell` and gives it to both pages.
- `dashboard/status_render.go:68`: `BuildStatusTables` uses `work.PaneFacts{}` and a `BuildOptions` with no `Lift`.
- `tasks/setkind/attribute.go` and `setkind.go`: the Task-set kind's attribution.
- `wayfinder/attribute.go`, `wayfinder/workkind.go`, `wayfinder/deps.go`: the Map kind's attribution, `MapKind.Load`, and `Deps.trunk()`.
- `routine/attribute.go`: answers the tag pass only. Its header comment already says why it has no neighbourhood pass.
- `internal/deps/git.go` has the `Git` seam (`Command`, `CommandInDir`). `internal/deps/memogit.go` has `MemoGit`, which lives for one load. `tasks/deps.go` has the `Deps.Git` field.
- `tasks/storage.go:60`: `ResolveRepositoryIdentity(d, cwd)` runs one `rev-parse --git-common-dir`, then `identityFromCommonDir`, which makes the path canonical. `repogroup/repogroup.go:185` fills `Group.RepoCommonDir` from the same `id.CommonDir`.

### Which seam owns what
- **Grant of the lift:** `WorkViewPreset.Lift` → `drain.Deps.WorkBuildOptions` → `work.BuildOptions.Lift` → `liftAttributed`. `work` does not import `config` (ADR-0210).
- **Attribution order:** `work.AttributePane` owns the order of the passes and the order of the kinds. Each kind owns its own answer.
- **Task-set answers:** `setkind.Kind` keeps `panes paneIndex`, `sets []recordedSet` and `live []tasks.RunningDrain`. `Load` resets all three (`setkind.go:168`), then calls `recordPanes` for each group.
  - `recordPanes` adds every row to `panes` and to `sets`, including rows the preset hides. A `recordedSet` has an empty `runtimePath` when `snap.bindingFor` gives no binding path.
  - `AttributePane` answers the Set, Verify, Fold and Assist tags.
  - `AttributePaneNeighbourhood` uses `canonPath(Directory)`, then `attributeLiveDrainAt`, then `attributeBoundCheckoutAt` (the deepest containing checkout, using `pathUnder`). `rankBoundCandidates` ranks the candidates by: claim holder, then drain recency (`tasks.LatestDrainStarts`), then `tasks.WorkRowLess`.
- **Map answers:** `MapKind.panes mapPaneIndex` holds `byMap`, `byTicket` and `inRepository` (each Map beside its group's `RepoCommonDir`, read by `MapKind.AttributePaneRepository`). `Load` refills it for each scanned group (`workkind.go:173–179`). `MapKind.AttributePane` checks, in this order: ticket, assist, Work stamp, then `MapIDFromSession`. The Map kind does not implement `PaneNeighbourhoodAttributor`. The Map row's cursor key is `mapCursorKey(g, id)`; the Task-set row's key is `setCursorKey(g, id)`.
- **Trunk:** `wayfinder.Deps.Trunk func() (string, error)` is read by `trunk()`. `drain`'s `MapKindDeps` builder (`tasks/drain/kinds.go:142`) does not set `Trunk` on the `wayfinder.Deps` it builds.

### Invariants that hold
- `PaneFacts` is comparable, and `Empty()` is `f == PaneFacts{}`. Empty facts give a nil attribution (`AttributePane`).
- Attribution is computed under every preset. Only the move to the top depends on `Lift` (`BuildSnapshotForPane`).
- `liftAttributed` moves rows and never copies them. It skips keys the build does not hold, so the preset is never widened. It keeps the rank order of the attribution and puts all other rows after it in their old order.
- The Task-set and Map kinds record attribution before the preset filter runs (`recordPanes` comments).
- For the same id in two groups, the first group wins (`setkind.recordPanes`, `mapPaneIndex.recordPanes`).
- The pane is read once at launch, and rebuilds read no tmux (`dashboardshell.launchPaneFacts`; test `TestRebuildsReadThePaneNoSecondTime`).
- `setkind.Kind.Load` forks no git (its doc comment, ADR-0060). `MemoGit` must live for only one load (`memogit.go` doc).
- `internal/tmux` holds all tmux knowledge (ADR-0142).
- Other uses of the word "pin" that are not this behaviour: `store/drains.go` `ExhaustedPinned`, the "pinned first" text in `dashboard/mute_menu.go`, "pinned model" in `tasks/agent_*.go`, `integrate/grill_overlay_drift_test.go`, the comments on `ui/list.go` `AnchorTop` and `AnchorBottom`, and the `work/snapshot.go` comments at lines 38–39 and 60.
- `CONTEXT.md` has no **Pane pin** or **Work lift** entry. Its line 394 `_Avoid_: Pin, watch` is about Following, and its **Pinned action menu** entry is at line 1439.

### The test shape
- `dashboard/work_lift_test.go`, `work_lift_rebuild_test.go` and `pane_bound_checkout_test.go` run the full path with a fake tmux:
  - Helpers: `openFromPane` (calls `LaunchPaneFacts(d.Tmux)` then `BuildPageSnapshot`), `rebuild` (`m.reload()`), `inPane`, `tagPane`, `liftedBlock`, `unliftedOrder`, `wantLiftedFirst`.
  - Fixtures: `boundCheckoutFixture` and `bindStemsToOneCheckout` use a real git worktree, `binding.Put`, and `queuetest.SetupSpawnRepo`. They set `d.ViewPreset.Lift = true` directly.
- `dashboardshell/work_lift_test.go` tests the entry page and page B.
- `cmd/work_status_lift_test.go` has `TestWorkStatusNeverLiftsUnderAnyPreset`.
- `config/work_view_presets_test.go:325,333,390` has `TestOnlyTheShippedActivePresetDeclaresLift`, `TestPresetLiftDecodesAndDefaultsToFalse` and `TestPresetPinIsASilentAliasForLift`.
- `work/conformance_test.go` is an external test package. It runs the real adapters through `conformanceCase` and `newFixture`. `twoKindPage(f)` wires both Map and Task-set kinds, and `TestAnAttributedMapRowLiftsAboveTheWholeTaskSetBlock` (line 428) uses it with `Lift: true`.
- Per-kind attribution tests are in `wayfinder/attribute_test.go` and `routine/attribute_test.go`. `tasks/setkind/` has no attribution test file.
- Test doubles: `internal/queuetest` (recording tmux, spawn repos, `RepoCommonDir`) and `internal/tmux/tmuxtest`. `RecordingTmux.Fake.CurrentPaneFactsCalls` counts tmux reads. No shared counting `deps.Git` fake was found.

### Read these in ranges
- `dashboard/dashboard.go` (4447 lines): 1015–1075 and 2790–2810.
- `wayfinder/workkind.go` (828 lines): 85–200.
- `tasks/setkind/setkind.go`: 85–175.
- `work/conformance_test.go`: 1–60 and 400–455.
- `config/work_view_presets.go`: 40–135, 355–420 and 525–587.
- `work/snapshot.go`: 85–220.
- `docs/adr/0241-*`: decisions 1–10.
