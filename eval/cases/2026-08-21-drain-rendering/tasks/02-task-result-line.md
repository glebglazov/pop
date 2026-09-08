# 02 — Task result line

## What to build

Every per-task ending in a whole-set drain prints exactly one Task result line, from the single chokepoint that sees all endings: a glyph, the `<set>/<task>` reference, and the outcome word, with the whole line colored —

- `✓ <set>/<task> done` — green
- `✗ <set>/<task> failed` — red (execution error after all retries)
- `✗ <set>/<task> out of agents (left open)` — red
- `◌ <set>/<task> quota-paused (<preset>)` — yellow
- `◌ <set>/<task> interrupted` — yellow

The line absorbs the existing success-only "Completed" line; the implementation-commit detail (sha or verified no-op) stays as detail beneath the green line. Blocked and Deferred are set-level terminal statuses handled by the terminal switch and the end-of-run summary — they never appear as Task result lines. Lines always print; color follows the drain output layer's existing TTY and NO_COLOR handling. The Work journal is untouched: stdout-only by design, no new store rows.

Glossary terms: Task result line, Drain, Work journal (see CONTEXT.md and the session fragment `.grill-context/CONTEXT.0032.C1F34703.md`).

## Orientation

Perishable pointers, as of authoring:

- The chokepoint: `(*implementRun).runSelectedTask` in `tasks/run_selected_task.go` — success ~line 188, exec error ~lines 99-143, interrupt ~lines 108-123, exhausted-walk-leaves-open ~lines 124-131, quota pause ~lines 145-186.
- The line to absorb: `printConciseSummary` in `tasks/attempts.go` (~line 749), called at ~line 310; keep `printAttemptBreakdown` (~line 313) and the commit-sha detail beneath the green line.
- Existing failure narration to leave in place (attempt-grain, not task-grain): `✗ Attempt %d/%d failed` (~attempts.go:317), `disposeExhaustedWalk` in `tasks/attempt_cap_exhaustion.go` (~line 146).
- Styling: `tasks/output.go` — `ansiGreen`/`ansiRed`/`ansiYellow`, `outputFor(w).line(...)`, TTY + NO_COLOR detection built in.
- Prove with: `go build ./... && go vet ./tasks/... && go test ./tasks/...`

## Type

AFK

## Acceptance criteria

- [x] A task that completes prints one green result line naming `<set>/<task>` and "done", with the implementation-commit detail beneath it
- [x] A task that fails after all retries prints one red result line; a task whose agent list is exhausted prints the red out-of-agents form and the task stays open
- [x] A quota pause prints the yellow form naming the preset; an interrupt prints the yellow interrupted form
- [x] No duplicate per-task terminal line remains (the old success-only "Completed" line is absorbed, not doubled)
- [x] Blocked/Deferred produce no Task result line
- [x] Lines print with output redirected (non-TTY); color only on a TTY, respecting NO_COLOR
- [x] `go build ./... && go vet ./tasks/... && go test ./tasks/...` passes

## Blocked by

None - can start immediately
