# Exploration report — 2026-08-28-work-lift

- Explored: 2026-10-10T11:13:25Z
- Work SHA: a0b86ff539ea
- Explorer: claude --model "opus"

## Where things live

- `work/attribute.go`: `PaneFacts`, `Attribution`, `AttributedContainer`, `AttributeOne`, the `PaneAttributor`, `PaneNeighbourhoodAttributor` and `PaneRepositoryAttributor` seams, and `AttributePane`. `AttributePane` makes two first-hit passes over `kindsInPrecedence(kinds)`, then a third repository pass that merges every kind's answer.
- `work/snapshot.go`: `BuildOptions{Ordering, Lift}` (the task's `SnapshotOptions` does not exist), `BuildSnapshotForPane` and `liftAttributed` (its local variables are `lifted` and `moved`). `work/kind.go:155-159`: `Container.Lifted`.
- `tasks/setkind/attribute.go`: the Task-set answer. It holds `paneIndex`, `recordedSet`, `boundSet` (embeds `recordedSet`), `recordPanes`, `AttributePane` (tags), `AttributePaneNeighbourhood` (live drain, then bound checkout), `AttributePaneRepository` (every set whose `repoCommonDir` matches), `rankBoundCandidates`, `claimHolder`, `drainStarts`, `(*Deps).canonPath` and `pathUnder`. The state is on `Kind` in `tasks/setkind/setkind.go:91-102` (`panes`, `sets`, `bound`, `live`), and `Load` resets it at `setkind.go:168`.
- `wayfinder/attribute.go`: the Map answer. It holds `mapPaneIndex{byMap, byTicket, byRepo}`, `recordPanes(g, m)`, `MapKind.AttributePane` (ticket, assist, `WorkKind`/`WorkID` stamp, `MapIDFromSession`) and `MapKind.AttributePaneRepository` (every Map whose group `RepoCommonDir` matches, in `MapKind.Less` order). The Map kind has no neighbourhood method. `wayfinder/workkind.go:172-180` calls `recordPanes` for every Map before `visible(...)` filters it, and `g` (with `g.RepoCommonDir`) is in hand there. `wayfinder/session.go` holds `EnsureMapSession` and `ErrNoTrunk`. `MapKind.sessionDeps` (`workkind.go:616`) resolves the Trunk through `binding.ResolveTrunkPathWith`.
- `routine/attribute.go`: tag pass only. Its file comment explains why there is no neighbourhood half and no repository half.
- `dashboard/panefacts.go`: `LaunchPaneFacts(t tmuxmod.Tmux, td *tasks.Deps) work.PaneFacts`. It makes one `CurrentPaneFacts()` call and gives zero facts on nil or on an error; then one `tasks.ResolveRepositoryIdentity` on the pane directory fills `RepoCommonDir`.
- `dashboardshell/shell.go:104,123-125`: `launchPaneFacts(d *drain.Deps)`, the only caller. It passes `d.Tmux` and `d.Tasks`, and the shell keeps `pane` for both pages.
- `dashboard/dashboard.go`: the `pane` field (~885), the `ui.Opts.Lifted` option (~1065-1068), `reload`/`unfilteredRows`/`unfilteredDeps` (~2755-2808, `preset.Lift = active.Lift` at 2803).
- `ui/list.go`: `RowState.Lifted` (22), `Opts.Lifted` (38-42), and `renderPrefix(selected, lifted, …)` (~470-510). The `AnchorTop`/`AnchorBottom` comments on lines 13-15 use "pins" in a layout sense.
- `config/work_view_presets.go`: `presetEntryKeys` includes `"lift"` and its silent alias `"pin"` (~45), `WorkViewPreset.Lift` with its `desc` tag (78-86), the shipped `active` preset `Lift: true` (~129-131), and the decode arm (~389-401) that reads `pin` then `lift` (so `lift` wins) and appends `"<key> must be a bool, got %T"` to `p.problems`.
- `tasks/drain/queue.go:154-155`: `WorkBuildOptions()` is the one place that sets `Lift: d.EffectiveViewPreset().Lift`. `tasks/drain/kinds.go`: `WorkKinds`/`RoutinePageKinds` wrap the deps in `WithGitMemo()`, and `kinds()` wires `[TaskSetKind, MapKind]` for page A.
- `dashboard/status_render.go:70-78`: `BuildStatusTables` builds both pages with `work.PaneFacts{}`.
- `repogroup/repogroup.go:46-62,185`: `Group.RepoCommonDir = id.CommonDir`. `Group.Storage()` is at line 76.
- `tasks/storage.go:60-72`: `ResolveRepositoryIdentity(d, cwd)` runs `rev-parse --git-common-dir`, then `identityFromCommonDir` makes the path absolute and canonical (`canonicalAbsPath`). `internal/deps/git.go`: the `Git` interface (`Command`, `CommandInDir`). `tasks/deps.go:35-37`: `tasks.Deps.Git`.

## Which seam owns what

- **The pass order and first-hit rule:** `work.AttributePane`. **Kind order:** `kindsInPrecedence`. A kind takes part in a pass only by type assertion on a seam interface.
- **What each kind answers:** the kind's own `AttributePane*` methods. They read only the indexes that their `Load` filled.
- **Whether attributed rows are lifted:** `BuildOptions.Lift`, set only by `drain.Deps.WorkBuildOptions` from the preset. **How rows are lifted:** `liftAttributed`, which moves rows by `CursorKey`.
- **The ▸ cell:** `Container.Lifted` → `dashboard.go` `Opts.Lifted` → `ui.List.renderPrefix`.
- **Reading pane facts:** `internal/tmux` reads tmux. `dashboard.LaunchPaneFacts` maps the result into `work.PaneFacts`. ADR-0241 decisions 5-6 say the new git fact also goes in `LaunchPaneFacts`, on `work.PaneFacts`.
- **Repository identity:** `tasks.ResolveRepositoryIdentity` (the one with a git fork) and `repogroup.Group.RepoCommonDir` (the one with no fork).

## Invariants that hold

- `PaneFacts.Empty()` is `f == PaneFacts{}` (`work/attribute.go:37`). All fields are strings, so the struct is comparable. With empty facts, `AttributePane` gives nil.
- The attribution indexes are filled before the preset filter: `setkind.recordPanes` reads `refresh.Rows`, and `wayfinder` calls `recordPanes` before `visible`. `liftAttributed` silently skips a `CursorKey` that the snapshot does not have, so the preset is never widened.
- `setkind.recordPanes`: the first group wins when two groups have the same set id. A `boundSet` is recorded only when `bnd.RuntimePath` is not empty. Paths are canonicalised only when the question is asked (comment on `boundSet`).
- Every `BuildSnapshotForPane` computes attribution again. `reload` uses the stored `m.pane` and makes no tmux call (`dashboard.go:2755-2770`).
- `unfilteredDeps` copies the active preset's `Sort` and `Lift` into the `all` preset.
- `MemoGit` is created again for each load by `WithGitMemo` (`tasks/drain/kinds.go:77`).
- `config/work_view_presets.go`: a wrong type gives a problem and does not fail the load (`p.problems`, ADR-0054).
- `CONTEXT.md` has **Pane work attribution** (1547) but no **Work lift** entry. **Following** (394) lists "Pin" under _Avoid_.

## The test shape

- `work/conformance_test.go`: `conformanceCase` (30-60) has no attribution column. `TestAnAttributedMapRowLiftsAboveTheWholeTaskSetBlock` (428) uses `twoKindPage(f)` with `BuildSnapshotForPane` and checks the "▸kind:id" order.
- `dashboard/pane_lift_test.go`, `pane_lift_rebuild_test.go` (`TestRebuildsReadThePaneNoSecondTime`, 106) and `pane_bound_checkout_test.go`. The last one sets up real git worktrees with `boundCheckoutFixture`/`bindStemsToOneCheckout` and `runGit`.
- `dashboardshell/pane_lift_test.go`: `tmuxtest.Fake` (`CurrentPaneFactsCalls` counts the reads), a `d.Kinds` override and `attributingKind`.
- `cmd/work_status_lift_test.go`: `TestWorkStatusNeverLiftsUnderAnyPreset` with `attributingStubKind`.
- `config/work_view_presets_test.go:325-415`: `TestOnlyTheShippedActivePresetDeclaresLift`, `TestPresetLiftDecodesAndDefaultsToFalse` and `TestPresetPinIsASilentAliasOfLift`.
- `wayfinder/attribute_test.go` and `routine/attribute_test.go`.
- `tasks/drain/kinds_git_memo_test.go` and `internal/queuetest` (`RepoCommonDir` helper at `deps.go:59`) are the existing memo and git-identity fixtures.

## Read these in ranges

- `dashboard/dashboard.go` (4447 lines): 875-895, 1055-1095, 2745-2815.
- `config/work_view_presets.go` (587 lines): 35-135, 370-400.
- `wayfinder/workkind.go` (828 lines): 85-190, 605-630.
- `work/snapshot.go` (331 lines): 80-200.
- `work/conformance_test.go` (596 lines): 30-180, 420-460.
- `docs/adr/0241-…md`: 42-110 (Decision).
