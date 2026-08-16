---
fragment: 19494D71
generation: 0018
branch: master
---

+ Code review
  A distinct **Drain** step that judges how well a **Task set**'s accumulated changeset adheres to the coding standards the human and the repository declare — not whether it met its acceptance criteria, which is **Agent verification**'s question, and not whether it works, which nothing in this step tests. Set-scoped, performed by a fresh **Reviewer**, gated by `[work.review].enabled` and off by default. It runs *after* the verify phase and before the terminal switch, so the document it leaves describes the tree the human is about to sign off on rather than one a **Remediation task** is about to move. It is a step and not a task because re-review is a normal act and a task cannot be re-run without being re-created (ADR-0214, overruling ADR-0207). It reaches no verdict, gates nothing, and spawns no work: its whole output is the **Review artifact**, and what happens next is the human's to decide.
  avoid: QA, verification, review task, lint step, code-review task

+ Reviewer
  The agent that performs **Code review**, running in a fresh context and chosen independently of the implementing agents so it does not review its own work — the same independence rule the **Verifier** holds, for the same reason. Its prompt is pop's own review instruction, the previous **Review artifact** if one exists, and the resolved `code-review` **Convention kind**; so what counts as good code here is prose in the **Convention stack**, never pop configuration. Unlike the Verifier it is expected to read the changed files itself: it is given the commit range and the **Work diff view** for orientation only, because naming, structure and idiom cannot be judged from a `--stat` table.
  avoid: code reviewer, critic, linter

+ Review artifact
  The single living document **Code review** maintains for a **Task set** — the standing assessment of its changeset against the resolved coding standards. Each review supersedes rather than appends: the **Reviewer** reads the previous document and writes the current one, prior documents stay under the set's `reviews/` directory, and every reader takes the latest by timestamp. It is the step's only output and its only route to a human, who may turn any of it into work or ignore it entirely. Being a **Task artifact** it never enters an **Implementation commit**; putting it in a PR is an explicit human act on `--show` output. Humans meet it as a pointer at the **HITL gate prompt** and in the **Task set detail view**, and an **Assist session**'s agent discovers it because the **Assist prompt** names its path.
  avoid: review report, findings file, review notes, review verdict

+ Review episode
  One contiguous stretch during which a **Task set**'s **Code review** stands as current, mirroring the **Verification episode**: reviewing disarms automatic re-review, and the episode ends when the done-AFK work composition changes, at which point the next arrival at AFK quiescence reviews afresh. Because Code review spawns no work of its own it cannot re-arm itself, so it needs none of the carve-outs a Verification episode carries. It constrains only automatic review: `pop tasks review <set>` runs one by hand at any time, on any set with a done AFK task and a non-empty commit range.
  avoid: review generation, review cycle
