---
fragment: 8C6C2E8A
generation: 0056
branch: master (grill: cost-versus-quality eval)
---

+ Eval
  The mechanism that runs a set of **Case**s through two or more **Arm**s and
  reports each arm's spend against its graded quality, so that a change to pop's
  machinery can be judged by what it costs and what it buys.
  avoid: benchmark, bench, cost report, comparison script
  under: Eval

+ Case
  One unit of eval work: a spec, an acceptance list written before any arm runs,
  and a pinned parent commit of a named repository that every arm starts from.
  avoid: scenario, test case, sample, task (the pop unit)
  under: Eval

+ Arm
  One configuration of how a **Case** is executed, held fixed across every
  **Trial** of that arm. The **Bare arm** and the **Pop arm** are the two floor
  and ceiling arms; an **Ablation arm** is pop with one step removed.
  avoid: condition, variant, mode, treatment
  under: Eval

+ Bare arm
  The **Arm** in which the same **Agent preset** and model as the **Pop arm**
  receives the **Case**'s spec as its whole prompt in one headless invocation,
  with no Refine, no Verifier, no retry and no task planning. It holds the model
  constant so pop's machinery is the only variable.
  avoid: raw agent, raw arm, vanilla agent, control, baseline
  under: Eval

+ Pop arm
  The **Arm** in which pop drives the **Case** end to end with the steps under
  test enabled. Its spend is the sum of every **Captured run** the drain filed.
  avoid: pop run, treatment arm, full pop
  under: Eval

+ Ablation arm
  A **Pop arm** with exactly one drain step or bound removed, so the difference
  against the full Pop arm is that step's cost and worth.
  avoid: variant, partial pop, knock-out
  under: Eval

+ Trial
  One **Arm**'s execution of one **Case** from the pinned parent commit to a
  final diff, graded once. Repeated Trials of the same arm and case measure
  variance. Not a pop **Drain** and not a **Captured run**: a Pop-arm Trial
  contains many Captured runs.
  avoid: run, execution, sample, attempt
  under: Eval

+ Acceptance list
  The per-**Case** list of behaviours a **Trial**'s diff must show, written and
  approved before any arm runs. Drafted by an agent from the Case's task bodies
  and the behaviour of the **Reference diff**, edited by the human; the only
  place the reference informs grading.
  avoid: rubric, checklist, expected output, spec criteria
  under: Eval

+ Reference diff
  The commits the historical Task set actually landed, found by their
  provenance trailers. It fixes the **Case**'s parent commit and lends its
  behaviour to the **Acceptance list**; its code is never a standard a Trial is
  graded against, since correct behaviour and good code are separate questions.
  avoid: etalon, golden diff, ground truth, expected diff
  under: Eval

+ Objective gate
  A per-repository command run on a **Trial**'s final tree before grading,
  such as build and tests. A failed gate scores the Trial zero and skips the
  **Grader**; the result is still recorded.
  avoid: hard check, precondition, CI step
  under: Eval

+ Grader
  The agent that scores one **Trial** blind: it sees the Case's repository at
  the parent commit, the Trial's diff and the **Acceptance list**, never which
  **Arm** produced the diff and never the **Reference diff**. It runs on a model
  no arm uses, so it is not the **Verifier** and shares nothing with it.
  avoid: verifier, judge, reviewer, evaluator agent
  under: Eval

+ Trial record
  The committed result of one **Trial**: case, arm, repeat, model, timing, the
  Spend lens JSON for the Trial, gate outcomes, the Grader's scores, and the
  final diff as a patch beside it, so a Trial can be re-graded without being
  re-run.
  avoid: result file, log, run output
  under: Eval
