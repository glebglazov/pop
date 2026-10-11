# Exploration report — 2026-09-07-exploration-report

- Explored: 2026-10-04T09:04:44Z
- Work SHA: 647143bb0a85
- Explorer: claude --model "opus"

## Where things live

- `tasks/artifacts.go`: the artifact type constants (`ArtifactTypeVerify/Refine/Spec/Progress`), `Artifacts()`, the tier order `artifactTierOrder` (verify, refine, spec, exploration, progress) and `reportArtifactKinds = []passReport{verifyReport, refineReport}`. Files at the set root are matched by name in a `switch` on `SpecFileName` / `ExplorationFileName` / `ProgressFileName`.
- `tasks/manifest.go`: `parseManifestJSON` (~346). Any set-level key it does not name goes into `m.Unknown`. Also `validateNoOrphanMarkdown` (~509), which exempts only `SpecFileName`, `legacySpecFileName` and `ExplorationFileName`. Then `optedOut`, `VerifyOptedOut`, `RefineOptedOut`, `ExploreRequested` (opt-in), `AgentDirective`, `VerifierOverride`, `RefinerOverride` and `ExplorerOverride`.
- `tasks/authoring_guide.go`: `AuthoringGuide()`. Its set-level key prose is a raw string. The `verify` / `refine` and `verifier` / `refiner` bullets are hand-written; the `explore` / `explorer` bullets print `ManifestKeyExplore` / `ManifestKeyExplorer` and `ExplorationFileName` (`TestTaskGuideExploreKeysAreTheOnesTheManifestReads`). `tasks/authoring_guide_test.go` asserts the enums and templates (`TestTaskGuideEnumsEqualTheValidatorsSets`).
- `tasks/refine.go`: the model pass. It holds `refineReport`, `RefineTaskSetWith` → `refineResolvedSet`, `runRefiner`, `resolveRefiner`, `runConfiguredRefiner`, `refinerRole`, `refineEnabled` and `buildRefinerPrompt`.
- `tasks/agent_fallback_walk.go`: `agentRole` (`Noun`, `Gerund`, `Phase`, the `Persist*` hooks, `RetryEligible`, `ReadOnly`) and `runAgentFallbackWalk`.
- `tasks/pass_report.go`: `passReport`, `renderDocument`/`renderDocumentBy`, `writeDocument` (timestamped, never overwrites), `latestDocument`, `reportHeaderField`, `ReportPointer` with `CommitPhrase`, `Summary` and `StaleAgainst`.
- `tasks/spent_retry_cap.go`: `spendPhaseImplement/Verify/Refine/Explore` and `recordSpentRetryCap`.
- `tasks/stream.go` `phaseOrder` (~132) ranks the `spendPhase*` constants; `persistSetLevelRun` files every Verifier, Refiner and Explorer run. Phase words are still literals in `tasks/spend.go` `spendBreakdownRowKey` (~817) and `tasks/stream_tracer.go` `setLevelRunStream` (~209).
- `tasks/prompt.go` and `tasks/prompts/*.tmpl.md`:
  - `BuildAgentPrompt(d, taskPath, runtimePath, implementationConvention)` (line 139) takes `Deps` but no `Manifest`; it names the report through `explorationBlock` (`tasks/exploration_pointer.go`). Its callers are `tasks/run_selected_task.go:94` and `tasks/executor.go:275`.
  - `BuildAssistPrompt` (line 535) sets `view.Refine = refineBlock(...)`, `view.Exploration = explorationBlock(...)` and `view.RefineMark`.
  - The shared `latest-refine-report` partial is in `partials.tmpl.md:15`.
- `tasks/refine_pointer.go`: `RefinePointer = ReportPointer`, `latestRefinePointer`, `refineBlockView`, `refineBlock`.
- `tasks/refine_mark.go`: `RefineMark`, `UnrefinedReason` and `ResolveRefineMark`. It derives the mark from the latest `refine`-phase captured run alone. Callers: `tasks/render.go:784`, `tasks/gate_menu.go:126`, `tasks/prompt.go:555`.
- `tasks/run_tasks.go`: `implementRun.loop` (189). The test seams `verifyRunner` and `refineRunner` are on the options struct (~65-88).
- `tasks/refine_phase.go`: `refinePhase` and `refineDirective`.
- `config/config.go`: `RefineConfig` (354), `WorkConfig.Refine` (572), `RefineSettings` (1250). The `*bool` default-on pattern is `KillPanePromptEnabled` (122), read through `DashboardKillPanePromptEnabled` (1759).
- `cmd/tasks.go`: `taskRefineCmd` (`Use` at line 170, flags at 436-442), `runTaskRefineWith` (~1030), and the seams `implementationConvention(d)` (1120) and `refineOverlay(d)` (1134). A whole-set drain runs through `tasks/implement.RunWholeSetWith` (~1400). `taskImplementCmd` flags are at 408-423; `--skip-explore` is the one that skips a phase.
- `tasks/status.go`: the status constants (20-42, `StatusExploreFailed` last) and `DeriveStatus` / `TerminalStatus` / `DeriveStatusWithVerdict` (~135-215). `tasks/verify_status.go`: `ApplyVerifyVerdictsWith` / `decorateRowWithVerdict` (~175-250), which write `row.VerifyMark`.
- `tasks/drain/queue.go`: `Decision` (379), `decideProjectDispatches` (728), `selectReadySets` (1065), which picks only `StatusReady && row.AutoDrain`, and `firstAwaitingApprovalSetID` (991).
- `tasks/progress.go`: `AppendSetProgress` writes the task field `"set"`.
- `tasks/setkind/artifacts.go`: `Kind.Artifacts` over `tasks.Artifacts`.
- `tasks/setkind/setkind.go`: `detailSections` (445) is the detail view's sections (the Exploration pointer, then `artifactSections` at 465, the "Artifacts" section), and line 411 copies `VerifyMark` onto the container. `dashboard/render.go` contains no artifact code.

## Which seam owns what

- **Artifact listing:** `tasks.Artifacts` owns it. The CLI (`cmd/tasks.go` `runTaskArtifactsWith`) and the dashboard (`setkind.Kind.Artifacts`) both read it.
- **Manifest keys:** a key's meaning is an accessor over `m.Unknown`, for example `optedOut(key)` and `agentDirective(key)`. `parseManifestJSON` names only the keys it parses into fields.
- **Agent selection precedence:** `resolveRefiner` applies CLI → `RefinerOverride()` → `cfg.RefineSettings()` / `RefineAgentList()` → `DefaultRefineEffort`.
- **Running a role:** `runAgentFallbackWalk` walks the agent list. The role decides `Phase`, persistence and retry eligibility.
- **Prompt seams:** `cmd` owns conventions and overlays and hands them in as funcs (`ImplementationConvention`, `DocumentOverlay`), because `conventions` sits above `tasks`.
- **Drain loop:** `implementRun.loop` re-runs `RefreshWith` and `SelectTaskInSet` every iteration. Refine is the first directive of the `selErr != nil` (quiescence) branch, followed by `verifyPhase` and then `terminalStatus`.
- **Read-side resolution:** status plus verification mark comes from `ResolveVerifiedStatus` via `decorateRowWithVerdict`. The Refine mark comes from `ResolveRefineMark`, which is not on `Row` or `work.Container`. `work.Container` carries `VerifyMark` (`work/kind.go:281`) and the Explore mark `ExploreMark` / `UnexploredReason` (286), which `ResolveExploreMark` resolves onto `Row` in `buildRows`.

## Invariants that hold

- `writeDocument` never overwrites. On a name clash it moves the instant forward by one second (`pass_report.go:129`).
- `renderDocument` stamps the header facts. `reportHeaderField` reads back only the first bullet list (`pass_report.go:221`).
- A Refiner answer that is retry-eligible or blank writes no report (`runConfiguredRefiner`, ~413), and `runRefiner` refuses an empty body.
- `refinePhase` declines when `m.HumanCompleted` or `m.RefineOptedOut()` is set, or when the set is not at DONE or AWAITING-APPROVAL. Every failure except an interrupt falls through (`refine_phase.go:98-108`).
- A hand run (`RefineTaskSetWith`) ignores `refineEnabled` and the episode (`TestHandRefineIgnoresTheEpisode`).
- `optedOut` treats a malformed or absent value as participating. `agentDirective` turns a malformed object into nil.
- The orphan check makes every `.md` at the set root except `spec.md` / `prd.md` / `exploration.md` a MALFORMED error unless the manifest lists it (`manifest.go:521`).
- `StatusBlocked` means Human-blocked (`DeriveStatus`, `hasOpenAFKTask`). `selectReadySets` passes over any status other than READY.
- `ResolveRefineMark` and `ResolveVerifiedStatus` are read-only and return no mark for a non-terminal set.
- Unknown keys stay in `Unknown` and survive `WriteManifestAtomic`.

## The test shape

- `tasks/drain_refine_test.go` drives whole drains through `setupRunTaskSetFixture` with the `refineRunner` seam and `refineEnabledConfig()`. It asserts phase order, the episode, opt-out, and the captured run phase (`TestRefineRunIsCapturedUnderItsOwnPhase`).
- `tasks/refine_test.go`, `refine_retry_test.go`, `refine_surfaces_test.go`, `refine_mark_test.go` and `gate_menu_test.go` cover the pass and its surfaces.
- `tasks/prompt_golden_test.go` renders every template against `tasks/testdata/prompts/*.md`: `agent.md`, `agent.implementation-convention.md`, and `assist.{bare,full,empty-progress}.md`.
- Other suites: `tasks/manifest_test.go`, `tasks/authoring_guide_test.go`, `cmd/tasks_artifacts_test.go`, `cmd/tasks_artifacts_render_test.go`, `tasks/setkind/artifacts_test.go`, `tasks/verified_status_test.go`, `tasks/verify_status_test.go`.
- The proof for each task is the verify line in its own Orientation.

## Read these in ranges

- `tasks/run_tasks_test.go` (3553 lines): find the fixture with `rg -n "func setupRunTaskSetFixture"` and read only that range.
- `cmd/tasks.go` (2143 lines): lines 160-200, 400-445, 1030-1140 and 1375-1430.
- `tasks/drain/queue.go` (1336 lines): lines 379-400, 728-890 and 1065-1100.
- `config/config.go` (2821 lines): lines 340-375, 562-577 and 1232-1257.
- `dashboard/dashboard.go` (4784 lines): read status-cell ranges only, located with `rg -n VerifyMark`.
- `tasks/agent.go` (1597 lines): read the role presets by range.
- `tasks/manifest.go` (721 lines): lines 340-420 and 505-535.
- `CONTEXT.md`: lines 553-563, the drain-stop list.
