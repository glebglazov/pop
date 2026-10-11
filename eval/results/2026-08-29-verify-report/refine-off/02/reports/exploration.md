# Exploration report — 2026-08-29-verify-report

- Explored: 2026-10-10T03:46:40Z
- Work SHA: 00e2932d3da6
- Explorer: claude --model "opus"

## Where things live

- `tasks/report.go` holds the shared report machinery (moved out of `refine.go` by task 01). `reportKind{Name, Title, WrittenField, AgentField}` is parameterised by the pass; `refineReport` is Refine's kind. `reportFileTimeLayout = "20060102T150405Z"`. The type `reportDocument{Path, At, Body}`. The methods are `latest`, `FileInstant`, `write`, `render`, `dir` and `fileName`; the functions `reportHeaderField` and `ensureTrailingNewline`. The file name is `<Name>-<instant>.md` under `<set>/<Name>/`. The pass itself is `refineResolvedSet` in `tasks/refine.go`.
- `tasks/report_pointer.go` holds:
  - the type `ReportPointer{Title, Path, WorkSHA, CommitRange, Written}`
  - `reportKind.latestPointer`
  - the methods `CommitPhrase`, `Summary` (prefix is the kind's `Title`) and `StaleAgainst`
- `tasks/refine_pointer.go` holds `refineBlockView` and `refineBlock`.
- `tasks/refine_commit.go:20`: `splitRefinerReply` peels `COMMIT-SUBJECT:` off the front of the reply. Its scan stops at the first `#` heading.
- `tasks/artifacts.go` holds the constants `ArtifactTypeRefine` and `RefineDirName = ArtifactTypeRefine`. The file also holds `Artifacts`, the `artifactTierOrder` slice that `artifactTier` indexes (verify, refine, spec, progress; unknown sorts last), `ArtifactSummary`, `LatestArtifactSummary` and `ArtifactSectionTitle`.
- `tasks/setkind/artifacts.go` aliases `tasks.ArtifactTypeRefine` and `tasks.RefineDirName`, and wraps `tasks.Artifacts` for the dashboard.
- `tasks/gates.go` holds the HITL gate loop. Line 143 re-resolves `refineReport.latestPointer` on each pass of the loop. Line 149 dispatches `hitlGateReadRefine`. `pageReportDocument` (was `pageRefineDocument`) pages either kind. `promptHITLGateAction` is at 374: it adds the conditional menu entries through `add(...)`, and the refine entry is at 394. The preamble is built through `joinPreamble` at 405. The verify-failed gate's Accept is at about 625–645 and calls `acceptResolvedSet`.
- `tasks/gate_menu.go:111` holds `gateReportPreamble` (was `gateRefinePreamble`), which returns `"📝 " + p.Summary()` for either kind.
- `tasks/prompt.go` calls `refineBlock` in five prompt builders: `BuildHITLAssistancePrompt` (186), `BuildFailedAssistancePrompt` (267), `BuildVerifyFailedAssistancePrompt` (322), `BuildInterruptAssistancePrompt` (396) and `BuildAssistPrompt` (550). All five render the `latest-refine-report` partial in `tasks/prompts/partials.tmpl.md`.
- `tasks/render.go` holds:
  - `rowStatusDetail` (513), which calls `firstFindingsLine` at 549 (VERIFY-FAILED) and 580 (`verifyMarkDetailParts`)
  - `firstFindingsLine` (616)
  - `renderArtifactSummarySection` (784), which is the CLI detail view's only artifact surface
- `tasks/setkind/setkind.go:441`: `artifactSections` is the dashboard detail view's summary section.
- `tasks/verify.go` holds:
  - `verifyResolvedSet` (225), `acceptResolvedSet` (267) and `remediateResolvedSet` (307)
  - `runAndStoreVerdict` (506), `storeVerdict` (565) and `ensureVerifyVerdict` (585)
  - `drainVerifyPhase` (648), `invalidateVerifyVerdicts` (948) and `buildVerifierPrompt` (1193)
  - `printVerdict` (1411) and `printAcceptedVerdict` (1434)
  - `ParseVerdict` (1499), `extractFindings` (1550), `extractVerifierSummary` (1582), `unparsedFindings` (1620) and `parseVerifierCommitSubject` (1344)
- `tasks/prompts/verifier.tmpl.md:41–53` is the response contract. Line 47 is `SUMMARY: … omit for PASS`. Line 49 is `FINDINGS: … leave empty for PASS`.
- `store/verify.go` holds `VerifyVerdict`, whose doc comment says "Findings … (empty for PASS)" and describes `HumanAuthored` and `Note`. It also holds `PutVerifyVerdict` and `PutVerifyVerdictExec`, and `CaptureNoteThenInvalidate`, which is the hard delete.
- `tasks/verified_status.go` (`resolveSetVerifyMark`, `ResolveVerifiedStatus`) and `tasks/verify_mark.go` own the Verification mark. `DeriveVerifiedAtBadge` is used from `render.go`.

## Which seam owns what

- **Filename contract:** `reportKind.FileInstant` parses names. `reportKind.write` composes them through `fileName`. Both use the kind's `Name` and `reportFileTimeLayout` in `report.go`.
- **Latest-by-timestamp:** `reportKind.latest` returns the newest `At`, ignores directories and non-matching names, and returns the body trimmed.
- **Header facts:** `reportKind.render` writes `# <Title> report — <set>`, then `- <WrittenField>:` (RFC3339; `Refined` for refine), and then, each only when non-empty, `- Commit range:`, `- Work SHA:` (`ShortSHA`) and `- <AgentField>:` (`Refiner`). `reportHeaderField` reads facts back only from the first bullet list in the document.
- **Pointer → text:** `ReportPointer.Summary` (gate preamble), `CommitPhrase` (shared by all surfaces) and `StaleAgainst` (used only by `refineBlock` for `OutOfDate`).
- **Artifact listing:** `Artifacts` scans the set root for spec and progress, then `<set>/verify/` and `<set>/refine/` through each kind's `FileInstant`, then sorts by `artifactTier`. Inside a tier the order is newest first, then by name.
- **Verdict parsing vs. storing:** `ParseVerdict` decides verdict, findings and summary. `runAndStoreVerdict` is the only place that invokes an agent: the `opts.runVerifier` seam, or `runConfiguredVerifier`. It returns early, with no agent, for `work.Undetermined`. `ensureVerifyVerdict` returns a cached row or the latest PASS without calling `runAndStoreVerdict`.
- **Callers of `runAndStoreVerdict`:** `verifyResolvedSet` (251), `reverifyAtGate` (469) and `ensureVerifyVerdict` (616). `printVerdict` runs in `verifyResolvedSet` and `drainVerifyPhase`, and in the drain it runs for cached rows too.
- **Accept:** `acceptResolvedSet` builds the `HumanAuthored: true` row and upserts it inside `mutateWithCheckoutQuiescence`. That upsert overwrites any row at the same work SHA. It reads the prior verdict first through `latestVerdict`, for the Verify report it files in the same transaction. `latestVerdict` (357) is the existing reader for a row at a given SHA.

## Invariants that hold

- The report is never inlined. Every surface carries a path and a commit, never the body. Pinned by `refine_surfaces_test.go` (`refineProse`) and `setkind/refine_section_test.go`.
- The CLI and dashboard detail views show only `ArtifactSummary` (count, newest type, written time) and no refine path. `TestRefinePointerReachesEverySurface` asserts that the detail view does *not* contain the path or the SHA.
- A set with no report renders nothing. `refineReport.latestPointer` returns false, `gateReportPreamble` returns nil, the menu entry is omitted, and `refineBlockView{}` renders empty.
- The gate menu keys are contiguous through `add` in `promptHITLGateAction`. Keys resolve through the `keys` map, not by position.
- A malformed or empty reply resolves to `VerdictNeedsHuman`. `unparsedFindings` embeds the raw text after `"Raw response:\n"`.
- The `verify_verdicts` rows are deleted by `CaptureNoteThenInvalidate` on remediation spawn, reopen and scope growth. The Accept `Note` survives the delete through the forwarded-note side table (`TakeForwardedNote`).
- `buildVerifierPrompt` takes `priorNote` and nothing from Refine. The verifier goldens have no refine block.
- Writes never overwrite. `reportKind.write` moves forward one second at a time while a name is taken.
- Reports sit in the Work store, not in the repository (comment on `refineReport`).

## The test shape

- `tasks/refine_test.go` drives `refineResolvedSet` through the `runRefiner` seam.
- `tasks/refine_surfaces_test.go` drives the gate, detail and assist surfaces. It uses `seedRefineReport` (`refineReport.render` + `refineReport.write`), `hitlFixture` and `hitlGateOutput`.
- `tasks/verify_test.go` drives `verifyResolvedSet` with `runVerifier` stubs and `setupVerifyFixture`, and reads rows back with `readStoredVerdict`. Of interest:
  - `TestParseVerdict` (20) and `TestParseVerdictMalformedIncludesRawForHuman` (72)
  - `TestVerifyResolvedSetCacheHitWritesNoRun` (614)
  - the Accept tests at 660 and 697
- Other verify tests: `verify_phase_test.go`, `drain_verify_test.go`, `verify_invalidate_test.go`, `verify_fail_gate_test.go` and `verify_status_test.go`.
- Artifact order is pinned in three places: `tasks/setkind/artifacts_test.go` (expects `refine:` rows first), `dashboard/dashboard_artifact_test.go:113` and `cmd/tasks_artifacts_test.go`.
- `tasks/prompt_golden_test.go` covers the prompt goldens (`TestVerifierPromptGoldens` at 290, the five assistance and assist goldens at 199–288). The files are `tasks/testdata/prompts/*.md`. Regenerate them with `-update-prompt-goldens` (`internal/prompt/prompttest/golden.go:21`).

## Read these in ranges

- `tasks/verify.go` (1632 lines): 225–356, 500–668, 940–966, 1193–1260, 1405–1460, 1499–1632.
- `tasks/refine.go`: `refineResolvedSet` and `showLatestRefine`. `tasks/report.go` and `tasks/report_pointer.go` are short; read them whole.
- `tasks/render.go` (840 lines): 513–622, 780–795.
- `tasks/gates.go` (745 lines): 100–160, 286–320, 374–415, 615–650.
- `tasks/verify_test.go` (1665 lines): 20–176, 614–735.
- `tasks/prompt.go` (636 lines): the five `refineBlock` call sites listed above.
