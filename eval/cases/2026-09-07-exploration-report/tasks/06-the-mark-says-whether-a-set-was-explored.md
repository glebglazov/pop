## Parent

docs/adr/0262-an-interrelated-task-set-explores-once-before-it-implements.md

## What to build

You can see whether a set that asked for exploration got it, and where the work
stopped when it did not. The answer rides beside the set's status as a mark, the
way verification's and refinement's already do, with its reason — not yet run, or
the pass failed — as detail beside the mark rather than a value inside it. A set
that never asked carries no mark at all. The report itself reaches a human as a
pointer: its path and the tree it records, never its body.

## Orientation

Perishable pointers, as of authoring: verify before trusting.

- `tasks/verify_mark.go` and `tasks/verified_status.go` — the mark pattern and
  the single read-side resolution every surface reads, which is what stops a
  mark being re-derived per surface.
- `tasks/refine_pointer.go` plus `pass_report.go`'s `ReportPointer` — the shared
  pointer shape (path plus the commit the report's header records) that the
  sign-off gate, the detail view and the assist prompt all render.
- `dashboard/dashboard.go` (~3.4k lines — read ranges, not whole) for the status
  cells; `dashboard/render.go` for detail sections; `dashboard/status_render.go`
  for the plain-text render `pop work status` prints; `tasks/render.go` for
  `pop tasks status`.
- `tasks/gate_menu.go` for the sign-off gate's preamble and paging entry.
- Verify: `go build ./... && go vet ./tasks/... ./dashboard/... && go test ./tasks/... ./dashboard/...`

## Type

AFK

## Acceptance criteria

- [x] A set that asked for exploration and has no report carries a mark beside
      its status, with its reason as detail beside the mark.
- [x] A set that never asked carries no mark on any surface.
- [x] The mark resolves once on the read side beside the verification mark, and
      no surface derives it again.
- [x] The dashboard row, the dashboard detail view, `pop work status` and
      `pop tasks status` all render that one resolution.
- [x] The detail view carries the report as a pointer — path plus the tree it
      records — and never its body.
- [x] The sign-off gate names the report where it already names the refine
      report.
- [x] `go build ./... && go vet ./tasks/... ./dashboard/... && go test ./tasks/... ./dashboard/...` passes.

## Blocked by

- 05-a-set-that-cannot-be-explored-parks
