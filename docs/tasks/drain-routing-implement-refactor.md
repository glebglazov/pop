# Task breakdown: drain routing + Implement orchestration

Five tasks, sequential. Each leaves tests green. Scope: architecture review candidates #1 + #2; queue dashboard extraction explicitly deferred.

---

## Task 1 — Move `binding/` → `tasks/binding`

**Goal:** Mechanical package move; zero behavior change.

**Work:**
- Move root `binding/` to `tasks/binding/`
- Update imports in `cmd/`, `queue/`
- Delete empty root `binding/` package

**Acceptance:**
- `make test` passes
- `bindings.json` path unchanged
- No routing logic moved yet

---

## Task 2 — `RouteDrainCheckout` + drop bindings mirror + routing fixes

**Goal:** One entry point for drain checkout resolution; remove `DaemonState.WorktreeBindings`; fix routing inconsistencies.

**Work:**
- Add `RouteDrainCheckout(req)` with policy struct (`Trigger`: ImplementForeground | QueueSpawn; `Inline`, `RuntimeOverride`, `WorktreeReady`; `OnProvisionFailure`: Fail | FallbackInline)
- Move `ResolveExecutionBasePath` from `queue/representative.go` into `tasks/binding`
- Replace:
  - `cmd/tasks.go` — `resolveTaskSetRuntimeForImplement`, `provisionImplementWorktree`
  - `queue/supervisor.go` — `prepareWorktreeDrain`
  - `queue/representative.go` — `applyBindingRouting` (call router or shared lookup)
  - `queue/dashboard.go` — inline binding writes on spawn/bind
- Remove `WorktreeBindings` from `DaemonState`; `ReadDaemonState`/`WriteDaemonState` no longer load/save bindings
- **Allowed fixes:** Queue provision forks from **Execution base** via same path as Implement (not ad-hoc `dec.scan.ProjectPath`)

**Acceptance:**
- All binding reads/writes go through `tasks/binding` store API
- No `state.WorktreeBindings` references remain
- Tests cover precedence: existing binding wins, `--inline` rejected when bound, worktree-ready default provisions, queue provision fallback
- Document any user-visible routing fix in commit message

**Depends on:** Task 1

---

## Task 3 — Bind / unbind lifecycle → `tasks/binding`

**Goal:** Binding lifecycle verbs live with the store, not `queue/`.

**Work:**
- Move `queue/bind_worktree.go` → `tasks/binding`
- Move `queue/abandon.go` (Unbind worktree) → `tasks/binding`
- Update `cmd/tasks.go` bind/unbind handlers
- Queue daemon/dashboard call `tasks/binding` (thin logging wrappers in `queue/` OK)

**Acceptance:**
- `pop tasks bind-worktree` / `unbind-worktree` behavior unchanged
- Managed vs adopted teardown rules preserved (`Provisioned` bit)
- Tests migrated from `queue/*_test.go`

**Depends on:** Task 2

---

## Task 4 — `tasks/integration` module

**Goal:** Integrate and Mergeability off queue/daemon state.

**Work:**
- Create `tasks/integration/`
- Move from `queue/`:
  - `integrate.go` (+ tests)
  - `implement_mergeability.go`, `mergeability.go` (+ tests)
- Persist **Mergeability** beside bindings (e.g. `mergeability.json`), not `DaemonState.Mergeability`
- Migration: fold legacy daemon-state mergeability on first read (mirror bindings migration pattern)
- Update callers: `cmd/tasks.go` integrate command, implement epilogue helpers, queue supervisor Done-outcome mergeability, queue dashboard `I` key, queue status/backlog views

**Acceptance:**
- `pop tasks integrate` works from new module
- Integration backlog status reads from bindings + mergeability files
- No mergeability in `state.json`
- ADR-0038 daemon path updated to `tasks/integration`

**Depends on:** Task 2 (bindings off daemon state); Task 3 optional but cleaner before integrate teardown paths settle

---

## Task 5 — `tasks/implement` + slim `cmd/tasks.go`

**Goal:** Whole-set Implement orchestration out of cmd.

**Work:**
- Create `tasks/implement/`
- Move whole-set path: route (calls `tasks/binding`) → `RunTaskSetWith` → epilogue (calls `tasks/integration` for mergeability + integrate offer)
- Internalize adopt-at-lock (`BindCheckout` hook set inside module, not from cmd)
- `cmd/tasks.go`: Cobra flags → `tasks/implement.RunWholeSet`; delete `runTaskRunTasksWith` glue, `taskRecordMergeabilityOnDone`, `offerImplementIntegration`, `tryAutoIntegrateYes`
- **Keep** `RunTask` in `tasks/executor.go` for single-task file targets

**Acceptance:**
- `cmd/tasks_test.go` implement scenarios pass (may move tests to `tasks/implement/`)
- `resolveTaskSetRuntimeForImplement` has direct unit tests
- `cmd/tasks.go` no longer imports `queue` for implement path (integrate cmd uses `tasks/integration`)

**Depends on:** Tasks 2, 4

---

## Explicitly out of scope

- Queue dashboard action-layer extraction (arch review #3)
- Monitor/dashboard deepening (arch review #5)
- Agent integration package promotion (arch review #6)
- `queue.Deps` optional func field cleanup

## Reference

- ADR: `docs/adr/20260621-1556-task-execution-modules-split-from-queue.md`
- Glossary fragment: `CONTEXT.0003.FC249732.md` (+ Drain routing, Integration backlog ownership)
