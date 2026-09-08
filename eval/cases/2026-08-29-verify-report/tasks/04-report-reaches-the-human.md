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
