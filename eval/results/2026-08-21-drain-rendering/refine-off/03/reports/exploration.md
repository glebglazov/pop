# Exploration report — 2026-08-21-drain-rendering

- Explored: 2026-10-10T09:56:54Z
- Work SHA: 7a2e65178433
- Explorer: claude --model "opus"

## Drain rendering — code as found at 7a2e6517

### Where things live
- `tasks/implement/run.go`: whole-set route and drain. `RunWholeSetWith` calls `resolveTaskSetRuntime` first, then `tasks.RunTaskSetWith`. It also holds `provisionInWorktree` (`--in-worktree`), the `bindCheckout` hook, and the Drain header print (`reportDrainHeader` → `tasks.RenderDrainHeader`, `tasks/drain_header.go`); the old "draining at bound checkout %s" line is gone.
- `tasks/binding/route.go`: `RouteDrainCheckout` returns `RouteDrainCheckoutResult{RuntimePath, UsedExistingBinding, ProvisionedManaged, AdoptedNamed (retired, never set), BoundDefault, Rebound, Binding}`. `tasks/binding/binding.go`: `Binding = store.Binding`, which has the `Provisioned` bit. `Adopt` derives `Provisioned` from the location (under the managed root). `AdoptCurrentCheckout` returns `(true, nil)` only when it records a new adopted binding.
- `tasks/implement_run.go`: `implementRun`. `newImplementRun` (82) resolves `runtimePath` and `out` (default `os.Stdout`), then calls `BeginDrain`. `setup()` (193) calls `RefreshManagedCheckout`, then `BindCheckout` (211), then the dirty check, then the "No runnable AFK work" line, then `fmt.Fprintln` + `Render` (233–234), then `RenderTaskList`.
- `tasks/run_tasks.go`: `RunTaskSetOptions` and `RunTaskSetResult` (`Completed []*RunTaskResult`). `RunTaskSetWith` runs `newImplementRun`, then `setup`, then `loop`. `loop` sends each iteration to `verifyPhase`, `reviewPhase`, `terminalStatus`, or `runSelectedTask`. `printTaskSetSummary` (259) prints the set-grain line "✓ Completed task set %s".
- `tasks/run_selected_task.go`: `runSelectedTask`. It is the one function that sees every per-task ending in a whole-set drain.
- `tasks/attempts.go`: `executeTaskAttempts` (108) and `executeTaskAttemptsWithAgentFallback` (352). On success `executeTaskAttempts` prints nothing; it records the attempt streams on the result. `printTaskDone` (`tasks/task_result_line.go`) prints the Task result line "✓ %s/%s done", then "Implementation commit: <12-char sha>" or "No implementation commit (verified no-op)", then the Attempt timing breakdown.
- `tasks/attempt_cap_exhaustion.go`: `disposeExhaustedWalk` (146), `exhaustedWalkFault`, `exhaustedWalkPreset`, `attemptFault.leavesTaskOpen` (true only for `faultProvider`).
- `tasks/executor.go`: `RunTaskWith` (84) is the single-task path. It has no route step and no binding report. Its first output is `fmt.Fprintln(out)` + `Render` (about line 183), before the confirm.
- `cmd/tasks.go`: `runTaskImplement` uses `isTaskFileTarget` to choose `runTaskRunTaskWith` (1302, single task) or `runTaskRunTasksWith` (1332, whole set). Both write to `stdout`. `taskResolveInput` sets `CWD: cmdLayerDeps().WorkDir()`.
- `tasks/output.go`: `output`, `outputFor`, `colorEnabled`, `writerIsTerminal`, `styled`, `line`, and the `ansi*` constants.

### Which seam owns what
- `resolveTaskSetRuntime` decides the route. Its outputs are only the `tasks.ResolveInput` (with `RuntimeOverride` set when `Rebound`, `UsedExistingBinding`, `ProvisionedManaged`, or an override is given) and an error. The `route` value, `BoundDefault`, `route.Binding.Provisioned`, and `resolved.ProjectPath` do not leave this function.
- `provisionInWorktree` returns early. It does not reach `RouteDrainCheckout`; `provisionInWorktreeReported` wraps it and prints the Drain header. `binding.ProvisionWorktree` returns `Provisioned: true`.
- The `BoundDefault` binding is written inside `RouteDrainCheckout` step 4, before `tasks.RunTaskSetWith` starts. The executor's `BindCheckout` hook (`AdoptCurrentCheckout`) is a second, later adoption point. It is a no-op when a binding exists or on trunk, and its `bool` result is discarded in `bindCheckout`.
- `implementRun.runtimePath` (from `ResolveRuntimePathWith(resolved.ProjectPath, opts.RuntimeOverride)`) is the Runtime path that the executor uses. `RunTaskSetOptions` does not carry binding kind or invocation directory.
- Per-task endings inside `runSelectedTask`:
  - Exec error, interrupt: `isInterrupted(execErr)` leads to `r.interruptGate`. Continue returns `runTaskContinue`.
  - Exec error, exhausted walk left open: `exhaustedWalkFault` with `leavesTaskOpen()` returns `execErr`.
  - Exec error, failed: `r.failedGate`. If handled, the drain continues. If not, `printFailedStopAdvice` runs.
  - Pause: `taskResult.ProceedVerdict` non-time-healing returns `ExitSetup`. Time-healing goes to `ParkAndWaitForQuotaRecovery` (`tasks/recovery.go:119`). If `regFailed`, the pause fields are set, `printTaskSetSummary` runs, and it returns.
  - Success: `result.Completed = append(...)` (188).
  - All of these error branches run only when `refreshErr == nil`.
  - The "Paused: agent quota exhausted for %s/%s" line comes from `executeTaskAttempts` (286), not from `runSelectedTask`.
- `printTaskDone` (which calls `printAttemptBreakdown`, `tasks/timings.go:529`) is called by `runSelectedTask` (whole set) and `RunTaskWith` (single task). Both paths use `executeTaskAttempts`.
- Blocked and Deferred are handled in `terminalStatus` (`tasks/terminal_switch.go:35`). That function is not on the `runSelectedTask` path.

### Invariants that hold
- `outputFor` reuses an existing `*output`. Color is on only when the writer is an `*os.File` char device and `NO_COLOR` is empty (`colorEnabled`). A buffer never gets ANSI codes.
- `confirm.Output == nil` means `io.Discard` in `resolveTaskSetRuntime`. `ResolveTaskSetRuntime` passes an empty `RuntimeConfirmOptions`.
- A repository mismatch between the current checkout and the runtime is refused (`resolveTaskSetRuntime`, 235).
- `runTaskDirective` returns: `runTaskContinue` carries a nil result, and the return branches carry `r.result` (nil on a dirty-strategy failure).
- `disposeExhaustedWalk` writes the Failed transition (`finalizeTaskFailed`) unless the fault is a provider fault, so the task stays Open. "✗ Out of agents for %s/%s after %d attempts" and "✗ Attempt %d/%d failed" are attempt-grain lines.
- The Work journal is written from Drain rows (`finalizeDrain` in `implementRun.finalize`), not from these stdout lines.
- `RenderTaskComplete` (`tasks/notices.go:54`, "✓ Completed task %s/%s") is a separate line for HITL and human completion.

### The test shape
- `tasks/implement/run_test.go` drives `ResolveTaskSetRuntime` and `ResolveTaskSetRuntimeWith` on a git fixture (`setupImplementFixture`). `TestResolveTaskSetRuntimeBoundDrainsAtBindingFromOtherCheckout` (284) asserts the Drain header lines.
- `tasks/run_tasks_test.go` drives `RunTaskSetWith` with `setupRunTaskSetFixture` + `writeFakeAgent` + `env.runTaskSetOpts(yes, agent, &buf)`. `TestRunTaskSetYesPrintsConciseSummary` (around 1808) asserts "✓ demo/01-a done" and "✓ Completed task set demo".
- `tasks/run_selected_task_test.go` covers the quota-pause registration failure and the Failed gate (handled and unhandled).
- Other tests that assert the current strings:
  - "✓ demo/01-a done": `tasks/timings_test.go:831` (it also checks that this line comes before "Attempt timing"), `cmd/tasks_test.go:891` (`runTaskRunTasksWith`), and `supervisor/supervisor_spawn_test.go:80` ("✓ queue-drain/01-a done").
  - "Out of agents for …": `tasks/agent_exhaustion_disposition_test.go:47` and `tasks/agent_exhaustion_fallback_test.go:91`.
- `tasks/output_test.go` builds `&output{Writer: &buf, color: true}` to test color. `TestColorEnabledHonorsNoColor` uses `t.Setenv` and stays serial.
- Gates: `go build ./... && go vet ./tasks/... ./cmd/... && go test ./tasks/... ./cmd/...`. The supervisor test above is outside that scope, but `make test` includes it.

### Read these in ranges
- `tasks/implement/run.go` 125–268 (route and old report line), 302–313 (`bindCheckout`).
- `tasks/binding/route.go` 52–78 (result struct), 180–264 (steps 1–4).
- `tasks/implement_run.go` 82–163, 193–266.
- `tasks/attempts.go` 280–335 (success, quota, and failure lines), 352–465 (fallback walk and exhaustion), 749–757.
- `tasks/attempt_cap_exhaustion.go` 100–175.
- `tasks/executor.go` 84–300 (single-task path).
- `cmd/tasks.go` 1246–1370.
- `tasks/run_tasks_test.go` (about 3.4k lines): 1590–1610 and 1800–1825 only.
