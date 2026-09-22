---
fragment: 46DAC060
generation: 0065
branch: master — grill on docs/research/2026-09-22-deterministic-gates-handoff.md
---

~ Objective gate
  A per-repository command that pop runs and judges by its exit status alone,
  such as build, tests or lint. The Eval harness runs it on a Trial's final
  tree; a Task-set drain runs it on the work. Its result is passed, failed or
  could not run.
  avoid: step gate, hard check, precondition, CI step
  was: A per-repository command run on a **Trial**'s final tree before grading, such as build and tests. A failed gate scores the Trial zero and skips the **Grader**; the result is still recorded.
  under: Task sets

+ Gate manifest
  A repository's list of **Objective gates**, each with a name, the points it runs at and a
  command, held as the `gates` convention. Where no rank holds one, the
  repository has no Objective gates and its work is checked by the Verifier
  alone.
  avoid: gate config, check list, gate_commands
  under: Task sets

+ Gate service
  Infrastructure an **Objective gate** needs, such as a database or a cache,
  declared in the **Gate manifest** with commands to start it, probe that it
  is ready, and stop it. When it cannot be made ready, every gate that needs
  it could not run; pop does not run their commands.
  avoid: fixture, environment, infra step
  under: Task sets

+ Convention assist
  An attended agent session that drafts or revises one convention kind at a
  rank the human chooses. For the `gates` kind it ends with a dry run of the
  **Gate manifest**, so a manifest an agent wrote is one the human has seen
  run.
  avoid: convention wizard, gate detection, convention generator
  under: Conventions
