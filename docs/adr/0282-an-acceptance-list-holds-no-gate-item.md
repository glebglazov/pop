---
status: accepted
relates: "amends the Acceptance-list drafting of [ADR-0268](0268-the-eval-harness-compares-a-bare-agent-with-pop-on-spend-and-graded-quality.md); the gates it leaves to are those of [ADR-0278](0278-an-objective-gate-the-parent-tree-fails-does-not-score-a-trial.md)"
---

# An Acceptance list holds no gate item

## Context

Each drafted Acceptance list ended with an item that the build and the tests
pass, copied from the task bodies. The Grader never sees the gate results, and
a Trial reaches the Grader only after its gates pass. When the Grader changed
from Codex gpt-5.5 to gpt-6-astra, the same `detail-keymap` Trial got that
item MET from the first and NOT_MET from the second ("the supplied diff has no
verification result"). The item measured the Grader, not the Arm.

## Decision

An Acceptance list records no item that the build, the vet or the tests pass.
The Objective gates own that check, and a gate failure already has its own
grade status. The drafting prompt says so, and the item is removed from the
four pop Cases' lists.

## Consequences

- Scores from before this change count one more item than the scores after
  it, so a Case is graded again before its scores are compared.
- A behaviour that a test proves is still an item, written as the behaviour,
  not as "the tests pass".
