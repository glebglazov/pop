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

## Parent

docs/adr/0262-an-interrelated-task-set-explores-once-before-it-implements.md

## What to build

A set whose author said its tasks are interrelated must not have its builders
start from separate maps, so when exploration cannot produce a report the set
parks instead of building blind. The pass retries up to its own cap first, so a
park is a considered outcome rather than one flake. A parked set gets a status of
its own — not the one that means a human owes a decision — and nothing re-picks
it, so no daemon loops on it. One set-level progress record says why the work
stopped, readable without opening a captured run.

Three ways out: exploring by hand, draining this set once without exploring, or
retracting the declaration.

This slice is deliberately whole. Splitting it would either go horizontal or
ship an intermediate state where the daemon re-explores a parked set forever.

## Orientation

Perishable pointers, as of authoring: verify before trusting.

- `tasks/status.go` — the status constants (~20-36, note `StatusVerifyFailed`
  and its comment) and the derivation (~141-211). `work.SetStatus` in `work/` is
  the underlying enum; `work/ref` holds the closed kind enum.
- `tasks/verified_status.go` — the one read-side resolution answering both a
  set's status and its verification mark; a new park resolves here, not per
  surface.
- `tasks/drain/status.go` and `tasks/drain/queue.go` — the scan and the
  `Decision` it produces, which is what the supervisor dispatches on;
  `supervisor/supervisor.go` sequences reconcile, candidates and dispatch.
- `tasks/progress.go` — `AppendSetProgress`, already there for events belonging
  to the set as a whole.
- `tasks/attempt_cap_exhaustion.go` for what exhaustion does today; `cmd/tasks.go`
  for the implement flags.
- `CONTEXT.md` around the status derivation table (~557-563) holds the drain-stop
  list in prose.
- Verify: `go build ./... && go vet ./tasks/... ./work/... ./supervisor/... ./cmd/... && go test ./tasks/... ./work/... ./supervisor/... ./cmd/...`

## Type

AFK

## Acceptance criteria

- [x] The pass retries up to a standard per-phase cap before it gives up.
- [x] A declared set whose pass gave up with no report reads a park status of its
      own, and the drain stops there.
- [x] The park derives from the captured run of the explore phase plus the
      absence of the report — no new store column and no new table.
- [x] The park status is not the existing blocked status: that one means a human
      owes a decision, and it keeps that meaning.
- [x] Automatic selection and the Work daemon pass over a parked set, so nothing
      re-explores it in a loop.
- [x] The park appends exactly one set-level progress record naming the phase and
      the reason.
- [x] Draining with the skip flag drains a declared set once without exploring
      and without parking.
- [x] A successful hand-run pass clears the park, and so does retracting the
      declaration.
- [x] The status derivation prose in `CONTEXT.md` names the new park in the
      drain-stop list.
- [x] `go build ./... && go vet ./tasks/... ./work/... ./supervisor/... ./cmd/... && go test ./tasks/... ./work/... ./supervisor/... ./cmd/...` passes.

## Blocked by

- 04-drain-explores-before-it-implements

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
