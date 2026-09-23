---
status: accepted
relates: "found while repairing the Case parents that [ADR-0278](0278-an-objective-gate-the-parent-tree-fails-does-not-score-a-trial.md) makes grading depend on"
---

# A time limit in a test only stops a hang, it is never the assertion

## Context

`go test ./...` pushes the load average to about 20 on 12 CPUs. At that
load, tests failed that no change had touched. Some asserted that a run took
less than a few seconds, to show that no retry delay occurred. Others waited
2–5 s for an event that must come, such as a registered Recovery waiter, or
killed an agent shim after 100 ms, before the shim had recorded its call. An
eval Trial runs these gates, so a correct Trial got `baseline_failed` or
failed its own gates for tests it never touched.

## Decision

**1. A test asserts the outcome, not the time it took.** Where a test must
show that no retry delay occurred, it records the delays the run passed to the
retry-wait seam and asserts that there are none. Where it must show that an
attempt timed out, it reads the attempt's Captured run or the error, not a
file the killed process writes.

**2. A wait for an event that must come uses one hang guard.** Each package's
tests hold one `hangGuard` duration of a minute. It is sized for a loaded
machine running the whole tree, not for the event, so a correct run never
reaches it; it only stops a broken run from hanging the package.

**3. A deadline that the work under test must finish inside is sized for
load.** When a test needs a short deadline for one attempt and the same
deadline applies to attempts that must finish, the deadline is set for the
attempts that must finish, and the test pays that time.

## Considered Options

- **Run the gate with less parallelism (`go test -p 4 ./...`).** Rejected: it
  makes a failure less likely but does not remove it, and it changes the gate
  commands that the Bare prompt quotes.
- **Raise each time limit to a larger number.** Rejected as the rule: a larger
  limit that is still the assertion fails again on a slower machine.

## Consequences

- A wall-clock failure means a hang, not a slow machine.
- A test that waits out a deadline on purpose costs that time on every run.
