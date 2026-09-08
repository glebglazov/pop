## Parent

ADR-0245 (docs/adr/0245-verification-publishes-a-verify-report.md).

## What to build

A verification run leaves a readable document behind. After the Verifier judges
a set, its reasoning is on disk as a **Verify report**: what it checked, and why
each acceptance criterion is met or unmet.

The Verifier authors it. Its reply keeps the machine-read verdict line first —
so the agent commits to the enum before it begins justifying it — and everything
after that line becomes the report body. The verdict continues to parse exactly
as it does today, including for a malformed or absent reply.

A report is written on **every** verdict, PASS included. The response contract
changes accordingly: where it currently tells the Verifier to leave its findings
empty for a PASS, it now asks a passing run to state what it checked and why each
criterion is met. A green set that records nothing is the case the old contract
served worst, and it is the one this slice fixes.

One document per Verifier invocation, stamped with the instant, so a
verify-remediate-verify lap leaves the whole lap-by-lap trail rather than one
rewritten answer. The reports are deliberately not the verdict cache: the cache
stays keyed by work SHA and stays deleted when verification is invalidated,
while the reports survive that deletion and become the durable record the cache
never was. A cached verdict that is read back without invoking an agent writes
no new report — only a real invocation does.

The Verifier is never shown its own previous reports. That is a decision, not an
omission: the verdict gates a drain, and a judge reading its own homework anchors
the enum.

## Orientation

Perishable pointers, as of authoring — verify before trusting.

- `tasks/prompts/verifier.tmpl.md` carries the response contract; the
  `FINDINGS: ... leave empty for PASS` line is the one that changes. Regenerate
  goldens with the `-update-prompt-goldens` flag the prompt tests take.
- `tasks/verify.go` holds `VerifyResult`, the reply parser around
  `extractFindings` / `extractVerifierSummary`, `printVerdict`, and the
  malformed-reply path through `unparsedFindings`.
- `tasks/refine.go`'s `splitRefinerReply` is the precedent for peeling a leading
  machine-read line off a prose reply — reuse its shape rather than inventing one.
- The shared document machinery from slice 01 is what files the report.
- `tasks/verify_phase.go` is the drain's verify step; `store/verify.go` holds the
  `VerifyVerdict` row and is where the cache/report divergence is visible.
- Prove it with `go build ./... && go vet ./tasks/... && go test ./tasks/...`,
  then `make test`.

## Type

AFK

## Acceptance criteria

- [x] A Verifier invocation writes a report under the set's own directory,
      stamped with the instant, one document per invocation
- [x] The verdict line is parsed off the front of the reply and the remaining
      prose becomes the report body
- [x] A malformed or absent reply still yields the verdict it yields today, and
      does not lose the raw text
- [x] A report is written for PASS, FIXABLE and NEEDS-HUMAN alike
- [x] The response contract asks a PASS to state what was checked and why each
      criterion is met, and no longer tells it to leave findings empty
- [x] A verdict served from cache without invoking an agent writes no report
- [x] Reports survive verification invalidation; the verdict cache still does not
- [x] The Verifier prompt carries no previous report
- [x] Prompt goldens regenerated; `go build ./...`, `go vet ./tasks/...` and
      `make test` all pass

## Blocked by

- 01-shared-report-machinery
