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
