---
fragment: FD35F8DC
generation: 0020
branch: master
---

+ Commit provenance trailer
  The `Pop-Task: <task-set-id>/<task-id>` line ending every **Implementation commit** — the commit→task edge history keeps for itself, once a **Planned commit subject** stopped saying which task made a commit and **Fold**'s rebase stopped the manifest's recorded SHA from surviving. Its value is the canonical **Task identifier** pair, the set's full dated directory name over the display slug, so it reads as a **Task target reference** does. It is identity rather than style, so it sits outside the **Convention stack** and no configuration suppresses it (ADR-0216). Only task commits carry it: never the dirty-runtime checkpoint commit, never a skill commit, never a human's own commit in a pop worktree.
  avoid: pop marker, commit metadata, Pop-Origin

~ Implementation commit
  A commit created by the task executor from runtime-checkout changes. After successful task completion, the executor stages all runtime changes and commits them with a task-derived subject, the agent summary as body, and a **Commit provenance trailer** as the final paragraph. The subject's scope names the Task set by its identifier without the timestamp prefix; the trailer names it in full. Task artifacts remain local and unstaged.
  was: A commit created by the task executor from runtime-checkout changes. After successful task completion, the executor stages all runtime changes and commits them with a task-derived subject and the agent summary as body. The subject's scope names the Task set by its identifier without the timestamp prefix. Task artifacts remain local and unstaged.

~ Set base commit
  The parent of a Task set's first implementation commit, recorded in the manifest at the moment that commit is made. It anchors the Verifier's commit range; when the set's recorded commit SHAs no longer exist because history was rewritten, the range falls back to locating the set's **Commit provenance trailer**, and to a human when even that is gone. Searching recorded **Planned commit subject**s was the earlier fallback and is retired — a revert or a merge commit quoting a subject matched it and anchored the range too early (ADR-0216).
  was: The parent of a Task set's first implementation commit, recorded in the manifest at the moment that commit is made. It anchors the Verifier's commit range; when the set's recorded commit SHAs no longer exist because history was rewritten, the range falls back to locating Planned commit subjects, and to a human when even those are gone.
