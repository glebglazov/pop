---
fragment: 77BB76A8
generation: 0003
branch: master
---

+ Reference check
  The **Grader**'s grade of a **Case**'s **Reference diff** against its **Acceptance list**, made before the list is approved and kept with the Case. Each item the Reference fails is fixed or accepted with a reason; a change to the list, the Grader or the Reference range makes the check stale.
  avoid: calibration, reference grade, sanity check
  under: Eval

~ Grader
  The agent that scores one **Trial** blind: it sees the Case's repository at the parent commit, the Trial's diff and the **Acceptance list**, never which **Arm** produced the diff and never the **Reference diff**. In a **Reference check** it grades the Reference diff itself, the same way. It runs on a model no arm uses, so it is not the **Verifier** and shares nothing with it.
  was: The agent that scores one **Trial** blind: it sees the Case's repository at the parent commit, the Trial's diff and the **Acceptance list**, never which **Arm** produced the diff and never the **Reference diff**. It runs on a model no arm uses, so it is not the **Verifier** and shares nothing with it.
