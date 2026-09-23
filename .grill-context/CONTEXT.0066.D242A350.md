---
fragment: D242A350
generation: 0066
branch: master (eval: baseline gates, ADR-0278)
---

+ Baseline gate
  An **Objective gate** as it runs on the **Case**'s untouched parent commit,
  before the same gate judges a **Trial**'s final tree. When a baseline gate
  fails, the Trial gets no quality score and no **Grader** runs, because the
  failure belongs to the parent and not to the **Arm**.
  avoid: parent gate, pre-check, baseline check
  under: Eval
