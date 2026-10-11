# Exploration report — 2026-09-07-exploration-report

- Explored: 2026-10-10T07:55:45Z
- Work SHA: 647143bb0a85
- Explorer: claude --model "opus"

## Where things live

- `tasks/artifacts.go`: the artifact type constants (`ArtifactTypeVerify/Refine/Spec/Exploration/Progress`), `ProgressFileName`, and `Artifacts()`. `Artifacts()` recognises flat files with a `switch` on `SpecFileName`/`ExplorationFileName`/`ProgressFileName`. It scans `reportArtifactKinds` (`verifyReport`, `refineReport`) for the timestamped families, then sorts by `artifactTierOrder` (verify, refine, spec, exploration, progress). `LatestArtifactSummary` gives the head row.
- `tasks/manifest.go`: `SpecFileName` (line 23). `parseManifestJSON` (~line 340) puts every unread key into `m.Unknown`. `validateNoOrphanMarkdown` (~line 515) exempts only `SpecFileName`, `legacySpecFileName` and `ExplorationFileName`. `VerifyOptedOut`/`RefineOptedOut` go through `optedOut` (~line 641). `AgentDirective` with `VerifierOverride`/`RefinerOverride` goes through `agentDirective(key)` (~line 676–721).
- `tasks/authoring_guide.go`: `AuthoringGuide()`. The `verify`/`refine` opt-out bullet and the `verifier`/`refiner` bullet are at ~line 203–215. Tests: `tasks/authoring_guide_test.go` (`TestTaskGuideEnumsEqualTheValidatorsSets`, `TestTaskGuideCarriesTheJudgmentRules`).
- `tasks/refine.go`: `pop tasks refine`. The path is `RefineTaskSetWith`, then `refineResolvedSet`, then `runRefiner`, then `resolveRefiner`/`runConfiguredRefiner`. Other parts: `refinerRole`, `buildRefinerPrompt` with `refinerPromptView`, and `refineReport` (a `passReport` value).
- `tasks/agent_fallback_walk.go`: `agentRole` (`Noun`, `Gerund`, `Phase`, `Persist*`, `RetryEligible`, `ReadOnly`), `agentFallbackWalk`, `runAgentFallbackWalk`.
- `tasks/pass_report.go`: `passReport`. `renderDocument`/`renderDocumentBy` write the header facts (Written, Commit range, Work SHA, agent). `writeDocument`/`latestDocument`/`fileInstant` are the timestamped mechanics. `ReportPointer`, `reportHeaderField`, `CommitPhrase`, `Summary` and `StaleAgainst` are also here.
- `tasks/stream.go`: `capturedRunMeta.Phase`/`Verdict`, `writeCapturedRun`, `phaseOrder` (explore 0, implement 1, verify 2, refine 3, other 4), and `persistRefineRun`/`persistSkippedRefineRun`/`persistExploreRun` (~line 404), which all file through `persistSetLevelRun`. Phase strings are also matched literally in `tasks/spend.go:840-847` and `tasks/stream_tracer.go:213-219`.
- `tasks/spent_retry_cap.go`: `spendPhaseImplement/Verify/Refine`.
- `tasks/prompt.go`: `BuildAgentPrompt(d, taskPath, runtimePath, implementationConvention)` (~line 139). It takes no `Manifest`; it resolves the Exploration report beside the task file through `explorationBlock` (`tasks/exploration_pointer.go`). Its callers are `tasks/run_selected_task.go:94` and `tasks/executor.go:275`. Also here: `BuildAssistPrompt` (~line 529) with `assistPromptView.Refine`, and the four assistance builders, which all carry `refineBlock(...)`.
- `tasks/refine_pointer.go`: `RefinePointer = ReportPointer`, `latestRefinePointer`, `refineBlockView`, `refineBlock`.
- `tasks/prompts/agent.tmpl.md`: the edit-boundary lines 9–18 ("Do NOT modify {{.ManifestPath}}…", "The task file above is the one file you also edit", or the task file and the Exploration report when the set has one), the Exploration report block at lines 20–24, and `## Before you hand-write mechanism` at lines 25–36.
- `tasks/prompts/assist.tmpl.md` and `partials.tmpl.md` (`latest-refine-report`, `exploration-report`, `task-listing`).
- `tasks/run_tasks.go`: `implementRun.loop` (~line 186). The `for` begins with `RefreshWith` and then `SelectTaskInSet` (`tasks/scheduler.go:140`). `refinePhase` is the first call in the `selErr != nil` branch. `RunTaskSetOptions` carries `ImplementationConvention`, `DocumentOverlay` and the test seam `refineRunner` (~line 69–88).
- `tasks/refine_phase.go`: `refineDirective` (`refineFallThrough`, `refineReturn`) and `refinePhase`.
- `config/config.go`: `RefineConfig` (~line 351, `Enabled bool` defaults off), `WorkConfig.Refine` (~line 573), `RefineSettings`/`RefineAgentEntries` (~line 1238–1257), and the `*bool` pattern `KillPanePromptEnabled` (line 122) read by `DashboardKillPanePromptEnabled` (~line 1761).
- `tasks/status.go`: the `TaskSetStatus` constants (lines 19–41), `DeriveStatus`, `TerminalStatus`, `DeriveStatusWithVerdict`.
- `tasks/verified_status.go`: `ResolveVerifiedStatus` returns `VerifiedResolution{Status, Mark, …}`. `tasks/verify_status.go:decorateRowWithVerdict` writes `row.VerifyMark`.
- `tasks/refine_mark.go`: `RefineMark`, `UnrefinedReason`, `ResolveRefineMark`, `latestRefineRun` (reads `listCapturedRunMetas` and filters `Phase == "refine"`), `refineMarkPhrase`.
- `tasks/gate_menu.go`: `gateRefineState`, `resolveGateRefineState`, `gateRefinePreamble`, `gateRefineEntryDetails`, `gateVerifyPreamble`.
- `tasks/render.go`: `renderTaskSetDetail` (~line 720). It calls `renderRefineMark`, `renderVerifyReportPointer` and `renderArtifactSummarySection`. `verifyMarkDetailParts` is at ~line 575.
- Dashboard path: `tasks/setkind/artifacts.go:Artifacts` wraps `tasks.Artifacts`. `tasks/setkind/setkind.go` fills `work.Container.VerifyMark` (~line 411) and `DetailSections` from `artifactSections` (~line 441). `StatusCell` delegates to `tasks.WorkRowStatusSegments`. `work/kind.go:281` is `Container.VerifyMark`.
- Drain scan: `tasks/drain/queue.go` holds `Decision` (~line 379) and `selectReadySets` (~line 1065, which picks `row.Status == tasks.StatusReady && row.AutoDrain`). `tasks/drain/status.go:BuildStatus`/`StatusFromDecisions` build what `pop work status` prints.
- `tasks/progress.go`: `AppendProgress` and `AppendSetProgress` (task file `"set"`).
- `cmd/tasks.go`: `taskRefineCmd` (~line 174), its flags (~line 465–472), `runTaskRefineWith` (~line 1069), the seams `implementationConvention`/`refineOverlay` (~line 1185–1210), and `runTaskArtifactsWith` (~line 1128).

## Which seam owns what

- **Artifact membership and order:** `tasks.Artifacts` and `artifactTierOrder`. The CLI (`runTaskArtifactsWith`) and the dashboard both read it through `setkind.Kind.Artifacts`.
- **Manifest key reading:** each set-level key that has a typed field has a `case` in `parseManifestJSON`. `verify`, `refine`, `verifier` and `refiner` have no case. They stay in `m.Unknown` and accessors read them (`optedOut`, `agentDirective`). Orphan markdown is `validateNoOrphanMarkdown`.
- **Agent precedence:** `resolveRefiner` goes CLI first, then `m.RefinerOverride()`, then `cfg.RefineSettings()`/`cfg.RefineAgentList()`, then `DefaultRefineEffort`. It returns a `verifierSelection`.
- **Walk versus role:** `runAgentFallbackWalk` owns retries, fallthrough and spent-cap recording (`Phase: w.role.Phase`, ~line 327). The role owns persistence and the retry rule.
- **Report document:** `passReport.renderDocument` stamps the Work SHA, and the agent does not write it. `reportHeaderField` reads it back for `ReportPointer.WorkSHA`.
- **Drain phases:** `implementRun.loop` orchestrates. Each phase method returns a directive type. `refinePhase` decides enabled → terminal zone → `m.HumanCompleted` → `RefineOptedOut` → episode armed. It calls `refineResolvedSet` with `checkoutHeld: true`.
- **Status versus mark:** `DeriveStatusWithVerdict` and `ResolveVerifiedStatus` own the status and the Verification mark. `ResolveRefineMark` is the only place that derives the Refine mark. It reads captured runs and has no store column.
- **Daemon pick:** `selectReadySets` passes over every row that is not `StatusReady`.

## Invariants that hold

- `artifactTier` sorts an unknown type after all known types (`tasks/artifacts.go`). The progress row is never ordered by recency.
- A `passReport` document never overwrites another: `writeDocument` advances the instant if a name is taken.
- `runConfiguredRefiner` returns no answer when `walked.AnswerRetryEligible` is true. A failed pass does not supersede an existing report.
- `refineResolvedSet` writes the report before `commitRefinePass`.
- Phase words are equal to spend-cap words (`spent_retry_cap.go` comment). Each phase reader filters on `Phase` before it reads `Verdict` (`capturedRunMeta` comment).
- The `AgentDirective` steers how a phase runs and never opts a set in (comment at `manifest.go` ~line 676). `verify` and `refine` are opt-out: absent or malformed means the set participates.
- `refinePhase` gates nothing. Only an interrupt returns from it.
- A hand-run `pop tasks refine` ignores `RefineOptedOut`, `refineEnabled` and the episode (`TestHandRefineIgnoresTheEpisode`).
- A report reaches a prompt or a surface only as a path (`refineBlock`, the `partials.tmpl.md` heading "NOT inlined", and the `ReportPointer` doc).
- `ResolveRefineMark` gives no mark for a set that is not terminal, is disabled or opted out.
- `StatusBlocked` means an open AFK task waits behind a HITL gate (`DeriveStatus`).

## The test shape

- Goldens: `tasks/prompt_golden_test.go` uses a stub FS at `goldenSetDir` (`goldenFixtureDeps`, `goldenFullManifest`/`goldenBareManifest`). `prompttest.Assert` compares output with `tasks/testdata/prompts/*.md`, and `-update-prompt-goldens` rewrites the files. `TestAgentPromptGoldens` covers `agent.md` and `agent.implementation-convention.md`.
- Refine end to end: `tasks/refine_test.go` drives `refineResolvedSet` through the `runRefiner` seam and asserts on the prompt and the report on disk. `tasks/drain_refine_test.go` drives whole drains through `opts.refineRunner` (`TestDrainRefinesBeforeVerifyAndOnlyOncePerEpisode`, `TestRefineRunIsCapturedUnderItsOwnPhase`). `cmd/` has no refine test.
- Artifacts: `cmd/tasks_artifacts_test.go`, `cmd/tasks_artifacts_render_test.go`, `tasks/setkind/artifacts_test.go`.
- Marks: `tasks/refine_mark_test.go`, `tasks/refine_surfaces_test.go`, `tasks/verified_status_test.go`.
- Daemon pick: `tasks/drain/queue_test.go:TestSelectReadySet` and `checkout_claim_dispatch_test.go` use table rows of `tasks.Row{Status, AutoDrain}`.

## Read these in ranges

- `tasks/run_tasks_test.go` (~3.4k lines): open only the table that has the case you need.
- `dashboard/dashboard.go` (4784 lines): ~480–500 (`row.VerifyMark` use). The status cells come from `tasks.WorkRowStatusSegments`, not from this file.
- `config/config.go` (2823 lines): 110–125, 345–370, 560–580, 1230–1260, 1755–1765.
- `cmd/tasks.go` (2208 lines): 165–230, 385–480, 1065–1210.
- `tasks/drain/queue.go` (1336 lines): 375–480, 714–800, 1060–1110.
- `tasks/agent.go` (1597 lines): read the role presets by symbol, not whole.
- `tasks/manifest.go`: 225–250, 330–420, 500–535, 636–721.
- `tasks/refine.go`: 1–240, 318–500, 535–614.
