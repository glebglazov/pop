# Exploration report — 2026-09-07-exploration-report

- Explored: 2026-10-04T07:38:19Z
- Work SHA: 647143bb0a85
- Explorer: claude --model "opus"

## Exploration report: 2026-09-07-exploration-report

This describes the tree at work commit 647143bb, before any task in this set was built. Nothing named "explore" exists yet. Every seam below is a refine or verify sibling that a task can copy.

### Where things live
- `tasks/artifacts.go`: the artifact constants (`ArtifactTypeSpec`, `ArtifactTypeProgress`, `ProgressFileName`, `RefineDirName`, `VerifyDirName`). It also holds `Artifacts()`, which matches root files by name in a `switch` (`SpecFileName`, `ProgressFileName`) and then scans `reportArtifactKinds = []passReport{verifyReport, refineReport}`. Tier order is the list `artifactTierOrder` (verify, refine, spec, exploration, progress). A type that is not in that list sorts last.
- `tasks/setkind/artifacts.go`: `Kind.Artifacts` maps `tasks.Artifacts` straight onto `work.Artifact`. `setkind.go` `artifactSections` builds the dashboard detail section from `tasks.LatestArtifactSummary`. **The detail section is not in `dashboard/render.go`.** That file only styles status cells and Verified-at badges.
- `tasks/manifest.go`: `SpecFileName`/`legacySpecFileName` (lines ~21–28). `parseManifestJSON` (~340–416) sends every set key it does not name into `m.Unknown`. `validateNoOrphanMarkdown` (~505–531) exempts `SpecFileName`, `legacySpecFileName`, `ExplorationFileName` and listed files. `optedOut`/`VerifyOptedOut`/`RefineOptedOut` (~634–674) and `AgentDirective`/`VerifierOverride`/`RefinerOverride`/`agentDirective` (~676–721) read from `Unknown`.
- `tasks/authoring_guide.go`: `AuthoringGuide()` is one `fmt.Sprintf` with positional constants. The `verify`/`refine` and `verifier`/`refiner` bullets are at ~205–215.
- `tasks/refine.go` (614 lines): the whole Refine pass. `tasks/pass_report.go`: the report machinery. `tasks/refine_pointer.go`: `RefinePointer = ReportPointer` plus `refineBlock`. `tasks/refine_mark.go`: the mark. `tasks/refine_phase.go`: the drain step.
- `tasks/run_tasks.go`: `RunTaskSetOptions` (~15–90), which holds the test-only `refineRunner`/`verifyRunner` and the `ImplementationConvention`/`DocumentOverlay` seams, and `implementRun.loop` (~185–252).
- `tasks/prompt.go`: `BuildAgentPrompt(taskPath, runtimePath, implementationConvention, explorationReport string)` (~140). It takes no manifest or Deps; callers resolve the report path with `explorationBlock` (`explore.go`). Its callers are `run_selected_task.go:94` and `executor.go:275`. `BuildAssistPrompt` is at ~529.
- Templates are in `tasks/prompts/` (`agent.tmpl.md` 74 lines, `assist.tmpl.md`, `refiner.tmpl.md`, `partials.tmpl.md`, which defines `latest-refine-report`). Goldens are in `tasks/testdata/prompts/`.
- `config/config.go`: `RefineConfig` (~351, `Enabled bool`, `Agents`, `Effort`), `WorkConfig.Refine` (~555), `RefineSettings()` (~1231), and `DashboardKillPanePromptEnabled` (~1714), which is the `*bool` default-true read. `config/agent_list.go` has `RefineAgentList`.
- `cmd/tasks.go`: `taskRefineCmd` (~169), its flags (~435–442), `runTaskRefineWith` (~1032), the `implementationConvention`/`refineOverlay` seams (~1120–1142), and implement wiring through `implement.RunWholeSetWith` (~1400–1430). There are no `--no-refine`/skip flags on implement.
- `tasks/drain/queue.go`: `Decision` (~379), `selectReadySets` (~1065; selects only `StatusReady && AutoDrain`), and `SpawnWithResult` (~1264), which spawns the literal `pop tasks implement <set>`.

### Which seam owns what
- **Report file mechanics:** `passReport` holds `dir`, `fileInstant`, `latestDocument`, `writeDocument` (timestamped, never overwrites), `renderDocument`/`renderDocumentBy` (header: title, written, commit range, `Work SHA`, agent) and `latestPointer`. `reportHeaderField` reads the header back. `ReportPointer.CommitPhrase`/`Summary`/`StaleAgainst` are the pointer shape. `latestDocument`/`latestPointer` both scan a sub-directory, so a flat root file has no ready reader.
- **Refine pass:** `RefineTaskSetWith` resolves paths, config and repo, then calls `refineResolvedSet`. That function does: `loadVerifiableManifest` (refuses malformed or unknown sets) → `AcquireTreeStable` unless `checkoutHeld` → prompt → `runRefiner` → `splitRefinerReply` → render and write → `commitRefinePass` → episode. It writes nothing before `runRefiner` returns. `runRefiner` errors on empty prose.
- **Agent precedence:** `resolveRefiner`: CLI → `m.RefinerOverride()` → `cfg.RefineSettings()` effort → `ResolveAgentGroupPresets(cfg.RefineAgentList())` → `DefaultRefineEffort`.
- **Agent walk:** `runAgentFallbackWalk(d, agentFallbackWalk{role: agentRole{…}})`. The role sets `Noun`, `Gerund`, `Phase`, `Persist`/`PersistAnswer`/`PersistSkipped` and `RetryEligible`. `runConfiguredRefiner` passes `maxTries: config.DefaultTaskMaxTries`. When the cap is spent, the walk calls `recordSpentRetryCap` with `role.Phase` (`agent_fallback_walk.go:325`).
- **Phase words:** `spendPhaseImplement/Verify/Refine` (`spent_retry_cap.go`). Captured runs of every set-level role are written by `writeCapturedRun` through `persistSetLevelRun(d, errOut, phase, …)` (`stream.go`), with the role's `spendPhase*` word as the phase. `capturedRunMeta.Phase`/`Verdict` hold the phase and the role's outcome. The phase string is matched by hand in `phaseOrder` (`stream.go` ~134), `setLevelRunStream` (`stream_tracer.go` ~215), `spendBreakdownRowKey`/`spendRefineRowKey` (`spend.go` ~811–822) and `latestPhaseRun` (`refine_mark.go`, which takes the phase word).
- **Drain step:** `implementRun.refinePhase` returns `refineFallThrough`/`refineReturn`. It declines on `!refineEnabled(cfg)`, on a non-terminal row, on `m.HumanCompleted` and on `m.RefineOptedOut()`, and passes `checkoutHeld: true`. It runs at the head of the quiescence branch. `implementRun.explorePhase` (`explore_phase.go`) runs once before the loop's `for`, ahead of the first task selection.
- **Status:** `DeriveStatus` (`status.go` ~126) works from the manifest only. `DeriveStatusWithVerdict`/`ResolveVerifiedStatus` (`verified_status.go`) add the verdict, and `decorateRowWithVerdict` (`verify_status.go` ~221) writes `Row.Status`/`VerifyMark`. `work.SetStatus`/`work.VerifyMark` (`work/cell.go`) are constant-free aliases.
- **Refine mark:** `ResolveRefineMark` gives a `RefineResolution{Mark, Reason}` derived from the latest `refine` captured run. It is rendered by `refineMarkPhrase` in `tasks/render.go:796` (`pop tasks status <set>`), in `gate_menu.go` (`resolveGateRefineState`, `gateRefinePreamble`, `gateRefineEntryDetails`) and in `BuildAssistPrompt`. It is not on the dashboard row or in `pop work status`. Those carry `VerifyMark` and the Explore mark (`ExploreMark`/`ExploreReason`, from `Row.Explore`) through `setkind.go` → `WorkRowStatusSegments` (`work_row.go` ~192).

### Invariants that hold
- `validateNoOrphanMarkdown`: any root `.md` that is not listed and is not the spec or `exploration.md` makes the set MALFORMED.
- Set keys are opt-out. `optedOut` and `agentDirective` read a malformed value as participating/nil, and both keys pass through `Unknown`. The guide text states "opt-**out** only".
- `refineEnabled` defaults off. Hand-run `pop tasks refine` ignores the config gate and the `refine` key.
- Pass reports live under the set dir in Task storage, never in the repository (`passReport` doc). Work SHA is stamped by pop, not asked of the agent.
- A timed-out or failed attempt never supersedes a report (`runConfiguredRefiner` checks `AnswerRetryEligible`).
- The existing word "parked" means `drain.DeferParked` → `Container.Parked`, the abnormal-exit park (`tasks/drain/deferral.go`). `StatusBlocked` is human-blocked (CONTEXT.md ~553–562 holds the drain-stop list).
- `agent.tmpl.md`: line 9 forbids editing the manifest and other task files, and the edit-boundary paragraph names the task file, and the Exploration report when the set has one, as the files outside the checkout the builder may edit. The `## Before you hand-write mechanism` section is the prior-art block.
- `refineBlock` names a report by path, never by body (`partials.tmpl.md` `latest-refine-report`).

### The test shape
- Whole-path refine tests: `tasks/refine_test.go` (`setupRefineFixture`, `refineOpts` with a `run` stub) and `tasks/refine_retry_test.go` (scripted agent streams through the real walk).
- Drain tests: `tasks/drain_refine_test.go` uses `setupRunTaskSetFixture` (`run_tasks_test.go:3303`) and `refineEnabledConfig()`. It asserts on the captured-run phase (`TestRefineRunIsCapturedUnderItsOwnPhase`) and on terminal status with refine on and off.
- Orphan check: `tasks/orphan_markdown_test.go` (`TestSpecMarkdownIsNotAnOrphan`).
- Artifact order: `cmd/tasks_artifacts_test.go`, `cmd/tasks_artifacts_render_test.go`, `tasks/setkind/artifacts_test.go`, and `tasks/setkind/refine_section_test.go` (asserts the detail section carries no path or prose).
- Guide drift: `tasks/authoring_guide_test.go` (`TestTaskGuideEnumsEqualTheValidatorsSets`, `TestTaskGuideCarriesTheJudgmentRules`).
- Prompts: `tasks/prompt_golden_test.go` uses the fixed `goldenSetDir`/`goldenFixtureDeps`/`goldenFullManifest`/`goldenBareManifest` and `prompttest.Assert(t, goldenPath(...), ...)`. Agent goldens are `agent.md`, `agent.implementation-convention.md` and `agent.exploration.md`, and assist goldens are `assist.{full,bare,empty-progress}.md`. `tasks/refine_surfaces_test.go:216` also renders `BuildAgentPrompt`.
- Status cell: `tasks/work_row_test.go` (~248–253).

### Read these in ranges
- `tasks/run_tasks.go` 178–252 (loop). `tasks/run_tasks_test.go` 3303–3340 (fixture).
- `tasks/manifest.go` 15–30, 335–420, 505–531, 630–721.
- `config/config.go` 322–375, 545–565, 1215–1240, 1708–1720.
- `cmd/tasks.go` 160–200, 405–442, 1028–1142, 1375–1430.
- `tasks/drain/queue.go` 375–420, 714–890, 1065–1100, 1255–1295.
- `tasks/stream.go` 85–160, 400–445. `tasks/render.go` 63, 355–365, 715–800. `tasks/prompt.go` 130–165, 529–582.
- `dashboard/dashboard.go` (4784 lines) — read only by grep. The mark predicate is at line 495.
