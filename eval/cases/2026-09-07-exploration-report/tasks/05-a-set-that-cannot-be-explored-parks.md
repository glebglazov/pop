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
