# Exploration report — 2026-08-21-drain-rendering

- Explored: 2026-10-10T00:10:56Z
- Work SHA: 7a2e65178433
- Explorer: claude --model "opus"

## Where things live

- `tasks/implement/run.go` — whole-set Implement: `RunWholeSetWith` (line 70) resolves the route, then calls `tasks.RunTaskSetWith`. `resolveTaskSetRuntime` does the routing and ends by printing the Drain header through `reportDrainHeader` (`tasks.RenderDrainHeader`); the old `"draining at bound checkout %s"` line is gone. `provisionInWorktree` (275) handles `--in-worktree`. `bindCheckout` (307) is the hook that adopts the current checkout.
- `tasks/binding/route.go` — `RouteDrainCheckout` and its result `RouteDrainCheckoutResult` (fields `RuntimePath`, `UsedExistingBinding`, `ProvisionedManaged`, `BoundDefault`, `Rebound`, `Binding`, lines 53–80). Step 4 (~252–266) writes the Default binding with `Adopt` + `Put`.
- `tasks/binding/binding.go` — `Binding = store.Binding`. Its `Provisioned` bit (doc at 93) is set from location: true = managed, false = adopted. Also holds `Adopt` (232) and `AdoptCurrentCheckout` (258).
- `tasks/run_tasks.go` — `RunTaskSetOptions` and `RunTaskSetResult` (13–114), `RunTaskSetWith` (122), `loop` (~150), `printTaskSetSummary` (set-level lines, ~259).
- `tasks/implement_run.go` — `implementRun`, `newImplementRun` (82), `setup` (193). The initial `Render` is at line 234, inside `setup`, after `BindCheckout` (211).
- `tasks/run_selected_task.go` — `(*implementRun).runSelectedTask`: the drain's per-task branch.
- `tasks/attempts.go` — `executeTaskAttempts` (108), `executeTaskAttemptsWithAgentFallback` (352).
- `tasks/attempt_cap_exhaustion.go` — `attemptFault.leavesTaskOpen` (39), `exhaustedWalkFault` (117), `disposeExhaustedWalk` (146).
- `tasks/executor.go` — `RunTaskWith` (84): the single-task file run. `cmd/tasks.go` — `runTaskRunTaskWith` (1302) runs a single task and `runTaskRunTasksWith` (~1332) runs a whole set.
- `tasks/output.go` — `output`, `outputFor`, `colorEnabled`, `writerIsTerminal`, `line`, and the `ansi*` constants.

## Which seam owns what

- **Route and binding kind:** `resolveTaskSetRuntime` owns the route. It returns only `tasks.ResolveInput`, so the `RouteDrainCheckoutResult` (including `BoundDefault` and `Binding.Provisioned`) does not get past `tasks/implement`. The `--in-worktree` path returns early through `provisionInWorktree` and calls no `RouteDrainCheckout`.
- **Where the location line is written:** `resolveTaskSetRuntime` writes to `RuntimeConfirmOptions.Output` before `tasks.RunTaskSetWith` starts. The executor's first stdout line comes later, from `setup` (the `hitlFallback` notice at 230, then `Render`).
- **Which path is adopted:** `bindCheckout` → `binding.AdoptCurrentCheckout` returns `(true, nil)` only when it records a fresh adopted binding. The hook in `RunTaskSetOptions.BindCheckout` returns only `error`, so that bool is lost.
- **Runtime path:** `newImplementRun` gets `runtimePath` from `ResolveRuntimePathWith(d, resolved.ProjectPath, opts.RuntimeOverride)`. The invocation directory is `opts.ResolveInput.CWD`.
- **Per-task endings:** `runSelectedTask` sees every ending:
  - done: line 188
  - interrupt: `isInterrupted`, 108–123
  - out of agents with the task left open: `exhaustedWalkFault` + `leavesTaskOpen`, 124–131
  - failed: Failed gate, then `printFailedStopAdvice`, 132–143
  - human-healing stop: 147–162
  - quota pause: `ParkAndWaitForQuotaRecovery`, 163–186

  The interrupt and the out-of-agents checks happen only when `refreshErr == nil` (100–102).
- **Today's success line:** `printTaskDone` (`tasks/task_result_line.go`) prints the done Task result line `✓ <set>/<task> done`, then the commit detail and the attempt breakdown. `runSelectedTask` calls it on success; the single-task `RunTaskWith` (executor.go) calls it too. `printConciseSummary` is gone; `executeTaskAttempts` only puts the attempt streams on the `RunTaskResult`.
- **Line inside the walk:** `disposeExhaustedWalk` prints `✗ Out of agents for %s/%s after %d attempts` for the out-of-agents ending, and calls `finalizeTaskFailed` unless `leavesTaskOpen`. The quota pause also prints `Paused: agent quota exhausted for %s/%s` inside `executeTaskAttempts` (~286).
- **Set-level terminals:** `terminalStatus` (`terminal_switch.go`) and `printTaskSetSummary` handle Blocked, Deferred, Done, AwaitingApproval and VerifyFailed.

## Invariants that hold

- `outputFor` reuses a writer that is already an `*output`. It enables color only when the writer is an `*os.File` char device and `NO_COLOR` is empty. A `bytes.Buffer` never gets ANSI codes (`TestOutputForBufferDoesNotEmitANSI`).
- `AdoptCurrentCheckout` never writes over an existing binding, and records nothing on trunk.
- `RouteDrainCheckout` step 4 is the only writer of a Default binding (`BoundDefault: true`).
- `newImplementRun` registers the deferred `finalize` before `BindCheckout` (run_tasks.go:138). Any failure in `setup` still finalizes the Drain row.
- The `Provisioned` bit is set from location by `Adopt`. `ProvisionWorktree` always sets `Provisioned: true` (binding.go:419).
- `finalizeDrain` reads `RunTaskSetResult` flags (`NoAgentStarted`, `ProceedVerdict`, …). The Work journal comes from Drain rows, not from stdout.

## The test shape

- `tasks/implement/run_test.go` drives `ResolveTaskSetRuntime` / `ResolveTaskSetRuntimeWith` against real git fixtures (`setupImplementFixture`). `TestResolveTaskSetRuntimeBoundDrainsAtBindingFromOtherCheckout` (284) checks the Drain header with its "invoked from" line. No test in `tasks/implement` or `cmd` calls `RunWholeSetWith` directly. The `cmd` tests use `runTaskRunTasksWith` / `runTaskRunTaskWith` with a `bytes.Buffer` stdout.
- `tasks/run_tasks_test.go` holds the drain tests. They use `setupRunTaskSetFixture` (3274), `runTaskSetOpts` (out = buffer), and `writeFakeAgent` / `fakeAgentStep` (`skipSentinel` drives the task to Failed).
- These tests check for the done line `"✓ <set>/<task> done"` (they checked `"✓ Completed <set>/<task>"` before task 02):
  - `tasks/run_tasks_test.go:1816`, `1826`
  - `tasks/executor_test.go:327` (single-task path)
  - `tasks/timings_test.go:831`
  - `cmd/tasks_test.go:891`
  - `supervisor/supervisor_spawn_test.go:80`

  The `"✓ Completed task %s/%s"` lines come from `RenderTaskComplete` (notices.go:54). They are a different string, but their prefix matches substring checks.
- The endings have their own test files: `tasks/agent_exhaustion_disposition_test.go`, `agent_exhaustion_fallback_test.go`, `interrupt_auto_drain_test.go`, `retry_test.go` and `drain_ending_test.go`. `tasks/output_test.go` checks the `NO_COLOR` handling with `t.Setenv`, so that test stays serial.
- To prove a change: `go build ./... && go vet ./tasks/... ./cmd/... && go test ./tasks/... ./cmd/...`

## Read these in ranges

- `tasks/run_tasks_test.go` (~3.4k lines): 1795–1830 (redirected output, no ANSI), 3240–3330 (fixture and options).
- `cmd/tasks.go` (2076 lines): 1270–1360.
- `tasks/attempts.go` (886 lines): 108–140, 270–350, 352–465, 600–630 (`completeSuccessfulTask`, `NoOp`).
- `tasks/binding/route.go`: 20–110, 240–266.
- `tasks/implement/run_test.go`: 120–160, 284–325.
