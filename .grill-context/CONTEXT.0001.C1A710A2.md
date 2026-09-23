---
fragment: C1A710A2
generation: 0001
branch: master
---

~ Case preparation
  The harness step that turns a historical Task set into a **Case**: it concatenates the set's planned task files into the spec, strips the human sign-off tasks and every **Remediation task** — a Remediation task carries the historical **Verifier**'s findings, which would tell a **Trial** what the first implementation missed — derives the parent commit and the **Reference diff** from provenance trailers, and drafts the **Acceptance list** for the human to approve. The human runs it and approves; nothing about a Case is generated at Trial time.
  avoid: case extraction, import, seeding
  was: The harness step that turns a historical Task set into a **Case**: it concatenates the set's task files into the spec, strips the human sign-off tasks, derives the parent commit and the **Reference diff** from provenance trailers, and drafts the **Acceptance list** for the human to approve. The human runs it and approves; nothing about a Case is generated at Trial time.
