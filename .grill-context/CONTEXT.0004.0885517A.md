---
fragment: 0885517A
generation: 0004
branch: master
---

+ Ticket
  The request for a **Case**'s work as the human first wrote it, before any planning: the opening message of the session that planned the source **Task set**, with only what a **Trial** cannot use taken out and nothing added from the spec. A Case with no record of that message has no Ticket, because one written later is written with the spec in mind.
  avoid: prompt, issue, brief, summary
  under: Eval

+ Ticket arm
  A **Bare arm** that receives the **Case**'s **Ticket** in place of its spec, so its difference against the Bare arm is what planning added. It is graded against the same **Acceptance list**.
  avoid: unplanned arm, no-spec arm, raw ticket
  under: Eval

+ Name-only item
  An **Acceptance list** item that passes only on a name the planning chose, such as a status or a config key. No **Ticket** can ask for it, so the rollup also scores each **Trial** without these items; the **Grader** still grades them.
  avoid: naming item, cosmetic item
  under: Eval

~ Case
  One unit of eval work, self-contained under the harness: a manifest naming a repository clone URL, a parent commit SHA, gate commands and scope; the spec the Bare and Pop arms receive; the approved **Acceptance list**; the Pop arm's task split; and, where one exists, the approved **Ticket**. It points at no local folder, so it runs from any machine that can clone the repository.
  was: One unit of eval work, self-contained under the harness: a manifest naming a repository clone URL, a parent commit SHA, gate commands and scope; the spec both arms receive; the approved **Acceptance list**; and the Pop arm's task split. It points at no local folder, so it runs from any machine that can clone the repository.

~ Arm
  One configuration of how a **Case** is executed, held fixed across every **Trial** of that arm. The **Bare arm** and the **Pop arm** are the two floor and ceiling arms; an **Ablation arm** is pop with one step removed; a **Ticket arm** is the Bare arm with planning removed.
  was: One configuration of how a **Case** is executed, held fixed across every **Trial** of that arm. The **Bare arm** and the **Pop arm** are the two floor and ceiling arms; an **Ablation arm** is pop with one step removed.
