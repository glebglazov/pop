---
status: accepted
relates: "extends the Response contract of [ADR-0227](0227-a-role-driving-convention-is-the-prompt-body-pop-owns-only-the-frame.md) and the FINDINGS of the Verify report of [ADR-0245](0245-verification-publishes-a-verify-report.md); places a rule in the frame as [ADR-0276](0276-verification-holds-a-sets-criteria-to-its-planning-sources.md) did"
---

# The Verifier checks every clause of a criterion by running something

## Context

In an eval of five Task sets, the Verifier wrote that all criteria were met
for four of them. A Grader found one to four misses in each. Each miss was
stated in a criterion, for example "two repositories that share a project name
do not share work" and "the same refusal flashes from the detail view". The
Verifier read those criteria and wrote "met" without a test of the clause. The
one real finding of the five Verify reports came from a probe of an edge case
that the Verifier ran.

## Decision

The Verifier's envelope (`tasks/prompts/verifier.tmpl.md`) tells it:

1. Split each Acceptance criterion into its clauses: each condition, path,
   state, and named case, such as colliding keys or the other entry point.
2. For each clause, run a check in this checkout in this attempt: a test, a
   command, or a probe of the other path, state, or key. Reading the diff is
   not a check of a behavioural clause.
3. A clause with no executed check is unmet, not met. A clause whose check
   fails is unmet like any other. A clause that cannot be checked here at
   all is an unrunnable gate: NEEDS-HUMAN, never PASS.
4. FINDINGS has one line for each clause: the criterion, the clause, the
   command, and the result.
5. The Verifier deletes each probe it wrote before it replies.

The rule is in the frame, not in the shipped `verification` convention. A
repository's written convention replaces the shipped one whole, so a rule in
the shipped document would stop at the first repository that wrote its own.
The rule does not say how a repository checks work, which commands, gates, or
toolchain. That stays the convention's. It says what a PASS asserts and what
FINDINGS must hold. ADR-0227 gives those to the Response contract, which no
convention can displace.

## Considered options

- **Put the rule in `conventions/shipped/verification.md`.** Rejected: a
  repository with its own verification document would lose it, and the eval
  repository is one of those.
- **Let an unchecked clause be FIXABLE.** Rejected: a Remediation task builds
  from the same criteria. It cannot make the Verifier run a check that it did
  not run. A clause that the Verifier could check, it must check. A clause
  that nobody can check here needs a human.

## Consequences

- Verification costs more turns and tokens on each Task set, because each
  behavioural clause needs a run. The eval measures that against the misses.
- A Verify report is longer: one FINDINGS line for each clause, not one for
  each criterion.
- A Verifier that writes a probe changes the tree for a short time. The
  delete-before-reply rule keeps the checkout as the drain found it.
