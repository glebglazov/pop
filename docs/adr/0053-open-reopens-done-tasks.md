# Open reopens Done tasks, not just Failed and Skipped

Status: accepted (extends [ADR-0006](0006-manual-issue-state-overrides.md) manual state overrides; inverse of `complete` per the task state machine)

`pop tasks open` previously reset only Failed and Skipped tasks back to Open; a Done task was inert in the picker (`·`) and rejected by the file-reference form. There was no way to undo a completion — most painfully, a human-in-the-loop task marked Done by a premature `pop tasks complete` could not be returned to "awaiting verification." We make `open` accept **any non-Open task — Failed, Skipped, or Done — of any type**, so it is the clean inverse of `complete` and adds the `done → open` edge to the task state machine.

## Considered options

- **Allow Done of any type (chosen).** `open` is already type-blind for Failed/Skipped, and reopening a Done AFK task to re-run it is as legitimate as retrying a Failed one. A single uniform rule ("any non-Open task reopens") is the easiest to explain and keeps `open`/`complete` mirror images.
- **Restrict to Done HITL tasks only.** Matches the motivating use case exactly, but strands the equally-valid Done-AFK reopen and forces a per-type special case that's hard to justify ("why can I reopen a done HITL but not a done AFK?"). Rejected.
- **A confirmation prompt or `--force` on the Done case.** Rejected for symmetry: `complete` (the exact inverse) is deliberately unprompted, the file-reference form is already an explicit targeted intent, and reopening a *Failed* AFK task already re-arms draining with no prompt. Guarding only Done would be inconsistent. The whole-set picker stays manual-checkbox with **no row pre-checked**, so each reopen is a deliberate selection.

## Consequences

- Reopening a Done task flips the derived **Task set status** out of DONE. For a Done **AFK** task it becomes eligible again, so a later `implement` — or the **queue daemon** in an `auto_drain` set — re-fires an agent on it automatically. This auto-redrain is the accepted price of uniformity, not a bug.
- No state-file or manifest format change: the transition reuses the existing RESET progress record (`reset <set>/<task> to open (was done)`) and clears any recorded attempt count.
- The status table still prints copy-paste `open` hints only for Failed tasks; Done and Skipped tasks are reopenable but never advertised there, so the common case (most tasks end Done) stays uncluttered.
- Archived sets are still rejected before any reopen (ADR-0026), unchanged.
