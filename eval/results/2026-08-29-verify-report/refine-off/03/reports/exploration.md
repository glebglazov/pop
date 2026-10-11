# Exploration report — 2026-08-29-verify-report

- Explored: 2026-10-10T14:11:38Z
- Work SHA: 00e2932d3da6
- Explorer: claude --model "opus"

## Where things live

- `tasks/report.go` — the shared report machinery, parameterised by `reportPass{artifactType, dirName, noun, writtenLabel, agentLabel}`: `refineReport`, `publishedReports`, `reportFileTimeLayout`, `reportDocument`, the methods `dir`, `fileName`, `fileInstant`, `latest`, `write`, `render` and `latestPointer`, `reportHeaderField`, `ensureTrailingNewline`, and `ReportPointer` with `CommitPhrase`, `Summary` and `StaleAgainst`.
- `tasks/refine.go` — Refine pass `refineResolvedSet`. It keeps `refineDirName = "refine"`, the alias `refineDocument = reportDocument`, and one-line bindings to `refineReport` (`latestRefineDocument`, `writeRefineDocument`, `renderRefineDocument`). It also holds `printRefineWritten` and `showLatestRefine` (`--show`).
- `tasks/refine_pointer.go` — the alias `RefinePointer = ReportPointer`, the binding `latestRefinePointer`, `refineBlockView` and `refineBlock`.
- `tasks/refine_commit.go` — `splitRefinerReply` scans the leading non-heading lines for `COMMIT-SUBJECT:` (through `commitSubjectLabelValue`) and gives back `(subject, rest)`.
- `tasks/artifacts.go` — `ArtifactTypeRefine/Spec/Progress`, `RefineDirName` (a second exported copy of `"refine"`), `Artifacts` (scans each of `publishedReports`), `artifactTier` (`artifactTierOrder` slice), `ArtifactSummary` and `LatestArtifactSummary`. `tasks/setkind/artifacts.go` re-aliases these constants and wraps `tasks.Artifacts` for the dashboard.
- `tasks/verify.go` (1632 lines) — the Verifier flow, `ParseVerdict`, `printVerdict` and `acceptResolvedSet`. `tasks/verify_phase.go` is the drain step that calls `drainVerifyPhase`.
- `store/verify.go` — `VerifyVerdict`, `PutVerifyVerdict(Exec)`, `InvalidateVerifyVerdicts`, `CaptureNoteThenInvalidate` and `TakeForwardedNote`.
- `tasks/verify_report.go` — `verifyReport` (`verify` directory, noun `Verify`, labels `Verified` and `Verifier`), `writeVerifyReport` and `verifierReportBody`. `runAndStoreVerdict` calls `writeVerifyReport` after it parses the reply and before it stores the verdict. `ArtifactTypeVerify` is in `tasks/artifacts.go`, and `publishedReports` is `{verifyReport, refineReport}`.
- `tasks/prompts/verifier.tmpl.md` — the response contract. `FINDINGS:` asks a PASS to state what was checked and why each criterion is met; `SUMMARY: ... omit for PASS` is unchanged.
- Surfaces:
  - `tasks/gates.go`: `gateReports`/`latestGateReports` (the latest Verify and Refine pointers), the HITL gate loop, `pageReportDocument` and `promptHITLGateAction`.
  - `tasks/gate_menu.go`: `gateReportsPreamble` and `joinPreamble`.
  - `tasks/render.go`: `RenderTaskSetDetail` at line 712, `renderVerifyReportSection`, `renderArtifactSummarySection`, `firstFindingsLine` and `verifyMarkDetailParts`.
  - `tasks/setkind/setkind.go`: `artifactSections`.
- `tasks/prompt.go` — `refineBlock` is called by `BuildHITLAssistancePrompt`, `BuildFailedAssistancePrompt`, `BuildVerifyFailedAssistancePrompt`, `BuildInterruptAssistancePrompt` and `BuildAssistPrompt`.

## Which seam owns what

- **Filename and "latest":** `reportPass.fileInstant` parses the name. It is the only parser, and three places use it: `reportPass.latest`, `Artifacts`, and the setkind wrapper through `Artifacts`. `reportPass.write` writes the name. If the name is taken, it moves the instant forward one second; it never overwrites a file.
- **Header:** `reportPass.render` writes `# <noun> report — <set>` and then the bullets `- <writtenLabel>:`, `- Commit range:` (only when not empty), `- Work SHA:` (as `ShortSHA`), `- <agentLabel>:` and then one bullet for each `reportFact` it is given — for Refine, `Refined` and `Refiner` and no facts; for Verify, a `- Verdict:` fact. `reportHeaderField` reads the bullets back. It reads only the first bullet list, so there is no side-car file.
- **Pointer:** `reportPass.latestPointer` builds the pointer from the `latest` result plus two header reads. `Summary` leads with the pass's `noun`. `Written` is formatted as `2006-01-02 15:04Z`.
- **Pointer consumers:**
  - `gateReportsPreamble` prints `🔎 ` + the Verify `Summary()`, then `📝 ` + the Refine `Summary()`.
  - In `promptHITLGateAction`, `hitlGateReadVerify` and then `hitlGateReadRefine` ("Read the <noun> report (no agent runs)") each take the next free key after the conditional Re-verify entry.
  - `pageReportDocument` runs `$PAGER` (fallback `less -R`). It uses `AttendedCommandRunner` when one exists and otherwise prints the file.
  - `refineBlock` is the only consumer of `StaleAgainst`. It sets `OutOfDate`.
- **Detail view:** `RenderTaskSetDetail` carries the Verify pointer (`renderVerifyReportSection`) and no Refine pointer; `artifactSections` carries only the `LatestArtifactSummary` text. `TestRefinePointerReachesEverySurface` fails if the detail view contains the refine report path or the SHA.
- **Artifact order:** `artifactTier` is the index in `artifactTierOrder` (verify, refine, spec, progress); an unknown type ranks after all of them. Inside one tier, `Artifacts` sorts newest first, then by name.
- **Verifier calls:** there are three call sites of `runAndStoreVerdict`:
  - `verifyResolvedSet` (`pop tasks verify`, force path).
  - `reverifyAtGate`.
  - `ensureVerifyVerdict`, on a cache miss.

  `ensureVerifyVerdict` returns without an agent when there is a cached row at the SHA or an immunising PASS. Only the third path can be served from cache.
- **Undetermined range:** `runAndStoreVerdict` with `work.Undetermined` stores a NEEDS-HUMAN verdict and invokes no agent.
- **Reply parsing:** `ParseVerdict(raw)` returns `(verdict, findings, summary)`. It looks for the first line that starts with `VERDICT`. The verdict line does not have to be the first line. `extractFindings` and `extractVerifierSummary` do the label work. `parseVerifierCommitSubject(raw)` runs separately. The `VerifyVerdict` row stores only its fields; the reply without its verdict line goes into the Verify report (`verifierReportBody`), and a reply with no readable verdict goes in whole through `unparsedFindings`. `runConfiguredVerifier` returns `(raw, agent, err)`; the `runVerifier` and `verifyRunner` seams still return `(raw, err)`, so a stubbed run writes no `Verifier:` bullet.
- **Accept:** `acceptResolvedSet` builds `VerifyVerdict{HumanAuthored: true, Note}`, reads the row it overrides at the work SHA (best-effort), and upserts inside `mutateWithCheckoutQuiescence`. After the upsert it calls `writeAcceptedVerifyReport` (`tasks/verify_report.go`): a pop-rendered Verify report with the facts `Verdict: PASS`, `Authored by` (`humanAuthor`) and `Overrode`, and no `Verifier:` bullet. Then it calls `printAcceptedVerdict`.

## Invariants that hold

- Reports are never deleted. Being superseded only means that readers take the newest (`reportPass.latest` doc comment, ADR-0214).
- A malformed reply never becomes PASS. Empty, missing or unknown verdicts go to NEEDS-HUMAN, and `unparsedFindings` keeps the raw text under `Raw response:` (`ParseVerdict`).
- `StaleAgainst`: when either SHA is unknown, the report is not stale. A shared prefix counts as the same commit.
- Pointer surfaces never put the body inline (`TestRefinePointerReachesEverySurface` checks that `refineProse` is absent).
- A set with no report renders nothing. `gateReportsPreamble` returns nil, `refineBlockView{}` renders nothing, and `TestSetWithNoArtifactsShowsNothingExtra` checks both.
- Invalidation hard-deletes `verify_verdicts` rows and keeps only the Accept note (`CaptureNoteThenInvalidateExec`). `invalidateVerifyVerdicts` is best-effort.
- Status and mark come only from `ResolveVerifiedStatus` (`tasks/verified_status.go:54`). The badge comes only from `DeriveVerifiedAtBadge` (`tasks/verified_at_badge.go:34`).
- `firstFindingsLine` is used in `render.go` lines 549 and 580, and in `run_tasks.go:293`.

## The test shape

- Surfaces: `tasks/refine_surfaces_test.go` has `seedRefineReport`, `hitlFixture` and `hitlGateOutput`. It covers gate, detail and assist, the paging entry at key 5 (`fakeAttendedRunner`, `PAGER`), header read-back, and a `StaleAgainst` table.
- Verifier: `tasks/verify_test.go` (`setupVerifyFixture`) stubs the agent through the `verifyCoreOptions.runVerifier` seam. The drain uses the `opts.verifyRunner` seam.
- Other verify tests: `verify_phase_test.go`, `drain_verify_test.go`, `verify_invalidate_test.go`, `verify_status_test.go`, `gates_test.go`, `gate_menu_test.go`, `render_detail_test.go`, `refine_test.go`.
- Goldens: `tasks/prompt_golden_test.go` (`goldenPath` → `tasks/testdata/prompts/*.md`, compared with `prompttest.Assert`). `TestVerifierPromptGoldens` produces `verifier.full.md` and `verifier.bare.md`. The refiner and the five assist-family prompts each have golden pairs.
- Gate: `go build ./... && go vet ./tasks/... && go test ./tasks/...`, then `make test`.

## Read these in ranges

- `tasks/verify.go`:
  - 120–170: `VerifyResult` and `verifyCoreOptions`.
  - 225–300: verify and Accept.
  - 445–478: `reverifyAtGate`.
  - 500–680: `runAndStoreVerdict`, `storeVerdict`, `ensureVerifyVerdict` and `drainVerifyPhase`.
  - 940–960: invalidation.
  - 1405–1460: the print helpers.
  - 1495–1632: the parser.
- `tasks/refine.go`: 15–30 (constants), 146–220 (pass and `--show`). The document machinery is all of `tasks/report.go`.
- `tasks/gates.go`: 100–160 (actions and loop), 286–320 (pager), 374–410 (menu).
- `tasks/render.go`: 530–625 (status details and `firstFindingsLine`), 712–800 (detail view).
- `tasks/prompt.go`: the five `Build*Prompt` functions at 166, 250, 299, 379 and 528.
