---
fragment: 0D1583F8
generation: 0010
branch: master
---

+ Commit convention
  The commit-message grammar a repository's team writes history in — types, scopes, subject style. Planning resolves it once per Task set: the Commit format doc wins when present; otherwise it is inferred from recent history, skipping pop-generated commits so pop never learns its own accent. The resolved convention is recorded on the Task set; when nothing resolves, pop's task-derived format remains the fallback.
  avoid: commit style, house style

+ Commit format doc
  A repository document, `docs/commit-format.md`, that declares the team's Commit convention. When it exists, planning follows it instead of inferring the convention from history.
  avoid: commit template

+ Set base commit
  The parent of a Task set's first implementation commit, recorded in the manifest at the moment that commit is made. It anchors the Verifier's commit range; when the set's recorded commit SHAs no longer exist because history was rewritten, the range falls back to locating Planned commit subjects, and to a human when even those are gone.
  avoid: creation-time HEAD, set start SHA

+ Planned commit subject
  The final commit subject line rendered onto a task under the set's Commit convention — by planning for planned tasks, by the Verifier at spawn time for Remediation tasks. The executor uses it verbatim as the implementation-commit subject, so commit time stays free of agent work; the body remains the agent summary. A task without one falls back to pop's task-derived format.
  avoid: commit template, subject format string
