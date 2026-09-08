---
status: accepted
relates: "stands on the Spend lens and Captured run of [ADR-0165](0165-stream-shape-capabilities-are-declared-and-fixture-backed.md) and the Refine and verify phases as [ADR-0252](0252-refine-fixes-in-place-before-the-verify-phase.md) placed them; it adds one exported seam to the tasks package and no command"
---

# The eval harness compares a bare agent with pop on spend and graded quality

## Context

Pop's promise is that its machinery — planning into tasks, a drain with retries,
Refine, Agent verification and remediation — buys better work on big tasks than
an agent CLI run on its own, at a token cost that is worth paying. Nothing in
the repository can test that claim. The **Spend lens** measures the cost half,
but only for runs pop itself invoked under an **Agent adapter**: an agent run
started by hand files no **Captured run**, so it has no spend. The quality half
does not exist at all — the **Verifier** is a pop component and cannot sit
outside a pop-versus-not-pop comparison.

The work store on this machine holds 137 pop Task sets and 22 for a Swift
application repository, each with provenance trailers on its commits, most
with a verify PASS, almost none with a `spec.md` (four and two respectively),
and almost all ending in one human sign-off task. Big sets cost 15M to 55M
tokens, 200 to 650 turns and about two agent hours; the three largest cost
90M plus. That is the corpus and the price list this design was fitted to.

## Decision

**1. Two arms first, ablation designed in.** The **Eval** runs each **Case**
through a **Bare arm** and a **Pop arm**. **Ablation arm**s — Refine off, Verify
off, max tries 1, implementation convention not inlined, Explore off — and a
model-swap arm are declared as arm files from the start and run by nobody
until the two-arm result is stable. The two-arm milestone is two Cases, one
repeat, so the harness is proven on four **Trial**s before thirty are paid for.

**2. The Bare arm is one headless invocation, captured through a seam in
`tasks/`.** Same **Agent preset** and model as the Pop arm, the Case's spec as
the prompt behind a short fixed preamble that names the repository, the
completion expectation, the no-commit rule and the Case's gate commands. No
Refine, no Verifier, no retry, no planning. The model is held constant so pop's
machinery is the only variable. Its usage is captured by a new exported
function in the tasks package that runs one headless invocation through the
preset's **Usage extraction rule** and writes a Captured run pair into a
directory the caller names. A standalone stream parser was rejected: the
extraction rule's accumulation semantics differ per adapter, and a second copy
would drift silently and produce a plausible wrong number. A one-task Task set
with every step disabled was rejected too: it would still wrap the spec in
pop's implement prompt, and the turn count — which is where pop's promise on
big tasks shows — would not be a bare agent's.

**3. The Pop arm is the shipped binary as a subprocess under its own data
directory.** The harness sets `XDG_DATA_HOME` to a directory of its own, so a
Trial's Task set, Captured runs and verdict cache never reach the human's Work
dashboard or Spend lens, and a Trial reproduces from a clean directory. The
arm's configuration is committed with the harness: Verify on, Refine on,
implementation convention inlined, max tries 3, no turn cap. Human sign-off
tasks are stripped from the Case's set, so a Pop-arm Trial ends at Done,
Verify-failed or Failed with Verify still firing on the Done set.

**4. A Case is prepared from a historical set, and the reference code is never
the standard.** **Case preparation** concatenates the set's task files in
manifest order into the spec both arms receive — identical words, the Pop arm
additionally getting them split into tasks, an asymmetry recorded in the
harness README because planning is not yet a headless captured step. The
parent commit and **Reference diff** come from the provenance trailers. An
agent drafts the **Acceptance list** from the task bodies and the reference's
*behaviour*; the human edits and approves it before any arm runs. The reference
diff's code is graded against by nothing: correct behaviour and good code are
separate questions, and a reference can have the first without the second.

**5. Objective gates first, then one blind Grader on another model.** The
Case's build and test commands run on the Trial's final tree; a failure scores
zero and skips grading. Scope — files changed outside the Case's stated area —
is a flag the Grader sees, not a fail. The **Grader** runs on the codex preset
(a model no arm uses), sees the repository at the parent commit, the Trial's
diff and the Acceptance list, and never learns which arm produced the diff. It
grades absolutely per Trial: each list item met or not met with a reason, and
a five-point quality score against the repository's own documented standard at
the parent commit plus general engineering judgment. Pop's resolved
implementation convention was rejected as the yardstick because it is the
Refiner's own standard and would tilt the grade toward pop. Grader spend is
recorded and charged to no arm.

**6. Trials are files, invalid ones never count.** Every Trial writes a **Trial
record** — case, arm, repeat, model, timing, the Spend lens JSON, gate outcomes,
scores — and its final diff as a patch beside it, all committed, so a Trial can
be re-graded without being re-run. A rollup command prints, per Case and Arm,
median and spread of tokens, notional cost, turns, peak input and wall-clock
beside median list ratio and quality score, with counts of gate failures,
timeouts and **Invalid Trial**s. The **Trial ceiling** is a harness flag
defaulting to four hours; a quota pause or crash is an Invalid Trial, rerun
once and excluded.

**7. It lives in `eval/` at the repository root and is not a pop command.** A
Go main run with `go run`, importing the tasks package for the capture seam,
with cases, arm files, the isolated data directory and results beside it. A
subcommand would fix an interface before the eval has taught us what it
measures. Corpus: four Cases from pop covering a multi-file feature, a bug
with a reproduction, a refactor on one seam and a docs-or-convention change,
plus one multi-file feature from `vibe-coding-done-right`, chosen because it
is a personal repository of a different language and shape, so pop's large
glossary advantage does not carry. Case count and repeats are flags; five
Cases are prepared, and the human runs the Trials.

## Consequences

- The tasks package gains one exported function with no caller inside pop.
  That is deliberate: the eval must reach the real extraction rule, and the
  alternative is a second implementation of it.
- Thirty Trials at the middle band is roughly a thousand dollars of notional
  cost and sixty agent hours per arm pair. Wall-clock, not money, is the
  binding constraint, which is why case size is fixed at the middle band.
- The model may know pop's later state. A Case runs at the parent commit in a
  detached checkout, which removes later files from reach but not from the
  model's training; the second repository is the check on that.
- The Pop arm receives pop's task split for free. Until a headless planning
  step exists and is captured, the eval understates the Pop arm's cost by
  planning and is read with that caveat.
