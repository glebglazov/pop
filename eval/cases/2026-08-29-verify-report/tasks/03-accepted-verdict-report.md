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
