# Exploration report — 2026-08-28-work-lift

- Explored: 2026-10-10T02:02:49Z
- Work SHA: a0b86ff539ea
- Explorer: claude --model "opus"

## Where things live

- `work/attribute.go` — `PaneFacts`, `Attribution`, `AttributedContainer`, `AttributeOne`, `Leading`, the three optional seams `PaneAttributor`, `PaneNeighbourhoodAttributor` and `PaneRepositoryAttributor`, and `AttributePane` (the ladder).
- `work/snapshot.go` — `BuildOptions{Ordering, Lift}`, `BuildSnapshotForPane`, `liftAttributed` (the lift; its local is already `lifted`), `kindsInPrecedence` (line 207), `merge`.
- `work/kind.go:155-159` — `Container.Lifted`, which the builder stamps on lifted rows.
- `config/work_view_presets.go` — `presetEntryKeys` allowlist (line 41, contains `"lift"` and its alias `"pin"`), `WorkViewPreset.Lift` with its `desc` tag (line 83), shipped `active` preset with `Lift: true` (line 128), `UnmarshalTOML` decode arm for `lift` and its silent alias `pin` (lines 386-399), `workViewPresetFindings` (line 529).
- `tasks/drain/queue.go:154` — `Deps.WorkBuildOptions()` maps `EffectiveViewPreset().Lift` to `BuildOptions.Lift`.
- `dashboard/dashboard.go` — `Lifted:` list option (line 1068), `reload` (≈2766), `unfilteredDeps` with `preset.Lift = active.Lift` (line 2803), `BuildPageSnapshot` (line 1037). Line 4434 builds with `work.PaneFacts{}`.
- `ui/list.go` — `Opts.Lifted` (line 42), `RowState.Lifted` (line 22), `renderPrefix(selected, lifted, …)` (line 503).
- `dashboard/panefacts.go` — `LaunchPaneFacts(tmuxmod.Tmux, *tasks.Deps)`: one `CurrentPaneFacts` round-trip, mapped to `work.PaneFacts`, plus one `rev-parse --git-common-dir` for `RepoCommonDir`.
- `dashboardshell/shell.go:105,123` — `launchPaneFacts(d *drain.Deps)`, the only caller. It keeps the result on `Shell.pane`.
- `tasks/setkind/attribute.go` — `paneIndex`, `boundSet`, `recordPanes`, `AttributePane`, `AttributePaneNeighbourhood`, `attributeLiveDrainAt`, `attributeBoundCheckoutAt`, `rankBoundCandidates`, `claimHolder`, `drainStarts`, `canonPath`, `pathUnder`, `setCursorKey`. The state fields `panes`, `bound` and `live` are in `tasks/setkind/setkind.go:93-102`. They are reset and filled at `setkind.go:168-170`.
- `wayfinder/attribute.go` — `mapPaneIndex{byMap, byTicket, byRepo}`, `recordPanes(g, m)`, `MapKind.AttributePane` (ticket → assist → stamp → session name), `MapKind.AttributePaneRepository` (live Maps keyed by `g.RepoCommonDir`), `mapCursorKey`. `wayfinder/workkind.go:173-179` resets and fills `k.panes` in `Load`. Groups come from `MapKind.groups()` (line 634).
- `wayfinder/session.go` / `wayfinder/deps.go:75` — `Deps.trunk()` and `ErrNoTrunk`. `MapKind.sessionDeps` (`workkind.go:616`) resolves the Trunk through `binding.ResolveTrunkPathWith` and falls back to the container checkout.
- `routine/attribute.go` — `recordPanes` and the tag-only `AttributePane`. The comment there already explains why the Routine kind has no neighbourhood half.
- `repogroup/repogroup.go:51,185` — `Group.RepoCommonDir = id.CommonDir`.
- `tasks/storage.go:31,60` — `RepositoryIdentity.CommonDir` ("canonical git common directory") and `ResolveRepositoryIdentity(d, cwd)`, which forks `git rev-parse --git-common-dir`.
- `internal/deps/git.go` — the `Git` seam (`Command`, `CommandInDir`). `tasks.Deps.Git` is in `tasks/deps.go`. `internal/deps/memogit.go` is `MemoGit`.
- `dashboard/status_render.go:68-78` — `BuildStatusTables` builds with `work.PaneFacts{}` and `BuildOptions{Ordering}` only, so `Lift` is never set.

## Which seam owns what

- **Ladder order and first-hit rule:** `work.AttributePane`. Pass 1 runs `PaneAttributor` over `kindsInPrecedence(kinds)` and returns the first non-empty answer. Pass 2 does the same with `PaneNeighbourhoodAttributor`. Pass 3 (`attributePaneRepository`) merges every `PaneRepositoryAttributor` answer. The function returns `nil` when `facts.Empty()` is true.
- **Whether rows lift:** `BuildOptions.Lift`, which `drain.Deps.WorkBuildOptions` sets from the preset. Attribution is computed on every build, whatever the grant. Only `liftAttributed` reads the grant.
- **Where lifted rows sit:** `liftAttributed`. It runs on the merged list, matches rows by `CursorKey`, moves each row once, sets `Lifted=true`, and puts the rows first in attribution order.
- **What each kind answers:** the kind adapters own their answers. Task sets: tag pass (`Set`/`Verify`/`Fold`/`Assist`), then neighbourhood (live drain at the directory, then the deepest bound checkout and every set bound there, ranked by `rankBoundCandidates`). Map: tag/stamp pass, then the repository pass (every live Map of the repository). Routine: tag pass only.
- **Pane facts:** `internal/tmux.PaneFacts` holds tmux data only. `dashboard.LaunchPaneFacts` turns it into `work.PaneFacts`. Downstream code does not read tmux again.
- **Repository identity:** `RepositoryIdentity.CommonDir`, carried fork-free on `repogroup.Group.RepoCommonDir`. `boundSet.repoCommonDir` already copies it per bound set.

## Invariants that hold

- `PaneFacts` is comparable, and `Empty()` is `f == PaneFacts{}` (`work/attribute.go:37`).
- Attribution indexes are filled from the refresh before the preset filter (`setkind.recordPanes`, `wayfinder` `Load`). `liftAttributed` skips containers that the build does not hold, so the preset is not widened.
- `recordPanes` adds a set to `k.bound` only when a binding with a non-blank `RuntimePath` exists. The first group wins for a duplicate set id (`setkind/attribute.go:57-71`). Every set, bound or not, also goes into `k.inRepo`, keyed by `g.RepoCommonDir`.
- A binding path is canonicalised only when attribution asks (`boundSet` comment; `canonPath` uses `Tasks.FS.EvalSymlinks`).
- Pane facts are read once per shell (`dashboardshell/shell.go:114-124`). `reload` reuses `m.pane` with no tmux round-trip.
- `unfilteredDeps` copies `Sort` and `Lift` from the active preset, so the widened build lifts exactly when the active preset does.
- The `work` package does not import any kind or config (`BuildOptions` comment).
- Preset decoding is tolerant: unknown keys and wrong types go into `p.problems` → `workViewPresetFindings`, and the load still succeeds.
- Unrelated "pin" words: `store/drains.go` `ExhaustedPinned`/`pinned`, `dashboard/mute_menu.go`, `tasks/agent_*.go`, `integrate/grill_overlay_drift_test.go`, `ui/dashboard.go` `pinProtectedPane`/📌, `ui/list.go` `AnchorTop` comment, `supervisor/supervisor.go:121`, `work/kind.go:152` (CursorKey comment), `work/snapshot.go:38,60`, `wayfinder/invocation.go:13`, `wayfinder/parse.go:196`, `wayfinder/workkind.go:217`, `ui/appearance.go:18`, `ui/util.go`, `ui/program.go:113`, `cmd/tasks.go:1237`.

## The test shape

- **Config:** `config/work_view_presets_test.go:325` `TestOnlyTheShippedActivePresetDeclaresLift` and `:333` `TestPresetLiftDecodesUnderEitherSpellingAndDefaultsToFalse`.
- **Cross-kind:** `work/conformance_test.go` has `conformanceCases()` (line 62), `TestKindConformance` (180), `twoKindPage` (401), `TestAnAttributedMapRowLiftsAboveTheWholeTaskSetBlock` (428), and the `fixture.groups()` seam (493).
- **Kind level:** `wayfinder/attribute_test.go` and `routine/attribute_test.go`. There is no `tasks/setkind/attribute_test.go` and no `work/attribute_test.go`. Task-set attribution is driven through `dashboard/pane_lift_test.go` and `dashboard/pane_bound_checkout_test.go`.
- **Rebuilds:** `dashboard/pane_lift_rebuild_test.go` covers the rebuild cases. `TestRebuildsReadThePaneNoSecondTime` (106) counts `tmuxtest.Fake.CurrentPaneFactsCalls` across polls.
- **Shell entry:** `dashboardshell/pane_lift_test.go` (64, 114, 170, 211) covers entry on either page.
- **Status:** `cmd/work_status_lift_test.go:38` `TestWorkStatusNeverLiftsUnderAnyPreset`.
- **Shared fakes:** `internal/tmux/tmuxtest`, `internal/queuetest` (incl. `RepoCommonDir(t, td, path)` at `deps.go:59`), and the `tasks` fake runner and git-repo template. `tasks/drain/kinds_git_memo_test.go` already counts git calls.

## Read these in ranges

- `dashboard/dashboard.go` (4447 lines): 880-890 (model fields), 1015-1090 (`OpenPage`, `BuildPageSnapshot`, list options), 2750-2810 (`reload`, `unfilteredDeps`), 4430-4440.
- `wayfinder/workkind.go` (828): 90-195 (deps, `Load`, `recordPanes` call), 589-640 (`resolveMapForSpawn`, `sessionDeps`, `groups`).
- `config/work_view_presets.go` (587): 30-135, 355-400, 525-587.
- `tasks/setkind/setkind.go`: 90-175.
- `work/conformance_test.go` (596): 62-180, 400-460.
