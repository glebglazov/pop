# An unscheduled Routine is a durable manual-only state

`pop routine new` (renamed from `add`) no longer requires `--schedule`. A Routine without a schedule is a first-class, durable state — not a transitional gap: the Queue daemon never fires it regardless of its pause bit, and it stays manual-fire-only (`pop routine fire`) indefinitely. A set schedule clears back to unscheduled with `pop routine edit <id> --schedule ""` (and an empty submit in the dashboard's edit-schedule modal), mirroring the agent/effort modal's clear-to-unset semantics. Surfaces render the absent schedule as `manual`.

The alternative — keeping the schedule required and treating "paused forever" as the manual-only idiom — was rejected: cadence is exactly the kind of thing the Routine refinement session should settle in conversation (the session's briefing now states the schedule is optional and directs the agent to set one via `pop routine edit <id> --schedule` when the human wants it), and conflating "temporarily disabled" with "never scheduled" overloads the pause bit.

Two boundary rules keep the model coherent:

- **Absence is handled before the parser.** The schedule grammar ([ADR-0133](0133-routine-schedules-are-one-clause-production-over-a-step-mask-slot.md)) is untouched — `ParseSchedule` still rejects an empty expression. An unscheduled Routine has no schedule field to parse; manifest load accepts the absence instead of erroring.
- **`changed` pause requires an anchor.** Editing a run-affecting input pauses with reason `changed` ([ADR-0128](0128-any-failure-or-run-affecting-change-pauses-a-routine.md)) only once the Routine has fired at least once; a never-fired Routine keeps reason `created` through edits — `changed` means "drift since runs existed", and a never-fired Routine has nothing to drift from. This unblocks the created→refine→set-schedule flow without a misleading reason flip. The run-affecting fingerprint already omits unset fields, so unscheduled Routines fingerprint cleanly.

Amends the edges of [ADR-0124](0124-routines-are-created-paused-and-anchored-by-a-manual-first-fire.md) (created-paused discipline unchanged; the daemon's fire predicate gains "scheduled") and ADR-0128 (the anchor precondition above).
