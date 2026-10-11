# Exploration report — 2026-09-07-exploration-report

- Explored: 2026-10-10T06:39:53Z
- Work SHA: 647143bb0a85
- Explorer: claude --model "opus"

## Where things live

- `tasks/artifacts.go`: the artifact type constants (`ArtifactTypeRefine/Verify/Spec/Progress`), `ProgressFileName`, and `Artifacts()`. `Artifacts()` lists root files by an explicit `switch` on `SpecFileName`/`ExplorationFileName`/`ProgressFileName`. It then scans `reportArtifactKinds` (`verifyReport`, `refineReport`, which are timestamped sub-directories). `artifactTierOrder` is the sort order. `LatestArtifactSummary` reports the head row of that order.
- `tasks/setkind/artifacts.go`: `Kind.Artifacts` maps `tasks.Artifacts` onto `work.Artifact`. `tasks/setkind/setkind.go`'s `artifactSections` (~l.440) gives the dashboard detail its "Artifacts" section. `dashboard/render.go` has no artifact code: the dashboard gets artifacts only through the `work.ArtifactSource` seam.
- `tasks/manifest.go`: `parseManifestJSON` (~l.330–415) reads named set keys. Every other key goes into `m.Unknown`. `optedOut(key)` backs `VerifyOptedOut`/`RefineOptedOut`. `agentDirective(key)` backs `VerifierOverride`/`RefinerOverride`, and both return `*AgentDirective{Agents, Effort}`. `validateNoOrphanMarkdown` (~l.515) exempts only `SpecFileName`, `legacySpecFileName` and `ExplorationFileName`.
- `tasks/authoring_guide.go`: `AuthoringGuide()`. Its "Set-level keys" block (~l.193–225) documents `verify`/`refine` as opt-out and `verifier`/`refiner` as steering objects.
- `tasks/refine.go`: the Refine pass. `RefineTaskSetWith` resolves the paths and calls `refineResolvedSet`. `refineResolvedSet` runs the Refiner, renders and files the report, then makes the commit. Also here: `resolveRefiner` (the precedence chain), `runConfiguredRefiner`, `refinerRole` and `refineEnabled`.
- `tasks/agent_fallback_walk.go`: `agentRole` (`Noun`, `Gerund`, `Phase`, `Persist*`, `RetryEligible`, `ReadOnly`) and `runAgentFallbackWalk`.
- `tasks/pass_report.go`: `passReport` (`dir`, `fileInstant`, `latestDocument`, `writeDocument`, `renderDocument`), `ReportPointer` (`CommitPhrase`, `Summary`, `StaleAgainst`) and `reportHeaderField`.
- `tasks/stream.go`: `capturedRunMeta.Phase`, `phaseOrder`, and `persistRefineRun`/`persistSkippedRefineRun`. These call `persistSetLevelRun(..., "refine", ...)`, which calls `writeCapturedRun`.
- `tasks/run_tasks.go`: `implementRun.loop` (l.190). `tasks/refine_phase.go`: `refinePhase` and `refineDirective`.
- `tasks/prompt.go`: `BuildAgentPrompt(taskPath, runtimePath, implementationConvention, explorationPath)` (l.139) and `BuildAssistPrompt` (l.536). `tasks/refine_pointer.go`: `refineBlock` and `refineBlockView`.
- `tasks/gate_menu.go`: `gateRefineState`, `gateRefinePreamble` and `gateVerifyPreamble`. `tasks/render.go`: `renderRefineMark`, `renderVerifyReportPointer` and `renderArtifactSummarySection` (~l.809–865).
- `config/config.go`: `RefineConfig` (l.354), `WorkConfig.Refine` (l.573), `RefineSettings()` (l.1251) and `KillPanePromptEnabled *bool` (l.122).
- `cmd/tasks.go`: `taskRefineCmd` (l.174) with its flags (l.465–472), `runTaskRefineWith` (l.1069), and the seams `implementationConvention`/`refineOverlay` (l.1185–1210).

## Which seam owns what

- **Artifact membership and order:** `tasks.Artifacts`. The CLI (`cmd` `pop tasks artifacts`), `render.go` and `setkind` all read it, and none of them re-sorts.
- **Set-key parsing:** `parseManifestJSON`. A key that has no `case` goes to `m.Unknown`, and the accessors read it back from there. `validateManifest` does not check set-level keys.
- **Role differences in an agent pass:** `agentRole` only. The walk itself has no role-specific code.
- **Agent and effort precedence:** `resolveRefiner`. The order is CLI, then `m.RefinerOverride()`, then `[work.refine]`, then `RefineAgentList`/`DefaultRefineEffort`.
- **Drain step order:** the quiescence branch of `implementRun.loop` (when `SelectTaskInSet` errors) runs `refinePhase`, then `verifyPhase`, then `terminalStatus`. Task execution is `runSelectedTask`.
- **Spend and phase vocabulary:** the `spendPhase*` constants in `tasks/spent_retry_cap.go`. The phase strings are also written out literally in `stream.go` (`phaseOrder`, `writeCapturedRun` calls), `stream_tracer.go` (l.215) and `spend.go` (l.840).
- **Status vocabulary:** `tasks/status.go` (`Status*`, aliasing `work.SetStatus`). `DeriveStatus` and `DeriveStatusWithVerdict` derive it. `ResolveVerifiedStatus` (`verified_status.go`) is the only place that adds the verdict on top. `decorateRowWithVerdict` (`verify_status.go` l.221) writes `row.VerifyMark`.
- **Refine mark:** `ResolveRefineMark` in `tasks/refine_mark.go`, not `verified_status.go`. It is read by `prompt.go` l.562, `render.go` l.828 and `gate_menu.go` l.158. No non-test dashboard code reads `RefineMark`.
- **Supervisor dispatch:** `drain.Decision` (`tasks/drain/queue.go` l.379), produced by `decideProjectDispatches` (l.728).

## Invariants that hold

- The spec and `exploration.md` are the only root-level markdown files that `validateNoOrphanMarkdown` exempts. Any other `.md` file makes the set MALFORMED.
- A boolean that does not parse leaves the set participating (`optedOut`). A malformed directive object gives nil (`agentDirective`). Unread keys survive a rewrite through `m.Unknown`.
- Reports are filed under the set directory in Task storage, never in the checkout (comment on `passReport`). `writeDocument` never overwrites: on a name clash it moves the instant forward.
- `runConfiguredRefiner` returns no answer when `AnswerRetryEligible` is set, so a partial reply never replaces an earlier report. `refineResolvedSet` writes the report before it makes the commit.
- Every refine outcome returns `refineFallThrough`, except an interrupt, which returns `refineReturn`. The phase skips sets with `m.HumanCompleted` or `RefineOptedOut()`. The hand-run verb ignores both.
- `refineEnabled` defaults to off (`Enabled bool`).
- The agent prompt carries the `## Before you hand-write mechanism` block and the `Do NOT modify {{.ManifestPath}}` / other task files lines (`agent.tmpl.md` l.9–30).
- `BuildAgentPrompt` takes no `Deps` or `Manifest`. Its callers are `run_selected_task.go` l.94 and `executor.go` l.275.
- `CONTEXT.md` l.567 lists the drain-stop statuses as DONE, FAILED, BLOCKED, AWAITING-APPROVAL, VERIFY-FAILED, EXPLORE-FAILED and DEFERRED.

## The test shape

- Tests that guard the guide against drift are in `tasks/authoring_guide_test.go` (`TestTaskGuideEnumsEqualTheValidatorsSets` and the tests after it).
- Prompt goldens: `tasks/prompt_golden_test.go` (`TestAgentPromptGoldens`, `TestAssistPromptGoldens`, `TestRefinerPromptGoldens`, …) against `tasks/testdata/prompts/*.md`.
- Refine tests are in `tasks/refine_test.go`, `refine_retry_test.go`, `refine_mark_test.go`, `refine_surfaces_test.go`, `refiner_envelope_test.go` and `tasks/setkind/refine_section_test.go`. Artifact tests are in `tasks/setkind/artifacts_test.go`, `cmd/tasks_artifacts_test.go` and `cmd/tasks_artifacts_render_test.go`.
- Tests replace the agent through function fields:
  - `refineCoreOptions.runRefiner` for a pass.
  - `refineRunner` / `verifyRunner` on the run options (`run_tasks.go` l.65–88) for a drain.

## Read these in ranges

- `tasks/run_tasks_test.go` (3553 lines): grep for the test names you need.
- `dashboard/dashboard.go` (4784 lines): status cells are found through `dashboardStatusCellStyled`.
- `config/config.go` (2823 lines): l.345–372, l.558–578 and l.1230–1260.
- `cmd/tasks.go` (2208 lines): l.165–205, l.460–475 and l.1065–1210.
- `tasks/agent.go` (1597 lines): only the role presets.
- `tasks/drain/queue.go` (1336 lines): l.375–450 and l.728–800.
- `CONTEXT.md`: l.550–570.
