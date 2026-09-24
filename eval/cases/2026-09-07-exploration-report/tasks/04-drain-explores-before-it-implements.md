## Parent

docs/adr/0262-an-interrelated-task-set-explores-once-before-it-implements.md

## What to build

A drain of a set that declared it needs exploration, and has no report yet,
writes one before the first task runs. A set that already has a report reuses it
and the drain spends nothing on exploring. A set that never asked is untouched.
Configuration gates the step for the whole machine and defaults **on** — the
first agent phase that runs unasked, which is safe only because the set's own
declaration is what triggers it.

Nothing anywhere compares the report's recorded tree to the checkout. Absent
means write it; present means reuse it.

## Orientation

Perishable pointers, as of authoring: verify before trusting.

- `tasks/run_tasks.go` — `implementRun.loop` (~186) and its `for` (~193). The
  refine step is the *first* directive of the quiescence branch (~214); this one
  runs at the **head**, before the loop's first task selection.
- `tasks/refine_phase.go` — the directive shape to copy, including how it
  declines for a human-completed set (`m.HumanCompleted`) and how the drain
  treats a phase that hands nothing back.
- `config/config.go` — `RefineConfig` (~350) and `WorkConfig.Refine` (~553) are
  the group shape; `Enabled bool` there defaults off by zero value, so use the
  `*bool` pattern (`KillPanePromptEnabled`, ~118) for a key that defaults on.
- `tasks/implement_run.go` resolves the turn cap and the convention once per run;
  `tasks/executor.go` does the same for a single-task run.
- `tasks/run_tasks_test.go` (~3.4k lines — read ranges) holds the drain family's
  table tests.
- Verify: `go build ./... && go vet ./tasks/... ./config/... && go test ./tasks/... ./config/...`

## Type

AFK

## Acceptance criteria

- [x] A drain of a declared set with no report runs the pass at the head of the
      drain, before any task is selected.
- [x] A drain of a declared set that already has a report runs no pass.
- [x] A drain of a set that never declared exploration runs no pass.
- [x] The step is gated by its own config group and **defaults on** — absent
      configuration explores.
- [x] Config off means no pass in any drain, and the hand-run verb still works.
- [x] The `explorer` object overrides the configured agent list and effort for
      the pass; CLI flags still win over it.
- [x] The step declines for a human-completed set, as refinement does.
- [x] No freshness comparison exists anywhere in the path: the recorded tree is
      prose and nothing reads it back.
- [x] A test drives a whole drain both ways — declared with no report, and with
      a report already there — and asserts on what ran.
- [x] `go build ./... && go vet ./tasks/... ./config/... && go test ./tasks/... ./config/...` passes.

## Blocked by

- 02-explore-a-set-by-hand
