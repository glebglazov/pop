# Exploration report — 2026-09-07-exploration-report

- Explored: 2026-10-10T18:37:54Z
- Work SHA: 647143bb0a85
- Explorer: claude --model "opus"

## The code as found at 647143bb

This report describes the tree at commit `647143bb` ("fix(flaky-gates): the cmd tests no longer race…"). Nothing in it names `explore`, `exploration` or `explorer`. The only exception is `docs/adr/0262-…`.

### Where things live
- `tasks/artifacts.go`: the `ArtifactType*` constants, `ProgressFileName`, and `RefineDirName`/`VerifyDirName`. `Artifacts()` reads flat files with a `switch` on `SpecFileName`/`ExplorationFileName`/`ProgressFileName`. It then scans `reportArtifactKinds = []passReport{verifyReport, refineReport}`, and sorts by `artifactTierOrder` (verify, refine, spec, exploration, progress). `LatestArtifactSummary` reports the head row of that list.
- `tasks/manifest.go`:
  - `SpecFileName` is at line 23, beside `legacySpecFileName`.
  - `parseManifestJSON` (~342) has explicit cases only for `auto_drain`, `worktree`, `source_map`, `base_commit`, `commit_convention` and `human_completed`. Every other key, `verify`/`refine`/`verifier`/`refiner` included, goes into `m.Unknown`.
  - `validateNoOrphanMarkdown` (~505) exempts only `SpecFileName`, `legacySpecFileName` and `ExplorationFileName`.
  - The accessors are `VerifyOptedOut`/`RefineOptedOut`, both through `optedOut(key)`, and `VerifierOverride`/`RefinerOverride`/`ExplorerOverride`, all through `agentDirective(key)` over `AgentDirective{Agents, Effort}`. `ExploreRequested` reads `"explore"` as opt-in. The verify/refine key names are string literals; `ExploreKey`/`ExplorerKey` are constants beside `ExplorationFileName`.
- `tasks/authoring_guide.go`: `AuthoringGuide()`. The `verify`/`refine` and `verifier`/`refiner` bullets (~205–215) are hand-written prose. `tasks/authoring_guide_test.go` compares only the enums (`type`, `status`, …) against the validator's sets, through `enumInGuide`.
- `tasks/refine.go`, the sibling pass:
  - `refineReport` (a `passReport` value), `RefineOptions` (public) and `refineCoreOptions` (has the `runRefiner` test seam).
  - `RefineTaskSetWith` → `refineResolvedSet` → `runRefiner` → `resolveRefiner`. The precedence in `resolveRefiner` is CLI → `m.RefinerOverride()` → `cfg.RefineSettings()` → `cfg.RefineAgentList()` → `DefaultRefineEffort`.
  - `runConfiguredRefiner` uses `config.DefaultTaskMaxTries`. `refinerRole` builds the `agentRole`. `buildRefinerPrompt` and `refinerPromptView` build the prompt. The spec reaches the prompt through `readSpec(d, m)`.
- `tasks/pass_report.go`: `passReport` with `dir`, `fileInstant`, `latestDocument`, `writeDocument` (timestamped, never overwrites), `renderDocument`/`renderDocumentBy` (the header: Written / Commit range / Work SHA / agent), `reportHeaderField`, and `ReportPointer` with `latestPointer`, `CommitPhrase`, `Summary` and `StaleAgainst`.
- `tasks/agent_fallback_walk.go`: `agentRole` (`Noun`, `Gerund`, `Phase`, `Persist*`, `RetryEligible`, `ReadOnly`) and `runAgentFallbackWalk`. A Spent retry cap is filed with `Phase: w.role.Phase` (~327).
- `tasks/spent_retry_cap.go`: `spendPhaseExplore`, `spendPhaseImplement`, `spendPhaseVerify` and `spendPhaseRefine`.
- Captured runs:
  - `tasks/stream.go`: `capturedRunMeta.Phase` and `.Verdict` (~93), and `phaseOrder` (explore 0, implement 1, verify 2, refine 3, default 4).
  - `persistSetLevelRun` (`tasks/stream.go`) passes the role's phase to `writeCapturedRun`; `refinerRole` passes `spendPhaseRefine`.
  - `tasks/spend.go`: `spendBreakdownRowKey`, `spendVerifyRowKey`, `spendRefineRowKey` and `spendExploreRowKey` (~835).
  - `tasks/stream_tracer.go`: `setLevelRunStream` (~205) lists `explore`, `verify` and `refine`.
- `tasks/refine_phase.go`: `refinePhase` and `refineDirective` (`refineFallThrough`, `refineReturn`). `tasks/refine_mark.go`: `RefineMark`, `UnrefinedReason`, `ResolveRefineMark`, `latestSetLevelRun(d, m, phase)` (reads `listCapturedRunMetas` and filters on `Phase`; the Refine mark asks for `refine`, the Explore mark and park through `resolveExplore` in `tasks/explore_mark.go` for `explore`) and `refineMarkPhrase`.
- `tasks/run_tasks.go`: `RunTaskSetOptions` (~14–90) holds the `ImplementationConvention`/`DocumentOverlay`/`VerificationConvention` seams and the test-only `verifyRunner`/`refineRunner`/`exploreRunner`. `implementRun.loop` (~186) runs `explorePhase` (`tasks/explore_phase.go`) once at its head, then repeats Refresh → `SelectTaskInSet`. When nothing is selectable it runs `refinePhase` → `verifyPhase` → `terminalStatus`. Otherwise it runs `runSelectedTask`.
- `tasks/implement_run.go`: `implementRun` and `newImplementRun`, which resolve `turnCap` and `implementationConvention` once (~180). `tasks/executor.go:275` and `tasks/run_selected_task.go:94` are the two callers of `BuildAgentPrompt`.
- Prompts:
  - `tasks/prompt.go`: `BuildAgentPrompt(d, taskPath, runtimePath, implementationConvention)` takes no `Manifest`, and builds `agentPromptView`. `BuildAssistPrompt` (~529) sets `view.Refine = refineBlock(...)` and `view.RefineMark`.
  - `tasks/prompts/*.tmpl.md` are embedded through `promptTemplates`. `partials.tmpl.md` defines `latest-refine-report`, which `assist.tmpl.md:15` renders.
  - In `agent.tmpl.md`, the `Do NOT modify` list is at line 9, the edit boundary at 14–31, and `## Before you hand-write mechanism` at 33–44.
- `tasks/refine_pointer.go`: `RefinePointer = ReportPointer`, `latestRefinePointer`, `refineBlockView` and `refineBlock`.
- `config/config.go`:
  - `RefineConfig` (~351) has `Enabled bool` (zero value means off), `Agents` and `Effort`. It sits on `WorkConfig.Refine` (~555) with `merge:"fields" include:"fields"`.
  - The accessors are `RefineSettings`/`RefineAgentEntries` (~1216–1236).
  - The `*bool` default-on pattern is `MonitorDashboardConfig.KillPanePromptEnabled` (~122), read by `DashboardKillPanePromptEnabled` (~1714).
  - `tasks/agent_catalog.go:119`: `AgentGroupOrder` and the `AgentGroupCatalogs` map list `refine` by literal.
- `cmd/tasks.go`: `taskRefineCmd` (~169), its flags (~435–442) and `runTaskRefineWith` (~1032). The seams `implementationConvention`, `refineOverlay` and `verificationConvention` are at ~1115–1176. The implement wiring is `runTaskRunTasksWith` (~1402).
- Status, marks and surfaces:
  - `tasks/status.go`: the `Status*` constants (20–36), `Row` (`VerifyMark`, `VerifiedAt*`), `DeriveStatus` (~117), `TerminalStatus` and `DeriveStatusWithVerdict`.
  - `tasks/verified_status.go`: `ResolveVerifiedStatus` and `VerifiedResolution`. `tasks/verify_mark.go`: `VerifyMark` is an alias of `work.VerifyMark` (`work/cell.go`), carried on `work.Container.VerifyMark` (`work/kind.go:281`).
  - `tasks/verify_status.go`: `decorateRowWithVerdict` (~215).
  - `tasks/work_row.go`: `WorkRowStatusSegments` (~192) builds the status cell.
  - `tasks/gate_menu.go`: `gateRefineState`, `resolveGateRefineState`, `gateRefinePreamble`, `gateRefineEntryDetails` and `gateVerifyPreamble`.
  - `tasks/render.go:~790`: `renderRefineMark`, `renderVerifyReportPointer` and `renderArtifactSummarySection`.
- Drain and daemon:
  - `tasks/progress.go`: `AppendSetProgress` writes `progress.txt` with the file field set to `set`.
  - `tasks/drain/queue.go`: `Decision` (~379), `decideProjectDispatches` (~728) and `selectReadySets` (~1065). `supervisor/supervisor.go`: `tick` → `readPasses` → `dispatch`.
- Dashboard artifacts:
  - `tasks/setkind/artifacts.go`: `Kind.Artifacts` wraps `tasks.Artifacts`. `tasks/setkind/setkind.go:~440`: `artifactSections` makes the summary section.
  - `dashboard/dashboard.go:745–758` calls `artifactsFor`, and `:4162` renders `DetailSections`. `dashboard/render.go` holds only the status/badge styling, not an artifact section.

### Which seam owns what
- **Artifact membership and order:** only `tasks.Artifacts`. The CLI (`pop tasks artifacts`) and the dashboard (`setkind.Kind.Artifacts`) both read it.
- **Manifest key parsing:** `parseManifestJSON`. Key meaning comes from `Manifest` accessor methods that read `Unknown`.
- **Agent and effort precedence:** each role's own `resolve*` (for example `resolveRefiner`). The walk mechanics are `runAgentFallbackWalk`. The role-specific parts are only the `agentRole` fields.
- **Report file format and header read-back:** `passReport`. Each pass is a value of that type (`refineReport`, `verifyReport`).
- **Where `cmd` meets `tasks`:** the function-typed seams (`ImplementationConvention`, `DocumentOverlay`). The `conventions` package sits above `tasks`, so `cmd` wires these.
- **Drain sequencing:** `implementRun.loop`. Each phase method returns a typed directive.
- **What can be drained:** the `Row.Status` from `DeriveStatus`, with `ResolveVerifiedStatus` layered on top. The daemon spawns only `StatusReady && AutoDrain` rows (`selectReadySets`). Interactive selection is `SelectTaskSet`/`SelectTaskInSet` in `tasks/scheduler.go`.
- **Marks:** `ResolveVerifiedStatus` (verify) and `ResolveRefineMark` (refine). Every surface reads one of them.

### Invariants that hold
- Report directories sit under the set directory in Task storage, never in the repository (`passReport` comment).
- `refineResolvedSet` writes the report only after a non-empty reply. `runConfiguredRefiner` refuses prose from a retry-eligible attempt, so a failed pass leaves the earlier document in place.
- `refinePhase` cannot change a terminal status. It declines when `!refineEnabled(cfg)`, when the status is not `StatusDone`/`StatusAwaitingApproval`, when `m.HumanCompleted` is set, or when `m.RefineOptedOut()` is true. A hand-run `pop tasks refine` ignores the config gate and the opt-out.
- `AgentDirective` only steers. It never enables a phase. CLI flags win over it.
- A mark rides beside the status and is never put inside it (`VerifyMark`, `RefineMark`). `ResolveRefineMark` stores nothing and derives from Captured runs alone.
- Pointers carry a path plus `CommitPhrase`, never the body (`gateRefinePreamble`, `latest-refine-report`). `StaleAgainst` is the existing freshness compare, and only `refineBlock` reads it.
- Set-level runs carry an empty `TaskFile`. `setLevelRunStream` and `spendBreakdownRowKey` depend on that.
- `CONTEXT.md:557–563` lists the drain-stop statuses as DONE, FAILED, BLOCKED, AWAITING-APPROVAL, VERIFY-FAILED, EXPLORE-FAILED and DEFERRED.

### The test shape
- **Prompt goldens:** `tasks/prompt_golden_test.go` uses the `promptFixtureFS`, `goldenFullManifest`/`goldenBareManifest` and `bareDeps` fixtures. It compares through `prompttest.Assert` and regenerates with `-update-prompt-goldens`. The goldens live in `tasks/testdata/prompts/` (`agent.md`, `assist.*.md`, `refiner.*.md`).
- **Refine pass:**
  - `tasks/refine_test.go` covers `setupRefineFixture` and `refineOpts`, which drive `refineResolvedSet` with a stub `runRefiner`.
  - `tasks/refine_retry_test.go`: `refineRunnerDeps` and `scriptedRefineRun` drive the real walk and Captured runs.
  - `tasks/drain_refine_test.go` drives a whole drain through `setupDrainRefineFixture` with `opts.refineRunner`/`opts.verifyRunner`.
  - `tasks/refine_surfaces_test.go` covers the assist and agent prompt surfaces.
- **Drain fixtures:** `runTaskSetFixture` and `setupRunTaskSetFixture` (`run_tasks_test.go:3303`). `setupRunTaskSetFixtureWithKeys` adds manifest keys (`drain_verify_test.go:337`).
- **Artifacts:** `cmd/tasks_artifacts_test.go`, `cmd/tasks_artifacts_render_test.go`, `tasks/setkind/artifacts_test.go` and `tasks/setkind/refine_section_test.go`.
- **`cmd` coverage gap:** no `cmd` test calls `runTaskRefineWith`.

### Read these in ranges
- `tasks/run_tasks.go` 14–110 and 186–260
- `tasks/run_tasks_test.go` 3303–3360
- `config/config.go` 105–125, 350–370, 540–570 and 1210–1240
- `cmd/tasks.go` 165–200, 430–445, 1028–1060, 1110–1180 and 1395–1430
- `tasks/drain/queue.go` 375–430 and 1060–1175
- `dashboard/dashboard.go` 740–760 and 4155–4240
- `tasks/prompt.go` 125–165 and 520–575
- `tasks/manifest.go` 335–420, 500–535 and 630–721
- `tasks/stream.go` 88–145 and 395–445
- `CONTEXT.md` 549–561
