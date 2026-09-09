---
fragment: 1E7AB3D5
generation: 0059
branch: master
---

+ Trial
  One Arm's attempt at one Case at one repeat number — the unit the Eval
  records, grades and rolls up.
  avoid: Run, experiment, sample
  under: Eval

+ Trial outcome
  The closed set of ways a Trial ends, read by the Matrix's retry rule and by
  the Rollup: Completed, Timed out, Invalid, and Lost.
  avoid: Trial status, trial result
  under: Eval

+ Lost Trial
  A Trial whose Arm invocation finished and was paid for but whose final tree
  the harness could not read. Its spend counts and its quality does not.
  avoid: Failed Trial, Invalid Trial
  under: Eval

+ Eval work root
  The directory outside every repository where the Eval clones a Trial's
  repository and holds its scratch. A Trial's directory under it is deleted
  once that Trial's record, patch, Captured runs and reports are saved.
  avoid: Temp directory, scratch directory, eval/work
  under: Eval
