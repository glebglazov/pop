## Parent

docs/adr/0262-an-interrelated-task-set-explores-once-before-it-implements.md

## What to build

A builder working a set that has an exploration report is handed it, so the
whole set builds against one picture of the code instead of each attempt
re-deriving its own. It arrives as a path, never inlined, the way the refine
report already reaches a builder. The builder is also told the one edit it may
make to that report: a line this attempt falsified — a moved file, a retired
seam — and nothing else. An attended session names the report the same way.

This is the slice the whole feature exists for: without it a report is written
and read by nobody.

## Orientation

Perishable pointers, as of authoring: verify before trusting.

- `tasks/prompts/agent.tmpl.md` (74 lines) — the frame every implement attempt
  rides. The edit-boundary paragraph and the `Do NOT modify` list are what the
  correction duty has to be reconciled with; the `## Before you hand-write
  mechanism` block is the prior-art check and must come through unchanged.
- `tasks/prompt.go` — `BuildAssistPrompt` and its view struct, `refineBlock`,
  and the comment at the refine pointer explaining why a report is named as a
  path like every task body. `tasks/refine_pointer.go` is the pointer shape.
- `tasks/prompts/assist.tmpl.md`, plus `tasks/testdata/prompts/*.md` and
  `tasks/prompt_golden_test.go` for the golden files.
- Verify: `go build ./... && go vet ./tasks/... && go test ./tasks/...`

## Type

AFK

## Acceptance criteria

- [x] An implement attempt on a set that has a report is told the report's path
      and what it is; an attempt on a set with none is told nothing.
- [x] The report is delivered as a path and never inlined, whatever its length.
- [x] The frame states the correction duty: edit the report only to fix a line
      this attempt falsified, and change nothing else in it.
- [x] The frame's edit boundary still reads true with that duty in it — the
      report is named as the exception it is, beside the task file.
- [x] The prior-art block reaches the builder byte-unchanged.
- [x] An assist session's prompt names the report's path beside the refine
      pointer and the recent progress.
- [x] Golden prompt tests cover both arms: a set with a report and a set without.
- [x] `go build ./... && go vet ./tasks/... && go test ./tasks/...` passes.

## Blocked by

- 02-explore-a-set-by-hand
