# Acceptance list

Status: approved

1. A whole-set `pop tasks implement` run prints, as its very first output line and before the status table, a Drain header naming the Task set identifier and the resolved Runtime path; when the set has a Worktree binding, the header also names the binding kind (managed or adopted).
2. The Drain header prints unconditionally on every whole-set run, including when the run is invoked from inside the bound checkout.
3. When the invocation directory differs from the Runtime path, the header adds a follow-up "invoked from <cwd>" line naming the invocation directory, and that line is omitted when the two directories are the same.
4. When the run just recorded a Default binding to the current checkout, the header adds a line announcing "binding recorded to current checkout".
5. The conditional "draining at bound checkout <path>" line no longer appears in any run; its information is carried by the Drain header instead.
6. A single-task file run (`<set>/<file>.md`) prints no Drain header and instead prints a line stating it runs in the current checkout, naming that path.
7. In a whole-set drain, a task that completes prints exactly one green Task result line `✓ <set>/<task> done`.
8. The implementation-commit detail (commit sha or verified no-op) still prints, beneath the green result line, not above it.
9. In a whole-set drain, the old success-only "✓ Completed <set>/<task>" line no longer prints, and no task prints two Task result lines for one ending.
10. In a whole-set drain, a task that fails after all retries prints exactly one red Task result line `✗ <set>/<task> failed`, and the task ends failed.
11. In a whole-set drain, a task whose agent list is exhausted prints exactly one red Task result line `✗ <set>/<task> out of agents (left open)` instead of the `failed` Task result line, and the task stays open.
12. In a whole-set drain, a task parked by a quota pause prints a yellow line `◌ <set>/<task> quota-paused (<preset>)` naming the active agent preset.
13. In a whole-set drain, an interrupted task prints a yellow line `◌ <set>/<task> interrupted`.
14. When a set ends Blocked or Deferred, the drain reports it only through the set's terminal status and the end-of-run summary; neither status prints a Task result line, while a task that did run in that drain still prints its own result line.
15. Per-attempt narration (`✗ Attempt N/M failed`), the attempt breakdown, and the exhausted-walk narration (`✗ Out of agents for <set>/<task> after N attempts`) still print alongside the new per-task result line.
16. Drain header lines and Task result lines print in full when output is redirected to a non-TTY, carrying no ANSI escape sequences.
17. On a TTY the header and result lines carry color, and setting NO_COLOR suppresses that color on a TTY.
18. The Work journal gains no new rows for the header or result lines; the change is stdout-only.
