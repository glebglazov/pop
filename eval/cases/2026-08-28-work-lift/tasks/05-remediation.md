## What to build

Resolve the verification findings below. An independent Verifier judged this task set's completed work at 73fb8605be32 and returned FIXABLE: the acceptance criteria are not yet met, but the gaps are ones an agent can close. Make the changes needed to satisfy the set's acceptance criteria. Do not edit the other task specs — they are stable, task-scoped intent; fix the code and artifacts they describe.

This is remediation cycle 1.

## Findings

**Gates (all green, run in this checkout at 73fb860, clean tree)**
- `go build ./...` → exit 0
- `go vet ./...` → exit 0
- `go test ./...` (the whole-tree gate `make test` runs) → exit 0
- Fresh scoped re-run, no cache: `go test -count=1 ./work/... ./dashboard/... ./dashboardshell/... ./tasks/setkind/... ./wayfinder/... ./routine/... ./config/... ./ui/... ./cmd/...` → exit 0

**01-lift-is-not-a-pin — one criterion fails**

Criterion "No identifier, filename or comment in the tree calls the derived behaviour a pin" is unmet. Two comments about the lift survived the rename, both in files the diff never touched (so neither is caught by reading the diff alone):

- `dashboardshell/shell.go:45` — "…kept for the page the toggle builds later — **the pin** is per page, the tmux read is not." This is the same sentence, about the same fact, as the `launchPaneFacts` doc comment eleven lines of diff away that *was* corrected to "the lift is simply computed per page".
- `dashboard/status_render.go:68` — "Empty pane facts, so nothing is attributed and **nothing pins** whatever the preset declares…" — the grant the preset declares is now `lift`, and `BuildStatusTables` is exactly the surface task 02 tests as lifting nothing.

The four unrelated senses are correctly untouched: `store/drains.go`'s `ExhaustedPinned`, `dashboard/mute_menu.go`'s "pinned first", the "pinned model" family in `tasks/`, and `integrate/grill_overlay_drift_test.go`'s drift pin. The monitor dashboard's Following 📌 (`ui/dashboard.go:1663,1683`) and the Pinned action menu references (`dashboard/dashboard.go:1660`) are the two allowed manual senses. All test files and identifiers renamed (`pane_lift_test.go`, `pane_lift_rebuild_test.go`, `dashboardshell/pane_lift_test.go`, `cmd/work_status_lift_test.go`, `Container.Lifted`, `Opts.Lifted`, `BuildOptions.LiftPane`, `liftAttributed`).

The other three criteria of task 01 hold: `lift = true` grants it and `Lift: true` appears on exactly one shipped preset (`active`, `config/work_view_presets.go:133`), pinned by `TestOnlyTheShippedActivePresetDeclaresLift`; `pin` stays in the key allowlist and decodes into `Lift` before `lift` is read, so it grants identically, `lift` wins when both appear, and no finding is emitted for a well-formed `pin` entry (`TestPinIsASilentAliasForLift`, `TestLiftWinsWhenAPresetDeclaresBothSpellings`); a non-bool for either spelling appends to `p.problems` and does not fail the load.

**02-a-repository-pass-lifts-its-task-sets — all criteria met**

`work.PaneRepositoryAttributor` is a third seam rather than a rung inside `AttributePaneNeighbourhood`, so any checkout answer outranks any repository answer; `attributeRepository` merges across kinds and is commented as the one non-first-hit pass. `recordPanes` now records every row into `k.inRepo` and only bound rows into `k.bound`, both sharing one `sortRow`. The repository is resolved in `LaunchPaneFacts` via `tasks.ResolveRepositoryIdentity` and carried on `work.PaneFacts.RepoCommonDir` (not on `internal/tmux`'s facts); `PaneFacts` stays comparable and `Empty()` still short-circuits `AttributePane`. `dashboardshell/shell.go:125` is the only production caller.

Every criterion is asserted end-to-end in `dashboard/pane_repository_test.go` against real worktrees: sibling-worktree lift, bound-checkout answered by the checkout rung alone with the third set left where the sort had it, tagged pane unchanged from either directory, bound-and-unbound sets alike, silence outside a repository and in an unknown one, `TestThePanesRepositoryIsResolvedOnceAtLaunch` (counting git seam over both `Tasks.Git` and `Project.Git`, one `--git-common-dir` fork at launch and none across ten rebuilds, with the lift re-asserted so the ceiling is not cheap for the wrong reason), `TestWorkStatusLiftsNothingAndForksNoGitForAttribution`, and `TestARepositoryLiftDoesNotWidenThePreset`. The two-answering-kind arity is pinned kind-independently in `work/attribute_repository_test.go:71`.

**03-maps-lift-with-their-repository — all criteria met**

`MapKind.AttributePaneRepository` answers from `panes.inRepo`, filed under the group's `RepoCommonDir` (the Trunk's repository, not a re-derived trunk), gated by `liveForLocality` (BROKEN and abandoned out, archived still a candidate since row visibility is the preset's business). `ref.Kinds()` orders task set before map, so the merge puts set rows first — asserted by `TestARepositoryLiftsItsTaskSetsAndItsMapsTogether`, with a Map-only case beside it. `TestMapSessionBeatsTheRepositoryPass` pins the stamp answer surviving. `routine/attribute.go` gains the comment explaining the asymmetry and ending "Do not wire `AttributePaneRepository` here", and `work/conformance_test.go` now pins the per-kind pass table (`tags` for Routine, `tags`+`repository` for Map, all three for the Task set).

## Acceptance criteria

- [x] Every finding above is resolved and the task set's acceptance criteria are met
