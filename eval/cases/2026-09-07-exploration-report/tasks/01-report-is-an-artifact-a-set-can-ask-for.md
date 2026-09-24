## Parent

docs/adr/0262-an-interrelated-task-set-explores-once-before-it-implements.md

## What to build

A task set's folder may hold an exploration report — the code as found — beside
the spec and the progress record, and every surface that lists a set's artifacts
lists it, ordered what was decided, what exists, what happened. A set's author
may declare in the manifest that the set needs exploration, and may steer which
agent and effort the pass will run at. The authoring guide teaches both keys and
states when to set the first. Registration accepts them and still refuses
everything it refused before.

## Orientation

Perishable pointers, as of authoring: verify before trusting.

- `tasks/artifacts.go` — the artifact type constants, `ProgressFileName`,
  `Artifacts()` and its tier ordering comment, `reportArtifactKinds` (the
  timestamped families, which this artifact is not part of),
  `ArtifactSectionTitle`.
- `tasks/manifest.go` — `SpecFileName`, the set-level key block near the
  `DeprecatedKeys` comment, `AgentDirective` with `VerifierOverride` /
  `RefinerOverride` (~line 679), and `validateManifest`, which owns the
  orphan-markdown check that must ignore the new file as it ignores `spec.md`.
- `tasks/authoring_guide.go` — the `pop tasks authoring-guide` text, generated
  from the constants the validator reads; there is a test asserting it cannot
  drift.
- Readers to extend: `dashboard/render.go`'s detail-view artifact section,
  `cmd/tasks_artifacts_test.go`, `cmd/tasks_artifacts_render_test.go`,
  `tasks/setkind/artifacts_test.go`.
- Verify: `go build ./... && go vet ./tasks/... ./cmd/... ./dashboard/... && go test ./tasks/... ./cmd/... ./dashboard/...`

## Type

AFK

## Acceptance criteria

- [x] An `exploration.md` in a set folder is listed as an artifact of its own
      type by the CLI and by the dashboard detail view, ordered after the spec
      and before the progress record.
- [x] A set folder holding `exploration.md` with no manifest entry for it still
      registers — the orphan-markdown check ignores it as it ignores `spec.md`.
- [x] `"explore": true` is an accepted set-level manifest key, readable through
      an accessor beside the verifier's and refiner's.
- [x] An `"explorer": {"agents": [...], "effort": "..."}` object is accepted and
      read through the existing agent-directive shape.
- [x] `"explore"` is opt-**in** — absent or false means the set never explores —
      where `verify` and `refine` are opt-out, and the guide says so in as many
      words.
- [x] `pop tasks authoring-guide` documents both keys and states the rule for
      setting `explore`: two or more AFK tasks touch the same seam and a later
      one depends on how an earlier one shapes it.
- [x] The guide's new text is generated from the same constants the validator
      reads, asserted by test rather than assumed.
- [x] `go build ./... && go vet ./tasks/... ./cmd/... ./dashboard/... && go test ./tasks/... ./cmd/... ./dashboard/...` passes.

## Blocked by

- None - can start immediately
