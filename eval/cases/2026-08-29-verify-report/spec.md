## Parent

ADR-0245 (docs/adr/0245-verification-publishes-a-verify-report.md).

## What to build

A prefactor, with no behaviour change anywhere: the machinery that turns an
agent's prose into a timestamped, superseded-latest report — and the pointer
every surface carries it by — becomes one shape parameterised by which pass
wrote it, instead of belonging to Refine.

Today that machinery is Refine's alone: rendering a document with its header of
facts, filing it under the set's own subdirectory with an instant in the name,
scanning that directory for the newest, reading a header field back out of the
document, phrasing the commit it describes, and answering whether the checkout
has moved past it. ADR-0245 gives verification a report with the same mechanics
and a different role, so the mechanics move somewhere both passes can reach and
Refine starts using them through that seam.

Nothing a user or an agent can observe changes. The Refine report keeps its
directory, its filename stamp, its header and every surface it renders on, and
the refine prompt goldens are untouched — if a golden moves, the prefactor went
too far.

## Orientation

Perishable pointers, as of authoring — verify before trusting.

- `tasks/refine.go` holds `renderRefineDocument`, `writeRefineDocument`,
  `latestRefineDocument` and the `refineDirName` constant.
- `tasks/refine_pointer.go` holds `RefinePointer`, `latestRefinePointer`,
  `refineHeaderField`, `CommitPhrase`, `Summary`, `StaleAgainst`, and the
  `refineBlockView` / `refineBlock` pair the prompt views render.
- `tasks/artifacts.go` holds `ArtifactTypeRefine`, `RefineDirName`,
  `RefineFileInstant`, `artifactTier` and `Artifacts`.
- The header-field reader is the interesting part: it parses the report's own
  first bullet list rather than keeping a side-car, which is a property worth
  preserving in the shared form.
- Prove it with `go build ./... && go vet ./tasks/... && go test ./tasks/...`,
  then `make test` for the whole tree. The refine prompt goldens under
  `tasks/testdata/prompts/` must not need regenerating.

## Type

AFK

## Acceptance criteria

- [x] Document rendering, filing, newest-by-timestamp scanning and header-field
      reading exist in one form that takes the pass as a parameter
- [x] The pointer type — path, work SHA, commit range, written instant, commit
      phrase, summary line and staleness check — is likewise shared
- [x] Refine reaches all of it through the shared seam and holds no private copy
- [x] The artifact tier order is expressed so a second report type can take a
      position without renumbering by hand
- [x] Refine's behaviour is unchanged: same directory, filenames, header, and
      every surface renders exactly as before
- [x] The refine prompt goldens are unmodified
- [x] `go build ./...`, `go vet ./tasks/...` and `make test` all pass

## Blocked by

- None - can start immediately

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

## Parent

ADR-0245 (docs/adr/0245-verification-publishes-a-verify-report.md).

## What to build

When a human overrides a non-PASS judgment by recording a PASS themselves, that
decision leaves a report too — so the set most likely to puzzle a future reader
("why is this green when the findings said otherwise?") answers the question
itself.

No agent runs on this path, so there is no reply to split: pop renders the
document. It carries the human's own rationale, the verdict that was overridden,
and the fact that a human authored it — stated plainly enough that a reader
scanning the set's reports can tell this one apart from a Verifier's at a glance.

It files under the same directory, with the same timestamp stamp and the same
header of facts as a Verifier-authored report, so every reader that finds the
latest report finds this one when it is the latest.

## Orientation

Perishable pointers, as of authoring — verify before trusting.

- `tasks/verify.go` writes the human-authored verdict; the row it builds sets
  `HumanAuthored: true`.
- `store/verify.go` documents the field pair this renders from: `HumanAuthored`
  marks the override and `Note` carries the human's rationale. The same doc
  comment records that the note is already fed into later Verifier prompts
  "without suppressing a fresh judgment" — that behaviour stays as it is.
- The shared document machinery from slice 01 files it; slice 02's
  Verifier-authored report is the shape to match.
- Prove it with `go build ./... && go vet ./tasks/... && go test ./tasks/...`,
  then `make test`.

## Type

AFK

## Acceptance criteria

- [x] Recording a human PASS over a non-PASS judgment writes a report
- [x] The report carries the human's rationale and names the verdict it overrode
- [x] The report is identifiable as human-authored rather than Verifier-authored
- [x] It files in the same directory with the same stamp and header shape, and
      the latest-report readers pick it up like any other
- [x] No agent is invoked on this path
- [x] `go build ./...`, `go vet ./tasks/...` and `make test` all pass

## Blocked by

- 02-verifier-writes-the-report

## Parent

ADR-0245 (docs/adr/0245-verification-publishes-a-verify-report.md).

## What to build

A human who wants to know why verification judged as it did can find the report
from wherever they already are, without knowing a path.

The report reaches them as a **pointer** — where the document is and which commit
it was written against, never a line of what it says — on the surfaces a human
reads: the sign-off gate's preamble, the gate's own paging entry so the document
can be read without leaving the prompt, the set's detail view, and the set's
artifact list. In the artifact list it ranks above the Refine report: a verdict
outranks a polish note.

It stays **out** of the agent-facing prompt views that the Refine pointer rides.
An agent sent to fix something already has the findings in its task body, and a
second copy invites it to treat the report as the spec.

The report answers *why*, never *whether*. Staleness stays the existing
verified-at badge's question, and this slice must not introduce a second answer
to it — a set whose report is older than HEAD is not thereby unverified.

Truncating a verification's reasons to their first line is what made this
necessary; the one-line form stays where a status row genuinely has room for
only one line, but the pointer to the full document is now beside it.

## Orientation

Perishable pointers, as of authoring — verify before trusting.

- `tasks/refine_pointer.go` is the model: `refineBlock` renders into five prompt
  views plus the detail view, and `RefinePointer` drives the gate preamble
  (`gateRefinePreamble`) and the paging entry (`pageRefineDocument`). Take the
  human-facing subset only.
- `tasks/render.go` holds the detail-view rendering and the two
  `firstFindingsLine(row.VerifyFindings)` truncations.
- `tasks/artifacts.go` holds `Artifacts` and `artifactTier` — the tier order is
  where the ranking above refine is expressed.
- The verified-at badge is the concept not to duplicate; `tasks/verify_mark.go`
  and `tasks/verified_status.go` own it.
- Prompt goldens live under `tasks/testdata/prompts/`; the agent-facing verify
  prompts must not gain a block, so their goldens should not move.
- Prove it with `go build ./... && go vet ./tasks/... && go test ./tasks/...`,
  then `make test`.

## Type

AFK

## Acceptance criteria

- [x] The sign-off gate's preamble names the latest Verify report and the commit
      it was written against
- [x] The gate offers a paging entry that opens the document
- [x] The set's detail view carries the pointer
- [x] The report appears in the set's artifact list, ranked above the Refine
      report
- [x] No agent-facing prompt view gains the pointer, and their goldens are
      unmodified
- [x] A set that has never been verified renders nothing at all on any of these
      surfaces
- [x] The verified-at badge's behaviour is unchanged, and report staleness never
      affects a set's status
- [x] `go build ./...`, `go vet ./tasks/...` and `make test` all pass

## Blocked by

- 02-verifier-writes-the-report
