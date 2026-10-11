# Exploration report — 2026-08-21-drain-rendering

- Explored: 2026-10-10T00:32:34Z
- Work SHA: 7a2e65178433
- Explorer: claude --model "opus"

## The code as found at 7a2e6517: drain rendering

### Where things live
- `tasks/implement/run.go` resolves where a whole-set drain runs. `RunWholeSetWith` first calls `resolveTaskSetRuntime` and then calls `tasks.RunTaskSetWith`. This file also holds `provisionInWorktree` (`--in-worktree`), `bindCheckout` (wraps `binding.AdoptCurrentCheckout`) and `RuntimeConfirmOptions.Output`, which is the writer for the Drain header (`tasks.PrintDrainHeader`, `tasks/drain_header.go`); `routeTaskSetRuntime` returns the header and `resolveTaskSetRuntime` prints it.
- `tasks/run_tasks.go` holds `RunTaskSetOptions`, `RunTaskSetResult`, `RunTaskSetWith` and the `loop` method. It also holds `printTaskSetSummary`, which prints the set-level ending lines (Done, Deferred, Blocked, quota paused).
- `tasks/implement_run.go` holds the `implementRun` state, `newImplementRun` (which ends at `BeginDrain`) and `setup`. `setup` runs RefreshManagedCheckout → BindCheckout → dirty check → hitlFallback line "No runnable AFK work" → `fmt.Fprintln(r.out)` and `Render` (the status table) → `RenderTaskList`.
- `tasks/run_selected_task.go` holds `(*implementRun).runSelectedTask`. This is the branch that runs one task.
- `tasks/attempts.go` holds `executeTaskAttempts` and `executeTaskAttemptsWithAgentFallback` (line 352). `tasks/task_result_line.go` holds the Task result line: `taskEnding`, `execErrEnding`, `printTaskResultLine` and `printTaskDone` (which replaced `printConciseSummary`).
- `tasks/attempt_cap_exhaustion.go` holds `disposeExhaustedWalk` (line 146) and `exhaustedWalkFault`.
- `tasks/executor.go` holds `RunTaskWith`, the run for one task file. It has its own `Render` and does not use `implementRun`.
- `tasks/output.go` holds the `output` type, `outputFor`, `colorEnabled` (NO_COLOR), `writerIsTerminal`, `line`, the `ansi*` constants and `statusStyle`.
- `tasks/binding/route.go` holds `RouteDrainCheckout` and `RouteDrainCheckoutResult`. `tasks/binding/binding.go` holds `AdoptCurrentCheckout`.
- `cmd/tasks.go` has these entry points:
  - `runTaskRunTaskWith` (line 1302) is the single-task entry. It passes `taskBindCheckout`.
  - `runTaskRunTasksWith` is the whole-set entry. It calls `implement.RunWholeSetWith` with `Output: stdout`.

### Which seam owns what
- **Runtime path and binding kind.** `resolveTaskSetRuntime` owns this decision. It reads `route.RuntimePath` and `route.Binding`, and the binding kind is `Binding.Provisioned`. `store/bindings.go:20` documents true as managed and false as adopted.
- **`--in-worktree`.** This path returns early from `provisionInWorktree` with a local `Binding` and does not reach `RouteDrainCheckout`.
- **Default binding.** It is recorded in `RouteDrainCheckout` step 4 (`route.go` ~line 255, `Put`), and the result sets `BoundDefault: true`. `bindCheckout`/`AdoptCurrentCheckout` runs later, in `setup`. It returns `(true, nil)` only when it records a new adopted binding, and `bindCheckout` discards that bool.
- **Invocation directory.** The value is `ResolveInput.CWD` (`tasks/resolve.go:19`). The Drain header (`drainHeader`) compares `ResolveRuntimePathWith(td, resolved.ProjectPath, "")` against the bound runtime on every route (`run.go`).
- **Writer boundary.** The `tasks/implement` package prints through `confirm.Output`. All output after that is printed by the `tasks` package through `r.out`, and the status table first prints in `setup`.
- **Single-task run.** `RunTaskWith` prints its own blank line and `Render` (`executor.go` ~line 185). It does not route through `RouteDrainCheckout`.
- **How each task ending reaches `runSelectedTask`:**

  | Ending | Path |
  |---|---|
  | Success | `printTaskDone` and then `result.Completed = append(...)`. `printTaskDone` prints "✓ <set>/<task> done", the commit detail and `printAttemptBreakdown`; `executeTaskAttempts` only carries the streams on `RunTaskResult.attemptStreams`. |
  | Interrupt | `isInterrupted(execErr)` at line 108, then `r.interruptGate`. Continue makes the loop run the same task again. |
  | Out of agents, task left open | `exhaustedWalkFault(execErr)` with `leavesTaskOpen()`, line 124. |
  | Failed | `r.failedGate`. When it handles the failure, the drain continues. When it does not, `printFailedStopAdvice` prints. Before the gate, `disposeExhaustedWalk` has already printed "✗ Out of agents for …". |
  | Quota pause | `taskResult.ProceedVerdict` together with `TimeHealing()`. The preset is `u.Preset`. When registration fails, the function returns with pause fields. When the drain recovers, the loop continues. |
  | Not time-healing | Returns `ExitSetup`. |

  The interrupt, out-of-agents-open and failed checks are only inside `if refreshErr == nil`. The interrupt and failed gates call `Render` when `!opts.Yes`.
- **Shared summary line.** `RunTaskWith` calls `printTaskDone` itself after a successful walk; it prints no other Task result line form.

### Invariants that hold
- Color is on only when the writer is an `*os.File` char device and `NO_COLOR` is empty (`output.go`). `outputFor` reuses an `*output` that is already wrapped.
- Agent stream output is not styled (`output` doc comment).
- `newImplementRun` must stop at `BeginDrain`. The deferred `finalize` is registered before `setup` (`RunTaskSetWith`).
- An existing binding wins regardless of cwd (ADR-0151). A different repository is refused (`run.go`, `CommonDir` check).
- `AdoptCurrentCheckout` never overwrites an existing binding, and it records nothing on trunk.
- The attempt-level lines must stay: "✗ Attempt %d/%d failed" (`attempts.go` ~line 317) and the `disposeExhaustedWalk` lines.

### The test shape
- `tasks/implement/run_test.go` (`setupImplementFixture`) tests `resolveTaskSetRuntime` and `ResolveTaskSetRuntimeWith` directly. `TestResolveTaskSetRuntimeBoundDrainsAtBindingFromOtherCheckout` (line 284) checks the Drain header with its "invoked from" line.
- `tasks/run_tasks_test.go` uses `setupRunTaskSetFixture` with `writeFakeAgent` and drives `RunTaskSetWith` into a `bytes.Buffer`. The buffer is not a TTY, so there is no ANSI in it.
- `tasks/run_selected_task_test.go` and `tasks/implement_run_test.go` (`newTestImplementRun`) test the `runSelectedTask` and `implementRun` methods.
- `tasks/output_test.go` covers NO_COLOR (serial, `t.Setenv`).
- These tests check the string "✓ <set>/<task> done":
  - `tasks/run_tasks_test.go:1816`
  - `tasks/timings_test.go:831`
  - `cmd/tasks_test.go:891`
  - `supervisor/supervisor_spawn_test.go:80`
- These are different lines that use similar words:
  - "✓ Completed task demo/…" is the HITL completion line (`run_tasks_test.go:679`, `1161`, `1598`).
  - "✓ Completed task set" comes from `printTaskSetSummary`.
- These tests check the string "Out of agents for": `agent_exhaustion_disposition_test.go:47` and `agent_exhaustion_fallback_test.go:91`.

### Read these in ranges
- `tasks/implement/run.go`: lines 126–270.
- `tasks/implement_run.go`: lines 82–260.
- `tasks/attempts.go` (886 lines): lines 290–340 and 352–470 (the fallback walk).
- `tasks/run_selected_task.go`: lines 48–190.
- `tasks/run_tasks.go`: lines 130–300.
- `tasks/executor.go`: lines 84–260.
- `cmd/tasks.go` (2076 lines): lines 1280–1380.
- `tasks/run_tasks_test.go` (~3.4k lines): open only near the line numbers listed above.
