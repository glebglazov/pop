---
status: accepted
relates: "keeps the commit-safety guarantee `newRunPlan` was built for; applies the validation law of [ADR-0054](0054-config-validation-is-caller-scoped.md) to a re-read; generalises the gate menu's own reload in [ADR-0264](0264-an-attended-launch-surface-writes-the-agent-override-it-renders.md) decision 6; the skip reasons a stale gate hides are the subject of a separate task"
---

# A drain re-reads its configuration every turn and freezes only what it settled at start

## Context

A drain loaded pop's configuration once, at start, and every later phase read
that snapshot. Enabling `[work.refine]` while a long unattended drain was
running therefore did nothing until the next drain, and did it silently: a
`disk-census` drain ran for hours with Refine enabled on disk and never ran it.
The gate menu already re-reads the merged config after an attended pick and
keeps the config it had when the read fails (ADR-0264 decision 6), but that
re-read is local to the open gate; the drain loop kept its snapshot.

## Decision

1. **The drain re-reads its configuration at the top of every Drain turn**,
   on the same beat as the manifest refresh. A change applies at the next turn.
   There is no file watch: nothing in the loop acts between two turns except a
   running agent, which no watcher may interrupt, so a watch could not apply a
   change earlier than the next turn does.
2. **The snapshot goes live; the plan stays frozen.** Every setting a phase reads
   at its point of use — the Refine, Verify and Explore switches, the Verifier's
   and Refiner's agent lists and efforts, max-tries and retry delays for the next
   task, what a gate menu renders — reads the fresh snapshot. What `newRunPlan`
   resolves eagerly holds for the whole drain: the implementing agent list and
   first preset, agent output mode, dirty-runtime strategy, commit overrides,
   quota retry-after, and the effort validation. That is the line the run plan
   already drew, and it keeps the guarantee it was drawn for: a malformed
   `[work.implement.git]` fails before any commit could happen, and can never
   appear after commits have happened.
3. **A re-read that fails to load holds the previous turn's configuration** and
   the drain keeps going. Only an unparseable file can fail a load (ADR-0054); a
   renamed key remains a finding the consuming getter judges, as at start. The
   drain prints one line when the hold begins and one when a later re-read
   succeeds, never one per turn. A configuration file that has disappeared is
   not a failure: it reads as "no configuration", exactly as at start.
4. **The single-task path is unchanged.** `pop tasks implement <file>` has no
   loop and no phases, so it has no turn to re-read at.

## Considered Options

- **Re-read only the three phase switches.** Rejected: it fixes the reported
  symptom and leaves the same staleness one setting away — a Verifier agent list
  changed mid-drain would still be ignored, for no reason the code can state.
- **Re-resolve the whole plan each turn.** Rejected: an eager validation that
  can fail mid-drain needs a policy for every field (a commit-override error
  after commits already landed), and the Agent fallback list would change under
  an in-progress walk. Adding an implementing agent mid-drain is covered by
  interrupting and restarting.
- **Stop the drain on a failed re-read.** Rejected: a half-saved edit would end
  hours of unattended work. At start the drain has not begun, so failing is
  free; mid-drain it is not.

## Consequences

- "When does my change take effect" has a one-sentence answer: at the next Drain
  turn. A change to a frozen setting has one too: at the next drain.
- Making a skipped step's reason observable stays a separate task. This
  decision removes the *stale* reason for a silent skip, not the silence.
