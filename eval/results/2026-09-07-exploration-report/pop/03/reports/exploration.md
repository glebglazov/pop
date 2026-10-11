# Exploration report — 2026-09-07-exploration-report

- Explored: 2026-10-10T17:13:14Z
- Work SHA: 647143bb0a85
- Explorer: claude --model "opus"

## Exploration report: 2026-09-07-exploration-report

This report describes the tree at `647143bb`. Nothing in the set has been built yet.

## Where things live

- `tasks/artifacts.go` holds the artifact type constants (`ArtifactTypeVerify/Refine/Spec/Progress`, `ProgressFileName`). `Artifacts()` matches the flat files `spec.md`, `exploration.md` and `progress.txt` by name in a `switch`. It then scans `reportArtifactKinds` (`verifyReport`, `refineReport`) for the timestamped families. `artifactTierOrder` is the ordering: verify, refine, spec, exploration, progress.
- `tasks/manifest.go` has these parts:
  - `SpecFileName`, plus `legacySpecFileName` (`prd.md`).
  - A set-level key `switch` inside the parse, at ~354–414. Keys it does not name go to `m.Unknown`.
  - `validateNoOrphanMarkdown` (~507), which exempts only the spec names and `ExplorationFileName`.
  - `optedOut` (~661), with `VerifyOptedOut` and `RefineOptedOut` built on it.
  - `AgentDirective`, `VerifierOverride`, `RefinerOverride` and `agentDirective` (~676–721).
- `tasks/authoring_guide.go`: `AuthoringGuide()`. The "Set-level keys" prose (~194–220) writes `verify`/`refine`/`verifier`/`refiner` as literal strings, not as constants. Drift tests are in `tasks/authoring_guide_test.go`.
- `tasks/refine.go` is the hand-run pass:
  - Entry points: `RefineTaskSetWith`, then `refineResolvedSet`.
  - Agent resolution: `runRefiner`, `resolveRefiner`, `runConfiguredRefiner`.
  - Prompt: `buildRefinerPrompt` / `refinerPromptView` render `tasks/prompts/refiner.tmpl.md`.
  - The spec reaches the prompt through `readSpec` (`tasks/verify.go:1406`).
- `tasks/agent_fallback_walk.go`: `agentRole` (Noun, Gerund, Phase, `Persist`/`PersistAnswer`/`PersistSkipped`, `RetryEligible`, `ReadOnly`) and `runAgentFallbackWalk`.
- Captured runs:
  - `tasks/stream.go` has `writeCapturedRun`, `persistSetLevelRun`, `persistSkippedSetLevelRun`, and `phaseOrder` (~134), which knows explore, implement, verify and refine.
  - `tasks/stream_tracer.go:setLevelRunStream` and `tasks/spend.go:spendBreakdownRowKey` (~817) each have a `case` per set-level phase.
  - `tasks/spent_retry_cap.go` holds the `spendPhase*` constants.
- `tasks/prompt.go`:
  - `BuildAgentPrompt(d, taskPath, runtimePath, implementationConvention)` takes `Deps` and no `Manifest`. Its callers are `run_selected_task.go:94` and `executor.go:275`.
  - `BuildAssistPrompt` (~529) uses `refineBlock` and `refineMarkPhrase`.
  - The templates are `tasks/prompts/agent.tmpl.md` and `assist.tmpl.md`. The shared fragment `latest-refine-report` is in `partials.tmpl.md`.
- Drain:
  - `tasks/run_tasks.go` holds `RunTaskSetOptions` (~14) and `implementRun.loop` (~186).
  - `tasks/refine_phase.go` holds `refinePhase` and `refineDirective`.
  - `tasks/implement_run.go:newImplementRun` (~180) resolves `turnCap`, `implementationConvention` and `agentProbeMemo` once per run.
- CLI:
  - `cmd/tasks.go` has `taskRefineCmd` (~169), its flags (~435–442), `runTaskRefineWith` (~1032), and the `implementationConvention` / `refineOverlay` seams (~1115–1142).
  - `pop tasks implement` goes through `tasks/implement/run.go` (`WholeSetOptions`, which is copied into `RunTaskSetOptions` at ~100).
- `config/config.go`:
  - `RefineConfig` (~354) has `Enabled bool`, `Agents` and `Effort`.
  - `WorkConfig.Refine` (~555) and `RefineSettings()` (~1231).
  - `KillPanePromptEnabled *bool` (~122) is read through `DashboardKillPanePromptEnabled` (~1714). This is the default-on pattern.
  - `config/agent_list.go` has `KeyRefineAgents` and `RefineAgentList`. `tasks/agent_catalog.go` has `AgentGroupOrder` and `AgentGroupCatalogs`, which list `"refine"`.
- Status and marks:
  - `tasks/status.go` holds the `Status*` constants and `DeriveStatus` / `DeriveStatusWithVerdict`. `TaskSetStatus` is an alias of `work.SetStatus` (`work/cell.go:55`), which has no constants.
  - `tasks/verified_status.go` has `ResolveVerifiedStatus`. `tasks/verify_status.go:decorateRowWithVerdict` writes `Row.VerifyMark`.
  - `tasks/refine_mark.go` has `ResolveRefineMark`, `latestSetLevelRun` and `refineMarkPhrase`.
- Drain scan and daemon:
  - `tasks/drain/queue.go` has `Decision` (~379) and `decideProjectDispatches` (~705). These dispatch READY sets only (~1071).
  - `supervisor/supervisor.go` runs `tick` → `readPasses` (`Reconcile`, `Candidates`) → `dispatch`.
- Surfaces:
  - `tasks/setkind/setkind.go` maps a `tasks.Row` to a `work.Container` (~395–420, copies `VerifyMark`). `artifactSections` is at ~441.
  - `tasks/work_row.go:WorkRowStatusSegments` builds the STATUS cell, which `Kind.StatusCell` returns.
  - `tasks/render.go` has `renderRefineMark` (~795) and `verifyMarkDetailParts` (~575).
  - `tasks/gate_menu.go` has `gateRefineState`, `gateRefinePreamble` and `gateRefineEntryDetails` (~107–165).
  - `dashboard/status_render.go:RenderStatus` prints `pop work status`.

## Which seam owns what

- **Which files are artifacts:** `Artifacts()` owns it. `artifactTier` places each type by its index in `artifactTierOrder`. A type not in the list sorts last. `tasks/setkind/artifacts.go:Kind.Artifacts` re-exports this list for both the CLI (`runTaskArtifacts`, `cmd/tasks.go:1059`) and the dashboard (`dashboard/work_rows.go:129`).
- **Detail-view artifact list:** this is in `dashboard/dashboard.go` (`syncArtifacts` ~775, the table render ~4206–4340), not in `dashboard/render.go`. `dashboard/render.go` holds only status-cell and table-width helpers.
- **Which markdown is legal in a set folder:** `validateNoOrphanMarkdown` owns it. Unknown set-level JSON keys are never rejected: the `default:` arm keeps them in `m.Unknown`.
- **Participation in a phase:** `optedOut` treats a key as opt-out. Absent or malformed means the set takes part. Agent and effort steering is `agentDirective`.
- **Agent precedence:** `resolveRefiner` applies CLI flags, then `RefinerOverride`, then `[work.refine]`, then `RefineAgentList` / `DefaultRefineEffort`.
- **Where pass prose goes:** `passReport` is shared. `renderDocument` stamps the header (Written, Commit range, Work SHA, agent), and `reportHeaderField` reads it back. `latestDocument`, `writeDocument` and `latestPointer` handle only the timestamped files under `DirName`. `ReportPointer` carries `CommitPhrase`, `Summary` and `StaleAgainst`.
- **Phase label on a run:** `agentRole.Phase` sets it, and `persistSetLevelRun` passes the role's phase to `writeCapturedRun`. `ResolveRefineMark` reads only `capturedRunMeta.Phase/Outcome/Verdict` through `listCapturedRunMetas`.
- **Drain step order:** `implementRun.loop` owns it. Each iteration runs `RefreshWith`, then `SelectTaskInSet`. If selection fails, it runs `refinePhase`, then `verifyPhase`, then `terminalStatus`; otherwise it runs `runSelectedTask`. One step runs before the first `SelectTaskInSet`: `explorePhase` (`tasks/explore_phase.go`), once, ahead of the `for`.
- **cmd → tasks seams:** `ImplementationConvention` and `DocumentOverlay` are function types. `cmd` wires them because `conventions` sits above `tasks`.

## Invariants that hold

- `refineResolvedSet` writes the report only after `runRefiner` returns non-empty prose. `runConfiguredRefiner` refuses prose that `AnswerRetryEligible` marks as a fragment. The result: a failed pass writes nothing and leaves the old report in place.
- `refinePhase` never ends the drain except on `isInterrupted`. It declines on `!refineEnabled`, a non-terminal row, `m.HumanCompleted` or `RefineOptedOut`. A hand run (`RefineTaskSetWith`) ignores the opt-out.
- `refineEnabled` and `RefineConfig.Enabled` default to off through the zero value.
- `ResolveVerifiedStatus`, `ResolveRefineMark` and `ResolveExploreMark` are the only places marks are derived. Every surface calls them; none re-derives a mark.
- `ResolveRefineMark` stores nothing. It derives the mark from the latest phase-`refine` run.
- A mark is blank unless `TerminalStatus(DeriveStatus(m))`.
- `StatusBlocked` means a human-blocked set: an open AFK task waits behind HITL. The comments on `DeriveStatus` and `CONTEXT.md` (~553–563) say so. The drain-stop list is in the `CONTEXT.md` paragraph after the table.
- `work.Container.Parked` (`work/kind.go:220`) already means a park after repeated abnormal terminals.
- `agent.tmpl.md` has two rules that limit edits: the "Do NOT modify" lines (lines 9–10) and the edit-boundary paragraph after the runtime checkout line, which names the task file, and `exploration.md` when the set has one, as the files outside the checkout an attempt may edit. The `## Before you hand-write mechanism` block follows it.
- Reports and artifacts live in Task storage and are never committed. ADR-0214 is cited in `passReport`'s comment.
- `AppendSetProgress` writes a `[set]` record through `AppendProgress`.

## The test shape

- Artifacts: `tasks/setkind/artifacts_test.go`, `cmd/tasks_artifacts_test.go` (tier order is shared with the dashboard), `cmd/tasks_artifacts_render_test.go`.
- Orphan check: `tasks/orphan_markdown_test.go` (`TestSpecMarkdownIsNotAnOrphan`).
- Authoring guide: `tasks/authoring_guide_test.go` (`TestTaskGuideEnumsEqualTheValidatorsSets`, `TestTaskGuideCarriesTheJudgmentRules`).
- Hand pass:
  - `tasks/refine_test.go` uses `setupRefineFixture` and `refineOpts`, and injects `refineCoreOptions.runRefiner`.
  - `tasks/refine_retry_test.go` uses `refineRunnerDeps`, `scriptedRefineRunner` and `claudeRefineStream`. These drive the real walk through a fake `CommandRunner`.
- Drain: `tasks/drain_refine_test.go` uses `setupDrainRefineFixture` (a `runTaskSetFixture`) and `RunTaskSetOptions.refineRunner`. `TestRefineRunIsCapturedUnderItsOwnPhase` asserts on the run row. `TestHumanCompletedSetIsRefinedOnlyByHand` covers the human-completed decline.
- Marks and surfaces: `tasks/refine_mark_test.go` (`recordRefinePass`), `tasks/refine_surfaces_test.go`.
- Prompts: `tasks/prompt_golden_test.go` uses `goldenFixtureDeps`, `goldenFullManifest` and `goldenBareManifest`, and asserts through `prompttest.Assert` against `tasks/testdata/prompts/*.md`. `agent.md` and `agent.implementation-convention.md` are the implement goldens.

## Read these in ranges

- `cmd/tasks.go` (2143 lines): 169–200, 420–445, 1028–1060, 1110–1145, 1400–1440.
- `config/config.go` (2776): 115–125, 345–375, 540–560, 1210–1240, 1708–1722.
- `dashboard/dashboard.go` (4784): 485–500, 740–790, 4200–4340.
- `tasks/drain/queue.go` (1336): 379–420, 705–890, 1060–1080.
- `tasks/agent.go` (1597): read the role presets only.
- `tasks/run_tasks.go`: 14–80, 186–245.
- `tasks/manifest.go`: 15–30, 340–420, 505–532, 636–721.
- `tasks/refine.go`: 125–240, 318–500, 539–614.
- `tasks/run_tasks_test.go` (~3.4k): read only the fixture you reuse.
