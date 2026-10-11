# Exploration report — 2026-08-28-work-lift

- Explored: 2026-10-10T01:25:09Z
- Work SHA: a0b86ff539ea
- Explorer: claude --model "opus"

## Where things live

- `work/attribute.go`: `PaneFacts` (now with `RepoCommonDir`), `Attribution`, `AttributedContainer`, `AttributeOne`, `Leading`, the `PaneAttributor`, `PaneNeighbourhoodAttributor` and `PaneRepositoryAttributor` seams, and `AttributePane`, which is the ladder.
- `work/snapshot.go`: `BuildOptions.Lift`, `BuildSnapshotForPane`, `liftAttributed` (the lift) and `kindsInPrecedence`.
- `work/kind.go:155-159`: `Container.Lifted`, the mark that the builder stamps.
- `tasks/drain/queue.go:148-156`: `Deps.WorkBuildOptions` maps `EffectiveViewPreset().Lift` to `Lift`.
- `tasks/drain/kinds.go:67-95`: `WithGitMemo` wraps `tasks.Deps.Git` in a `deps.MemoGit` for one load (`internal/deps/memogit.go`).
- `config/work_view_presets.go`:
  - The `presetEntryKeys` allowlist (`"lift"`, and `"pin"` as its silent alias, line 45).
  - The `WorkViewPreset.Lift` field with its `toml:"lift"` and `desc` tags (lines 78-83).
  - The shipped `active` preset, which sets `Lift: true` (lines 124-128).
  - The decode arm over `pin` then `lift` (lines 386-399; `lift` wins), which appends `"<key> must be a bool"` to `p.problems`.
- `dashboard/panefacts.go`: `LaunchPaneFacts(t tmuxmod.Tmux, td *tasks.Deps)` does one `CurrentPaneFacts()` call and maps it to `work.PaneFacts`, and fills `RepoCommonDir` with one `tasks.ResolveRepositoryIdentity` of the pane's directory. On a nil tmux or an error, it returns zero facts.
- `dashboardshell/shell.go:104-125`: `launchPaneFacts(d *drain.Deps)`, the only caller. It stores the result on `Shell.pane` and gives it to `BuildPageSnapshot`.
- `dashboard/dashboard.go`:
  - The `Lifted:` option of `ui.NewList` (around line 1068).
  - `reload` (lines 2755-2776), which rebuilds with the held `m.pane` on each poll.
  - `unfilteredDeps`, which copies `preset.Lift = active.Lift` (line 2803).
  - An empty-facts build at line 4434.
- `dashboard/status_render.go:65-78`: `BuildStatusTables` builds both pages with `work.PaneFacts{}` and a `BuildOptions` that has no `Lift`.
- `ui/list.go`: `RowState.Lifted` (line 22), `Opts.Lifted` (lines 38-42), and `renderPrefix(selected, lifted, …)` (line 503), which draws the `▸` cell.
- `tasks/setkind/attribute.go`: the Task-set kind's `paneIndex`, `recordedSet`, `boundSet`, `recordPanes`, `AttributePane`, `AttributePaneNeighbourhood`, `AttributePaneRepository`, `attributeLiveDrainAt`, `attributeBoundCheckoutAt`, `rankBoundCandidates`, `claimHolder`, `drainStarts`, `canonPath`, `pathUnder` and `setCursorKey`.
- `wayfinder/attribute.go`: the Map kind's `mapPaneIndex` (`byMap`, `byTicket` and `byRepo`, keyed by `g.RepoCommonDir`), `MapKind.AttributePane` and `MapKind.AttributePaneRepository`. The Map kind does not implement `AttributePaneNeighbourhood`. `wayfinder/workkind.go:173-179` resets `k.panes` and calls `recordPanes(g, m)` before the `visible` preset filter. `containerFor` (around line 668) already sets `RepoCommonDir: g.RepoCommonDir` on each Map container.
- `routine/attribute.go`: the Routine kind's `recordPanes` and its tag-only `AttributePane`. Its comment (lines 9-22) already explains why there is no neighbourhood rung.
- `repogroup/repogroup.go:46`: `Group.RepoCommonDir` and `Group.RepoKey`.
- Git common-directory lookups that exist now:
  - `tasks/resolve.go:86` uses one `rev-parse --show-toplevel --git-common-dir`.
  - `tasks/storage.go:69` reads the common directory of a cwd.
  - `internal/queuetest/deps.go:59` has the `RepoCommonDir` test helper.
- Docs:
  - `docs/adr/0241-…` is the parent ADR.
  - `CONTEXT.md` has 44 lines that match "pin".
  - `.grill-context/CONTEXT.0037.EE1D9DA1.md` is the glossary fragment from commit `9a24df8d`.

## Which seam owns what

- **Rung order across kinds:** `work.AttributePane` owns it. Pass 1 is `PaneAttributor`, pass 2 is `PaneNeighbourhoodAttributor`, pass 3 is `PaneRepositoryAttributor`. All passes go over `kindsInPrecedence(kinds)` and return `nil` when `facts.Empty()`. Passes 1 and 2 return the first hit that has `len(Containers) > 0`; pass 3 concatenates every kind's answer.
- **Rungs inside a kind:** each kind owns its own rungs.
  - Task set, in `setkind`: pass 1 is the tag (`Set`/`Verify`/`Fold`/`Assist`). Pass 2 is a live drain at the directory, then the bound checkout that contains it (the deepest one wins), ranked by claim holder, then the most recent drain, then `tasks.WorkRowLess`.
  - Map, in `wayfinder`: pass 1 in this order: ticket, assist, `WorkKind`/`WorkID` stamp, session name; then pass 3, every Map of the repository in `MapKind.Less` order.
  - Routine: pass 1 only, on the `Routine` tag.
- **Whether attributed rows lift:** the preset grants it, through `WorkBuildOptions` and `BuildOptions.Lift`. `liftAttributed` does the lift. `BuildSnapshotForPane` computes `snap.Attribution` with or without the grant.
- **Reading the launch facts:** `dashboard.LaunchPaneFacts` is the only place. Tmux keys stay in `internal/tmux`, which has its own `PaneFacts` (`internal/tmux/panefacts.go`).

## Invariants that hold

- `PaneFacts` contains only `string` fields, and `Empty()` is `f == PaneFacts{}` (`work/attribute.go:37`).
- Each kind records its pane index from the rows it read before the preset filter:
  - `setkind.recordPanes` reads `refresh.Rows`.
  - `wayfinder/workkind.go:179` records before `visible`.
  - `routine.recordPanes` records at `Load`.
- `liftAttributed` lifts nothing for a container that the build does not hold. Thus a hidden row is not lifted (`work/snapshot.go:169-172`).
- `liftAttributed` keeps the attribution's ranked order, puts each row in the lifted block only one time (`lifted[i]`), and keeps the order of all other rows.
- In `setkind`, two groups can hold a set with the same id. The first group wins in `k.panes`.
- `setkind.recordPanes` appends every set to `k.sets` (for the repository pass), and to `k.bound` only when `snap.bindingFor` finds a binding that has a non-blank `RuntimePath`.
- `canonPath` uses `Tasks.FS.EvalSymlinks` and does not fork git. `boundSet` paths are canonicalised only when attribution asks for them.
- `status_render.go` and `dashboard.go:4434` pass `work.PaneFacts{}`, so attribution stays silent there.
- The words that are not this concept:
  - `store/drains.go` `ExhaustedPinned`
  - `dashboard/mute_menu.go` "pinned first"
  - `tasks/agent_*.go` "pinned model"
  - `integrate/grill_overlay_drift_test.go`
  - `ui/list.go` `AnchorTop`/`AnchorBottom` "pins rows" (lines 13-15)
  - `work/snapshot.go:38-39` model-skip "pins"

## The test shape

- **Ladder and lift:**
  - `work/conformance_test.go` is the per-kind conformance table. `TestAnAttributedMapRowLiftsAboveTheWholeTaskSetBlock` (lines 424-455) builds with `Lift: true` and checks `c.Lifted`.
  - `wayfinder/attribute_test.go:23` and `routine/attribute_test.go:43` call `BuildSnapshotForPane` with `Lift: true`.
- **Dashboard:** `dashboard/pane_lift_test.go`, `dashboard/pane_lift_rebuild_test.go` (rebuilds) and `dashboard/pane_bound_checkout_test.go`.
- **Shell:** `dashboardshell/pane_lift_test.go`.
- **Read surface:** `cmd/work_status_lift_test.go` and `dashboard/status_render_test.go`.
- **Config:** `config/work_view_presets_test.go`, which has the `…DeclaresLift`, `…PresetLiftDecodes…` and `…PresetPinIsASilentAliasForLift` tests.
- **Shared doubles:**
  - `internal/queuetest`: the recording tmux, the spawn-repo fixtures and `RepoCommonDir`.
  - `internal/tmux/tmuxtest`.
  - The fake `CommandRunner` and git-repo template in `tasks/`.
- **Gate:** `go build ./... && go vet ./<pkg>/... && go test ./<pkg>/...`, then `make test`.

## Read these in ranges

- `dashboard/dashboard.go` (4447 lines): 1035-1090 and 2750-2810.
- `work/snapshot.go`: 85-205.
- `config/work_view_presets.go`: 35-50, 75-130 and 370-400.
- `work/conformance_test.go`: 420-455.
- `wayfinder/workkind.go`: 100-185 and 605-675.
- `dashboardshell/shell.go`: 40-50 and 100-125.
