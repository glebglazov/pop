# Exploration report — 2026-08-21-drain-rendering

- Explored: 2026-10-10T09:27:11Z
- Work SHA: 7a2e65178433
- Explorer: claude --model "opus"

## Drain rendering — code as found at 7a2e6517

### Where things live
- `tasks/implement/run.go` — runs a whole-set Implement. `RunWholeSetWith` calls `resolveTaskSetRuntime`, then `tasks.RunTaskSetWith`. Its own hooks are `bindCheckout` and `refreshManagedCheckout`. Its only production caller is `cmd/tasks.go` `runTaskRunTasksWith` (~1331).
- `tasks/run_tasks.go` — `RunTaskSetWith` (122) runs `newImplementRun`, then `run.setup()`, then `run.loop()`. Also holds `printTaskSetSummary` (259), the set-level summary lines (Done, Deferred, Verify-failed, Awaiting-approval, Blocked, QuotaPaused, "stopped").
- `tasks/implement_run.go` — the `implementRun` struct, `newImplementRun` (82) and `setup()` (193). `setup()` calls RefreshManagedCheckout, then BindCheckout, then the dirty check, then the "No runnable AFK work" line. Then `Render` (234) and `RenderTaskList` print the status table.
- `tasks/run_selected_task.go` — `(*implementRun).runSelectedTask`, the branch that runs one task.
- `tasks/attempts.go` — the attempt loop. `printConciseSummary` is gone (task 02): a completed attempt carries its stream files as `RunTaskResult.AttemptStreams`, and `tasks/task_result_line.go` prints the Task result line.
- `tasks/attempt_cap_exhaustion.go` — `disposeExhaustedWalk` (146).
- `tasks/output.go` — `output`, `outputFor`, `colorEnabled`, `writerIsTerminal`, `line`, and the `ansi*` constants.
- `tasks/executor.go` — `RunTaskWith` (84), the single-task file run. It is called from `cmd/tasks.go` `runTaskRunTaskWith` (1302).
- `tasks/binding/route.go` — `RouteDrainCheckout` and `RouteDrainCheckoutResult`. `tasks/binding/binding.go` — `AdoptCurrentCheckout` (258). `Binding` is an alias of `store.Binding`, which carries the `Provisioned` bit.

### Which seam owns what
- **Route and Runtime path:** `resolveTaskSetRuntime` (`implement/run.go:125`). It calls `binding.RouteDrainCheckout`, and the result sets `in.RuntimeOverride` only for `Rebound`, `UsedExistingBinding`, `ProvisionedManaged` or an explicit override (247). Each result flag means one thing:
  - `BoundDefault`: route step 4 (`route.go:~255`) has just recorded a Default binding to the current checkout. With this flag, `RuntimeOverride` is not set.
  - `Binding`: the bound row, with its `Provisioned` bit.
- **The line to fold:** folded (task 01). `"draining at bound checkout %s"` and `RuntimeConfirmOptions.Output` are gone. `resolveTaskSetRuntime` now also returns a `tasks.DrainPlacement` (binding kind, invocation checkout, Default binding recorded), passed as `RunTaskSetOptions.Placement`. `setup()` prints the Drain header from it before `Render` (`tasks/drain_header.go`). `RunTaskWith` prints the single-task current-checkout line before its `Render`.
- **`--in-worktree`:** returns early through `provisionInWorktree` (275) at lines 184 and 187. That path never reaches the route or the print. It records `ProvisionWorktree` → `Put`, a managed binding.
- **Executor-side binding:** `setup()` calls `opts.BindCheckout` → `bindCheckout` → `AdoptCurrentCheckout`. That function returns `(true, nil)` only when it writes a fresh adopted binding, and `bindCheckout` discards the bool. Trunk and an already-bound set give no write.
- **Where the Runtime path resolves:** `newImplementRun` resolves it with `ResolveRuntimePathWith(d, resolved.ProjectPath, opts.RuntimeOverride)` (100) and stores it in `r.runtimePath`. `RunTaskSetOptions.CWD` exists and is read by `newGateEnv`.
- **Task endings:** all go through `runSelectedTask`:
  - Error from `executeTaskAttemptsWithAgentFallback` (99), split three ways:
    - `isInterrupted` → `interruptGate`; Continue resumes, otherwise it returns (108-123).
    - `exhaustedWalkFault(...).leavesTaskOpen()` returns with no gate (124-131).
    - Otherwise `failedGate`: handled ⇒ continue; else `printFailedStopAdvice` (132-143).
  - `ProceedVerdict` (145):
    - Human-healing gives `ExitSetup` (147-162).
    - Time-healing gives `ParkAndWaitForQuotaRecovery`. If registration fails, the pause fields are set and `printTaskSetSummary` runs (175-184). A recovered wait continues.
  - Success prints the done Task result line (`printTaskDone`), then appends to `result.Completed`.
  - Task 02: the exec-error branch and the time-healing quota branch print their Task result line first.
- **Blocked, Deferred, Done, Awaiting-approval:** handled at set level by `(*implementRun).terminalStatus` (`tasks/terminal_switch.go:35`) and `finishRunTaskSet`/`printTaskSetSummary`.
- **Existing per-task lines:**
  - Success: `printTaskDone` (`tasks/task_result_line.go`) prints `✓ <set>/<task> done` (green), then `  Implementation commit: <sha12>` or `  No implementation commit (verified no-op)`. Then `printAttemptBreakdown`. `runSelectedTask` and `RunTaskWith` call it.
  - Exhaustion: `disposeExhaustedWalk` prints `✗ Out of agents for … after N attempts` (red), plus a Last line, an open/overrun advice line, and `finalizeTaskFailed` unless the task stays open.
  - Quota: `attempts.go:286-287` prints `Paused: agent quota exhausted for <set>/<task>` (yellow).
  - Attempt failure: `✗ Attempt %d/%d failed` (317).

### Invariants that hold
- **Shared code paths:**
  - `printTaskDone` and `executeTaskAttemptsWithAgentFallback` serve both the drain (`run_selected_task.go:98`) and the single-task run (`executor.go:259`). A change in `attempts.go` reaches both.
  - `notices.go:54` `RenderTaskComplete` (`✓ Completed task <set>/<task>`) is a separate line for HITL or manual completion.
- **Color:** `outputFor` gives color only when the writer is an `*os.File` char device and `NO_COLOR` is empty. An existing `*output` is reused. Tests assert that redirected output has no `\033[` (`run_tasks_test.go:~1823`).
- **Drain lifecycle:** `newImplementRun` stops at `BeginDrain`. `RunTaskSetWith` defers `finalize` before `setup()`, so a failure in `setup()` still finalizes the Drain. `r.result` exists only at the end of `setup()`.
- **Repository check:** `resolveTaskSetRuntime` refuses when the current and runtime repository identities differ (235-238). This check runs before any report line.
- **Single-task run:** `RunTaskWith` claims no Drain before confirmation. It prints `Render` (~172) before `confirmExecution`. A HITL target routes to `runTargetedHITLGate` before that point.

### The test shape
- `tasks/implement/run_test.go`: fixtures `setupImplementFixture`, `seedImplementManagedBinding`. It drives `ResolveTaskSetRuntime`/`ResolveTaskSetRuntimeWith`. `TestResolveTaskSetRuntimeBoundDrainsAtBindingFromOtherCheckout` (284) asserts the returned `DrainPlacement`. `tasks/implement/drain_header_test.go` drives `RunWholeSetWith` with a fake agent and asserts the Drain header lines.
- `tasks/run_tasks_test.go`: `setupRunTaskSetFixture` (3274), `runTaskSetOpts(yes, agentCmd, out)`, `writeFakeAgent`. These tests drive `RunTaskSetWith` and assert substrings of the output buffer.
- Tests that assert `✓ demo/01-a done` (or the queue variant): `run_tasks_test.go:1816`, `timings_test.go:831` (ordering before "Attempt timing"), `cmd/tasks_test.go:891`, `supervisor/supervisor_spawn_test.go:80`.
- Tests that assert `Out of agents for …`: `agent_exhaustion_fallback_test.go:91` and `agent_exhaustion_disposition_test.go:47`.
- Gates: the task prompts. `navigation.md` gives `make test` as the whole-tree gate.

### Read these in ranges
- `tasks/attempts.go` (886): 270-340 (completion and failure inside the attempt loop), 352-460 (fallback walk, `disposeExhaustedWalk` call at 457), 745-760.
- `cmd/tasks.go` (2076): 1247-1365.
- `tasks/run_tasks_test.go` (~3.4k): 1795-1825, 3274-3320.
- `tasks/executor.go`: 84-260.
- `tasks/binding/route.go`: 50-80, 114-265.
