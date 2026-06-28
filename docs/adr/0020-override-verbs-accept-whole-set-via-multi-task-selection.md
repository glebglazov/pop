---
status: accepted
---

# Override verbs accept a whole-set target via a multi-task selection

> **Relates:** extends ADR 0015's target-shape dispatch to the manual override verbs

ADR 0015 made `implement` dispatch on **Task target reference** shape rather than on a verb: a `<task-set>/<file>.md` reference runs one task, a bare `<task-set>` drains the set. The manual override verbs — `complete`, `open`, `skip` — stayed single-task-only, *requiring* a file reference and rejecting a bare set (the "exactly two forms, file reference required" invariant in the glossary). Clearing several hand-finished tasks therefore meant typing one `<task-set>/<file>.md` command per task.

Decision: the override verbs adopt the same shape dispatch. A `<task-set>/<file>.md` reference still moves exactly one task with no prompt (unchanged). A whole-set target opens an interactive **Multi-task selection** — a checkbox UI listing every task in the set, with the verb's actionable tasks toggleable and non-candidate/at-target rows shown locked. Confirming applies the checked rows as one atomic batch; `complete` validates the batch and applies in `blocked_by` topological order so a fully-selected dependency chain just works, rejecting the batch whole if a selected task has an unsatisfied, unselected blocker. The whole-set target has two spellings, `<task-set>` and `<task-set>/`, which resolve identically; the trailing slash exists so shell completion can drill set→file (`NoSpace` directive) without the operator deleting a separator, and stopping at `<task-set>/` is a legitimate whole-set target.

## Considered options

- **A `--all` / `--yes` flag for batch override** — rejected for v1; a flag that means "operate on every task" is a second, non-interactive way to express the same intent, and it silently mass-mutates state in scripts. The feature is deliberately an interactive convenience first. A whole-set target with no interactive TTY is **rejected** with a pointer to the `<task-set>/<file>.md` form rather than defaulting to "all," precisely so nothing mass-mutates without a human watching. Batch non-interactive override can be added later if a real need appears.
- **A separate command (e.g. `complete-many`)** — rejected; it reintroduces the verb-proliferation friction ADR 0015 removed, and the shape of the argument already carries the one-vs-many distinction.
- **Keep override verbs single-task-only** — rejected; the per-task command-per-task friction was the whole motivation, and shape dispatch is already the house pattern.
- **Best-effort batch apply (skip invalid, report)** for `complete` — rejected in favor of all-or-nothing; a half-applied override is a surprising state to land in, and pre-validating the whole selection keeps "I finished this chunk by hand, mark it done" coherent.

## Consequences

- The glossary's **Task target reference** invariant loosens: override verbs no longer reject a bare set, and a third input spelling (`<task-set>/`) joins as a whole-set synonym. **Task shell completion** now drills uniformly across `implement`/`complete`/`open`/`skip` with a trailing-slash + no-space candidate at the set stage.
- A new bubbletea multi-select component is needed; the existing single-select `ui.Picker` returns one selection and has no toggle binding, so it does not cover this.
- Non-interactive callers (CI, pipes, redirected output) must target one task at a time with the file-reference form; a whole-set target there is an error, not a degraded-but-silent batch.
- The underlying state transitions and progress records are unchanged — the multi-task selection only changes how many tasks are chosen at once, so the manual-override semantics from ADR 0006 (bypass the Completion sentinel, no commit, Skipped satisfies `blocked_by`) carry over per selected task.
