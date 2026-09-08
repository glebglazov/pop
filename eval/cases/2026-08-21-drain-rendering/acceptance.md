# Acceptance list

Status: draft

1. A whole-set `pop tasks implement` run prints, as its very first output line and before the status table, a Drain header naming the Task set identifier, the resolved Runtime path, and the Worktree binding kind (managed or adopted).
2. The Drain header prints unconditionally on every whole-set run, including when the run is invoked from inside the bound checkout.
3. When the invocation directory differs from the Runtime path, the header adds a follow-up "invoked from <cwd>" line naming the invocation directory, and that line is omitted when the two directories are the same.
4. When the run just recorded a Default binding to the current checkout, the header adds a line announcing "binding recorded to current checkout".
5. The conditional "draining at bound checkout <path>" line no longer appears in any run; its information is carried by the Drain header instead.
6. A single-task file run (`<set>/<file>.md`) prints no Drain header and instead opens with a line stating it runs in the current checkout, naming that path.
7. A task that completes prints exactly one green Task result line `✓ <set>/<task> done`.
8. The implementation-commit detail (commit sha or verified no-op) still prints, beneath the green result line, not above it.
9. The old success-only "✓ Completed <set>/<task>" line no longer prints, and no task prints two terminal lines for one ending.
10. A task that fails after all retries prints exactly one red Task result line `✗ <set>/<task> failed`, and the task ends failed.
11. A task whose agent list is exhausted prints exactly one red line `✗ <set>/<task> out of agents (left open)`, does not read as failed, and the task stays open.
12. A task parked by a quota pause prints a yellow line `◌ <set>/<task> quota-paused (<preset>)` naming the active agent preset.
13. An interrupted task prints a yellow line `◌ <set>/<task> interrupted`.
14. Tasks that end Blocked or Deferred produce no Task result line, while a task that did run in the same drain still prints its own result line.
15. Per-attempt narration (`✗ Attempt N/M failed`) and the attempt breakdown still print alongside the new per-task result line.
16. Drain header lines and Task result lines print in full when output is redirected to a non-TTY, carrying no ANSI escape sequences.
17. On a TTY the header and result lines carry color, and setting NO_COLOR suppresses that color on a TTY.
18. The Work journal gains no new rows for the header or result lines; the change is stdout-only.
19. `go build ./... && go vet ./tasks/... ./cmd/... && go test ./tasks/... ./cmd/...` passes.
