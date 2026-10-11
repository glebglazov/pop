# Exploration report — 2026-08-29-verify-report

- Explored: 2026-10-10T13:21:17Z
- Work SHA: 00e2932d3da6
- Explorer: claude --model "opus"

## Where things live

- `tasks/refine.go` — Refine. It holds `refinePass` (the `reportPass` for Refine: name `Refine`, dir `refine`, header labels `Refined` / `Refiner`), `printRefineWritten` and the core `refineResolvedSet`.
- `tasks/report.go` — the shared report machinery, parameterised by `reportPass`: `reportDocument{Path, At, Body}`, `reportFileTimeLayout = "20060102T150405Z"`, `reportPass.fileInstant`, `latestReport`, `writeReport`, `renderReport`, `ensureTrailingNewline`, `reportHeaderField`, and `ReportPointer{Path, WorkSHA, CommitRange, Written}` with `latestReportPointer`, `CommitPhrase`, `Summary` (prefix is the pass name) and `StaleAgainst`.
- `tasks/refine_pointer.go` — `refineBlockView` / `refineBlock` only.
- `tasks/refine_commit.go` — `splitRefinerReply`. It lifts the `COMMIT-SUBJECT:` line off the reply, stops at the first `#` heading, and returns the trimmed raw text when it finds no subject.
- `tasks/artifacts.go` — `ArtifactTypeVerify/Refine/Spec/Progress` and `RefineDirName = "refine"` (the only copy; `refinePass.Dir` uses it). The file also holds `Artifacts`, `appendReportArtifacts` (one pass's reports as artifacts), `artifactTierOrder` (a list: verify, refine, spec, progress) with `artifactTier` (index in it; unknown sorts last), `ArtifactSummary`, `LatestArtifactSummary` and `ArtifactSummary.Body`.
- `tasks/setkind/artifacts.go` — aliases `tasks.ArtifactType*` and `tasks.RefineDirName`. `Kind.Artifacts` wraps `tasks.Artifacts` for the dashboard. `tasks/setkind/setkind.go:441` `artifactSections` renders `LatestArtifactSummary`.
- `tasks/verify.go` — `verifyResolvedSet` (225), `acceptResolvedSet` (267), `remediateResolvedSet` (307), `reverifyAtGate` (451), `runAndStoreVerdict` (506), `storeVerdict` (565), `ensureVerifyVerdict` (585), `drainVerifyPhase` (648), `invalidateVerifyVerdicts` (948), `printVerdict` (1411), `printAcceptedVerdict` (1434), `ParseVerdict` (1499), `extractFindings` (1550), `extractVerifierSummary` (1582) and `unparsedFindings` (1620).
- `tasks/verify_phase.go` — `implementRun.verifyPhase`, the drain step. It calls `drainVerifyPhase`.
- `store/verify.go` — the `VerifyVerdict` row. `CaptureNoteThenInvalidate` runs the hard `DELETE FROM verify_verdicts` (line 148).
- `tasks/prompts/verifier.tmpl.md` — the reply contract: `VERDICT:` first, then `SUMMARY:`, an optional `COMMIT-SUBJECT:`, and `FINDINGS:` (failures for FIXABLE / NEEDS-HUMAN; for PASS, what was checked and why each criterion is met). Everything after the `VERDICT:` line is filed as the Verify report (`tasks/verify_report.go`).
- `tasks/gates.go` / `tasks/gate_menu.go` — the HITL gate loop (130–160) and `promptHITLGateAction` (374). There, `hitlGateReadRefine` is added at line 394 and `gateRefinePreamble` goes into `Preamble` at line 405. `pageReportDocument` (any pass's report; was `pageRefineDocument`) is at ~305 and `pagerCommand` beside it. The verify-failed gate is `promptVerifyFailedGateAction` (~700). Its preamble is `gateFindingsPreamble` plus `gateRemediationPreamble`, with no refine line.
- `tasks/render.go` — `RenderTaskSetDetail` (712). It ends in `renderArtifactSummarySection` (784), which prints the summary and `pop tasks artifacts <id>`, not a pointer. `firstFindingsLine` (616) is used at 549 (VERIFY-FAILED row) and 580 (`verifyMarkDetailParts`). There is a third use at `tasks/run_tasks.go:293`.
- `tasks/prompt.go:186,267,322,396,550` — the five agent-facing views that set `Refine: refineBlock(...)`. They render through `tasks/prompts/partials.tmpl.md`, `{{define "latest-refine-report"}}`.

## Which seam owns what

- **Filename contract:** `reportPass.fileInstant` parses names. `writeReport` makes them and moves the instant forward one second when a name is already taken. `appendReportArtifacts` and `latestReport` both call `fileInstant` to filter directory entries.
- **Header of facts:** `renderReport(refinePass, …)` writes `# Refine report — <set>`, then `- Refined:` (RFC3339), `- Commit range:`, `- Work SHA:` (`ShortSHA`, 12 chars) and `- Refiner:`, with each line left out when its value is empty. `reportHeaderField` reads a value back from the first bullet list only and stops at the first non-bullet line after that list.
- **Pointer to surfaces:** `latestReportPointer(d, m, refinePass)` → `ReportPointer`. Human surfaces use `gateRefinePreamble` (`"📝 " + Summary()`) and `pageReportDocument`. Agent surfaces use `refineBlock`, which adds `OutOfDate` through `StaleAgainst(verifyWorkSHA(...))`.
- **Reply parsing:** `ParseVerdict` returns `(Verdict, findings, summary)`. `parseVerifierCommitSubject(raw)` is a separate pass over the same raw text in `runAndStoreVerdict`.
- **Real Verifier invocations:** only `runAndStoreVerdict` calls `opts.runVerifier` or `runConfiguredVerifier`. It is reached from `verifyResolvedSet`, `reverifyAtGate` and `ensureVerifyVerdict` (on a cache miss). It does not call the agent on the `work.Undetermined` branch, which stores a NEEDS-HUMAN through `storeVerdict`.
- **Cache reads:** `ensureVerifyVerdict` returns `GetVerifyVerdict` at the SHA, or an immunizing `GetLatestPassVerifyVerdict`, and does not invoke the agent in either case.
- **Human PASS:** `acceptResolvedSet` is the only non-test builder of `HumanAuthored: true`. It writes through `store.PutVerifyVerdictExec` inside `mutateWithCheckoutQuiescence`, then calls `printAcceptedVerdict`.
- **Status and badge:** `tasks/verified_status.go` and `tasks/verify_mark.go`. `render.go` `verifiedAtBadgeSuffix` → `DeriveVerifiedAtBadge`.

## Invariants that hold

- No surface prints a report body, only a pointer. `refineProse` in `tasks/refine_surfaces_test.go` pins this.
- A set with no report renders nothing. `latestReportPointer` returns false, and `TestSetWithNoArtifactsShowsNothingExtra` pins it.
- Staleness is false when either SHA is unknown, and a common prefix counts as the same commit (`StaleAgainst`).
- A malformed, empty or unrecognised reply yields NEEDS-HUMAN, and the raw text is kept in the findings (`unparsedFindings`, `TestParseVerdictMalformedIncludesRawForHuman`).
- Old reports are never deleted. "Latest" is a read-time choice (`latestReport`).
- The report directory sits under `m.Dir`, which is in the Work store and not in the repository (comment at `reportPass.Dir`).
- Invalidation deletes every verdict row but keeps the Accept note first (`CaptureNoteThenInvalidate`). `invalidateVerifyVerdicts` is best-effort.
- `artifactTier`: an unknown type sorts after all known types. Inside a tier, rows sort newest first, then by name.
- The detail view shows the artifact summary, not a refine pointer (ADR-0217). `TestRefinePointerReachesEverySurface` fails if the detail view contains the path or the SHA.

## The test shape

- `tasks/refine_surfaces_test.go`: `seedRefineReport` and `hitlFixture` (over `setupDrainVerifyFixture`) drive the gate, `RenderTaskSetDetail`, `BuildAssistPrompt`, `pageReportDocument`, the header read-back, `refineBlock` and the staleness table.
- `tasks/verify_test.go`: `ParseVerdict` tables. It also drives `verifyResolvedSet` through `opts.runVerifier` stubs, covering cache hit with no run (`TestVerifyResolvedSetCacheHitWritesNoRun`), Accept and Remediate.
- Other verify tests: `tasks/verify_invalidate_test.go`, `tasks/drain_verify_test.go` and `tasks/verify_phase_test.go`.
- Artifacts: `tasks/setkind/artifacts_test.go` and `tasks/setkind/refine_section_test.go`.
- Prompt goldens: `tasks/prompt_golden_test.go`. `TestVerifierPromptGoldens` covers `verifier.bare.md` and `verifier.full.md`. The agent-facing views have goldens too: `hitl-assistance`, `verify-failed-assistance`, `assist`, `failed-assistance` and `interrupt-assistance`. All are under `tasks/testdata/prompts/`. Regenerate them with `-update-prompt-goldens` (`internal/prompt/prompttest/golden.go:21`).
- Gate: `go build ./... && go vet ./tasks/... && go test ./tasks/...`, then `make test`.

## Read these in ranges

- `tasks/verify.go` (1632 lines): 225–355, 500–680, 940–960 and 1410–1632.
- `tasks/refine.go`: the `refinePass` var near the top and `refineResolvedSet`; `tasks/report.go` whole.
- `tasks/gates.go`: 95–160, 286–315, 374–415, 540–560 and 596–720.
- `tasks/render.go` (840): 530–625 and 712–800.
- `store/verify.go` (201): 1–80 and 130–160.
