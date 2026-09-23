---
status: accepted
relates: "amends the grading decisions of [ADR-0268](0268-the-eval-harness-compares-a-bare-agent-with-pop-on-spend-and-graded-quality.md): a gate failure scores zero only when the parent tree passes that gate"
---

# An Objective gate the parent tree fails does not score a Trial

## Context

The first graded Trial — Case `2026-09-06-detail-keymap`, Bare arm — recorded
`gate_failed` with zero acceptance and zero quality. The patch touched only
`dashboard/`; the failing tests were in `config/`. At the Case's parent
commit those tests called the production config load, which read the grading
machine's own `config.override.toml`. The same tests fail on the untouched
parent tree, so the zero said nothing about the Arm.

Isolating the gate environment does not repair this. The parent tree was run
three ways: the real environment fails `config`; isolated XDG directories fail
`supervisor` and `tasks/drain`, whose tests expect `XDG_DATA_HOME` unset; an
isolated `HOME` fails three `cmd` tests. A historical parent is frozen, so no
environment the harness chooses makes every Case's suite pass, and a fix on
the default branch never reaches an old parent.

## Decision

**1. Grading runs the gates on the parent tree first.** Before the Trial tree,
every Objective gate runs in a fresh clone of the parent commit, in the same
environment as the Trial-tree gates. The results are kept as
`grade.baseline_gates`. The Grader still gets its own fresh parent clone,
because a gate may change the tree it runs in.

**2. A baseline failure is its own grade status.** When any gate fails on the
parent, the grade is `baseline_failed`: no Trial-tree gate runs, no score is
recorded and no Grader runs. `gate_failed` with zero scores stays for a gate
the parent passes and the Trial tree fails — the only case where the failure
is the Arm's.

**3. Case preparation refuses a Case whose gates fail on its parent.**
`prepare` runs the gates on a clone of the parent commit and writes no Case
when one fails, naming the gate. The repair is a parent tree that passes, or a
gate list that does, chosen with `--gate`.

**4. The Rollup counts baseline failures apart.** A `baseline_failed` Trial
contributes its spend and work-shape figures, like a Lost Trial, and no
quality figure. It has its own count, so it cannot read as a gate failure.

## Considered Options

- **Isolate the gate environment (fresh `HOME` and XDG directories).**
  Rejected as the fix: every isolation shape broke a different part of the
  parent's suite, because the tests themselves assumed an environment.
  Hermetic tests on the default branch remain worth doing, for the Cases cut
  from later commits.
- **Compare failing tests between the parent and the Trial tree.** Rejected:
  it needs a parser per toolchain, and a gate is an opaque shell command.
- **Check the baseline only in `prepare`.** Rejected: a Case written by hand,
  or prepared before this check, still reaches grading.

## Consequences

- Grading clones the parent one more time and runs the gates twice.
- A Case whose parent fails a gate yields no quality figures until it is
  repaired, rather than figures that blame the Arm.
- Repairing an old Case means a new parent commit — the historical parent
  plus only the changes that make its tests hermetic, the production seams
  those tests inject through included — and a Case manifest that names it.
