# Exploration report — 2026-08-29-verify-report

- Explored: 2026-10-10T02:58:40Z
- Work SHA: 00e2932d3da6
- Explorer: claude --model "opus"

## Verify report: the code as found at 00e2932d

### Where things live
- `tasks/refine.go`: the Refine pass, including `printRefineWritten`. (Corrected after task 01: the document machinery moved to `tasks/report.go` — `passReport` with `refineReport`, its `render`/`write`/`latest`/`fileInstant`/`latestPointer` methods, `reportDocument`, `reportHeaderField`, `ensureTrailingNewline` and `ReportPointer`; `refineDirName`, `RefineFileInstant` and the private Refine copies are gone.)
- `tasks/refine_pointer.go`: `refineBlockView` and `refineBlock` only. (Corrected after task 01: the pointer is the shared `ReportPointer` in `tasks/report.go`, whose `Summary()` takes its label from the pass.)
- `tasks/artifacts.go`: the `ArtifactType*` constants, `RefineDirName` (now defined as `ArtifactTypeRefine`), `Artifacts`, `artifactTier`, `LatestArtifactSummary` and `ArtifactSummary.Body`.
- `tasks/setkind/artifacts.go`: re-aliases `tasks.RefineDirName` and the type constants, and maps `tasks.Artifacts` onto `work.Artifact`. `tasks/setkind/setkind.go:443` `artifactSections` is the dashboard detail section.
- `tasks/refine_commit.go:20`: `splitRefinerReply`. It lifts `COMMIT-SUBJECT:` and stops at the first `#` heading.
- `tasks/verify.go` (1632 lines): the verify core, the reply parser and the stdout printers.
- `tasks/verify_phase.go:59`: `implementRun.verifyPhase`, the drain's verify step. It calls `drainVerifyPhase`.
- `store/verify.go`: the `VerifyVerdict` row, Get/Put, `InvalidateVerifyVerdicts*` (a hard `DELETE`), `CaptureNoteThenInvalidate*` and `TakeForwardedNote`.
- `tasks/prompts/verifier.tmpl.md`: the Verifier envelope. The response contract is at the end of the file. `FINDINGS: <… — leave empty for PASS>` is near the last lines.
- `tasks/gates.go`, `tasks/gate_menu.go`: the HITL gate menu, its preamble and the paging entry.
- `tasks/render.go`: the CLI rows and the detail view.
- `tasks/prompt.go`, `tasks/prompts/partials.tmpl.md:15` (`latest-refine-report`): the agent-facing Refine block.

### Which seam owns what
- **Filing and naming a report:** `passReport.write` (formerly `writeRefineDocument`) creates `<setDir>/<type>/` and truncates the instant to the second. If a name is already taken, it adds one second instead of overwriting. The name is `refine-20060102T150405Z.md`. `passReport.fileInstant` is the only parser of that name. Both `passReport.latest` and `Artifacts` (which loops `publishedReports`) call it.
- **Header:** `passReport.render` (formerly `renderRefineDocument`) writes `# Refine report — <set>`, then bullets `Refined:` (RFC3339), `Commit range:` (if any), `Work SHA:` (`ShortSHA`, if any) and `Refiner:` (if any), then a blank line and the body. (Corrected after task 02: `render` also takes trailing `reportFact`s, written after the author bullet; the Verify report stamps `Verdict:` this way.) `reportHeaderField` reads a field back from the first bullet list only. A non-bullet line after that list ends the scan.
- **Pointer:** `passReport.latestPointer` builds `ReportPointer` from `passReport.latest` plus `reportHeaderField`. `Written` uses the format `2006-01-02 15:04Z`. `Summary()` opens with the pointer's `Label`, which the pass sets (`"Refine"`).
- **Agent-facing block:** `refineBlock` (it adds `OutOfDate` through `StaleAgainst` and `verifyWorkSHA`) has five callers in `prompt.go` (:186, :267, :322, :396, :550). These are the hitl, failed, verify-failed, interrupt and assist prompts. They all render through the `latest-refine-report` partial.
- **Gate:** `gates.go:143` resolves the pointer again on each pass of the menu loop. `promptHITLGateAction` (:374) adds the `hitlGateReadRefine` entry on the next free key (:393). It puts `gateRefinePreamble` (`gate_menu.go:111`, `"📝 " + p.Summary()`) last in `joinPreamble`. `pageRefineDocument` (:291) runs `$PAGER` attended and prints the file if the pager fails. It spawns no agent. (Corrected after task 04: the gate now resolves `gateReports` (`resolveGateReports`, `gate_menu.go`) for both passes; `gateReportPreamble` and `pageReportDocument` replace the Refine-only names, and a `hitlGateReadVerify` entry comes before the Refine one.)
- **Detail view:** at present, no Refine pointer shows in the detail view. `render.go:783` `renderArtifactSummarySection` and `setkind.artifactSections` show only `LatestArtifactSummary` (count, newest type and time, `pop tasks artifacts <set>`).
- **Artifact order:** `artifactTier` is the index in the `artifactTierOrder` list (verify, refine, spec, progress; unknown after them) — corrected after tasks 01 and 04. `Artifacts` sorts by tier, then newest first, then name.
- **Verifier invocation:** `runAndStoreVerdict` (verify.go:506) is the only place a Verifier agent is called (`opts.runVerifier`, or `runConfiguredVerifier`). If the range is undetermined, it stores a NEEDS-HUMAN verdict and calls no agent (:508-521). `ensureVerifyVerdict` (:585) returns a cached verdict at the SHA, or an immunizing PASS, before it reaches `runAndStoreVerdict`. `verifyResolvedSet` (:225) always calls `runAndStoreVerdict`, so a forced run always invokes the agent.
- **Reply parsing:** `ParseVerdict` (:1499) finds the first `VERDICT` line anywhere in the reply, not only the first line. It then calls `extractFindings` (:1550), which takes `FINDINGS:` and everything after it, or every line after the verdict line except the SUMMARY and COMMIT-SUBJECT lines, and `extractVerifierSummary` (:1582). An empty reply, no verdict line, or an unknown verdict token gives NEEDS-HUMAN plus `unparsedFindings(raw)`, which embeds the raw text. `parseVerifierCommitSubject` is applied to `raw` on a separate path.
- **Accept:** `acceptResolvedSet` (:267) builds a `HumanAuthored: true` PASS with `Note` and `ComputedAt: time.Now()`. It upserts this row inside `mutateWithCheckoutQuiescence`, which overwrites the row at the same SHA. (Corrected after task 03: it now reads the verdict it overrides through `latestVerdict` and files a human-authored Verify report inside the same transaction.) `latestVerdict` (:357) is the existing reader that the Remediate path uses.
- **Status and badge:** `ResolveVerifiedStatus` (`verified_status.go:54`) and `resolveSetVerifyMark` (:8) derive the status and the mark. `DeriveVerifiedAtBadge` and `VerifiedAtBadgeText` (`verified_at_badge.go:34`, `:72`) own staleness on the verify side.

### Invariants that hold
- Reports are stored in Task storage, never in the repository (`refineDirName` doc, ADR-0214). Older reports are never deleted; "latest" means the newest name (`latestRefineDocument`).
- No surface prints a report body. `refine_surfaces_test.go` asserts that `refineProse` is absent, and that the detail view contains neither the path nor the SHA.
- A set with no report renders no block, no preamble line and no menu entry (`TestSetWithNoArtifactsShowsNothingExtra`).
- The verdict cache is keyed by (repo, set, work_sha). It is deleted on reopen, on remediation spawn and on scope growth, through `CaptureNoteThenInvalidate`. A forwarded note goes into exactly one later Verifier prompt (`TakeForwardedNote`, `buildVerifierPrompt`'s `PriorNote`).
- `verifierPromptView` (verify.go:1234) has no report field. Today the Verifier prompt holds no prior Verifier prose.
- `firstFindingsLine` truncates in three places: `render.go:549`, `render.go:580` and `run_tasks.go:293`. `printVerdict` (:1411) prints the full findings to stdout.
- The Refine timestamps come from `d.Now()`. The verify rows use `time.Now()`.

### The test shape
- `tasks/refine_surfaces_test.go`: `seedRefineReport`, `hitlFixture` and `hitlGateOutput`. These drive the gate, `RenderTaskSetDetail` and `BuildAssistPrompt`. Header readback and staleness are covered at :152 and :247.
- `tasks/refine_test.go`: the pass end to end, through the `runRefiner` seam. `refine_commit_test.go:266` tests `splitRefinerReply` in a table.
- `tasks/verify_test.go`: `TestParseVerdict` (:20), malformed replies (:72, :229), forced runs (:176, :200), cache hit (:614), Accept (:660, :697). `verify_invalidate_test.go` and `drain_verify_test.go` cover invalidation and the drain verify step.
- `tasks/setkind/artifacts_test.go`: the tier order over `refine-*.md` names and non-report files.
- Goldens: `tasks/prompt_golden_test.go` (`TestVerifierPromptGoldens` :290, plus one test per prompt) compared with `tasks/testdata/prompts/*.md` through `internal/prompt/prompttest`. To regenerate, run `go test ./tasks/ -update-prompt-goldens`.
- The gate for a change is `go build ./... && go vet ./tasks/... && go test ./tasks/...`, then `make test`.

### Read these in ranges
- `tasks/verify.go`: 120-168 (result and options), 225-356 (resolve, Accept, Remediate), 506-622 (run, store, cache), 640-668 (`drainVerifyPhase`), 1193-1260 (prompt view), 1411-1456 (printers), 1499-1632 (parser).
- `tasks/refine.go`: 15-35 (constants), 158-235 (pass flow and `--show`), 446-600 (document machinery).
- `tasks/render.go`: 530-625 (row detail and `firstFindingsLine`), 780-797 (artifact summary section).
- `tasks/gates.go`: 130-160 (menu loop), 286-320 (pager), 374-410 (menu build).
