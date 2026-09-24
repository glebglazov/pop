## Parent

docs/adr/0262-an-interrelated-task-set-explores-once-before-it-implements.md

## What to build

Exploring a set by hand writes its report. A fresh agent is given the set's own
tasks and its spec, and it records the code as found in the area those tasks
touch: where things live, which seam owns what, which invariants hold, the test
shape, and which files to read in ranges. Every claim names a path or a symbol,
the whole thing fits about a page, and it states the tree it was observed at as
prose. The pass files a captured run and spends against a phase of its own, as
verification and refinement do. It asks for nothing about prior art.

A hand run is the human re-opening the question, so it runs on any set whatever
the set's own declaration says, and it rewrites an existing report.

## Orientation

Perishable pointers, as of authoring: verify before trusting.

- `tasks/refine.go` is the closest sibling and the model for the whole pass:
  role framing, the reply split, the agent walk. `tasks/agent_fallback_walk.go`
  is the shared walk both roles use, differing only in an `agentRole`.
- `tasks/pass_report.go` — the report machinery: the document render with its
  header of facts and the header read-back. This report is **flat**, so take the
  header and the render and leave the timestamped filing and newest-by-timestamp
  scan to the two families that rerun.
- `tasks/spent_retry_cap.go` — `spendPhaseImplement` / `spendPhaseVerify` /
  `spendPhaseRefine`; add the fourth beside them. `tasks/stream.go`'s `Phase`
  field is what a captured run is filtered on.
- `tasks/prompts/` holds the templates and `tasks/testdata/prompts/` the golden
  files; `tasks/prompt_golden_test.go` drives them.
- `cmd/tasks.go` — verb wiring; `pop tasks refine`'s own wiring is the model,
  including the seam `cmd` uses to hand the pass its convention and agents.
- `tasks/agent.go` — the role presets and their declared capabilities.
- Verify: `go build ./... && go vet ./tasks/... ./cmd/... && go test ./tasks/... ./cmd/...`

## Type

AFK

## Acceptance criteria

- [x] `pop tasks explore <set-id>` spawns a fresh agent whose prompt carries the
      set's manifest listing, its task bodies and its spec where one exists.
- [x] The prompt asks for the code as found in the area those tasks touch, one
      page, every claim naming a path or a symbol, plus the tree it describes.
- [x] The prompt asks for nothing about prior art, and adds no prior-art section
      to the report.
- [x] A successful pass writes `exploration.md` flat in the set directory, and
      overwrites an existing one.
- [x] The pass files a captured run of its own phase and spends against a cap of
      its own, beside implement, verify and refine.
- [x] A hand-run pass runs on any set on request, whatever its `explore` key
      says.
- [x] An interrupted or failed pass writes no report and leaves an existing one
      untouched.
- [x] Tests drive the whole path — command, agent reply, report on disk, captured
      run row — not the pieces separately.
- [x] `go build ./... && go vet ./tasks/... ./cmd/... && go test ./tasks/... ./cmd/...` passes.

## Blocked by

- 01-report-is-an-artifact-a-set-can-ask-for
