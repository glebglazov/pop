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
