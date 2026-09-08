# 01 — Drain header

## What to build

Every whole-set `pop tasks implement` opens with an unconditional Drain header, printed at the very beginning (before the status table): the Task set identifier, the resolved Runtime path, and the Worktree binding kind (managed or adopted). Two conditional follow-up lines: "invoked from <cwd>" only when the invocation directory differs from the Runtime path, and an announcement when the run just recorded a Default binding to the current checkout ("binding recorded to current checkout"). The old conditional outside-the-binding report line is folded into this header and deleted — where a drain runs is always stated, not only when it surprises.

A single-task file run (`<set>/<file>.md`) has no drain, so it prints a sibling line instead: that it runs in the current checkout, with the path.

Lines always print; color (if any) follows the drain output layer's existing TTY and NO_COLOR handling. Glossary terms: Drain header, Runtime path, Worktree binding, Default binding (see CONTEXT.md and the session fragment `.grill-context/CONTEXT.0032.C1F34703.md`).

## Orientation

Perishable pointers, as of authoring:

- Route resolution and the line to fold: `tasks/implement/run.go` — `resolveTaskSetRuntime` (~line 126), the existing `"draining at bound checkout %s"` print (~line 265), `--in-worktree` provisioning (~line 276), default-binding adoption hook (~line 308). Binding kind comes off the binding's `Provisioned` bit (`tasks/binding/`).
- Executor setup and current initial render: `tasks/implement_run.go` — `newImplementRun` (~line 82), `run.setup()` (~line 193), `BindCheckout` hook invocation (~line 211), initial `Render` (~line 233 in `run_tasks.go` path). The header belongs before that render.
- Single-task entry: `cmd/tasks.go` `runTaskRunTaskWith` (~line 1300) → `tasks.RunTaskWith`.
- Styling: `tasks/output.go` — `outputFor(w).line(style, ...)`, TTY + NO_COLOR detection built in. Drain output does not go through `ui/`.
- Prove with: `go build ./... && go vet ./tasks/... ./cmd/... && go test ./tasks/... ./cmd/...`

## Type

AFK

## Acceptance criteria

- [x] A whole-set drain invoked from inside its bound checkout prints a header naming the set, the Runtime path, and the binding kind (managed/adopted) before the status table
- [x] Invoked from a different directory than the Runtime path, the header adds an "invoked from" line with the invocation directory
- [x] A drain that records a Default binding announces it in the header
- [x] The old conditional "draining at bound checkout" line is removed; its information is carried by the header
- [x] A single-task file run prints its current-checkout line instead of the drain header
- [x] Lines print with output redirected (non-TTY); color only on a TTY, respecting NO_COLOR
- [x] `go build ./... && go vet ./tasks/... ./cmd/... && go test ./tasks/... ./cmd/...` passes

## Blocked by

None - can start immediately

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
