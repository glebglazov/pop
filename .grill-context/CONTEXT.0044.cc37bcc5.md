---
fragment: cc37bcc5
generation: 0044
branch: master
---

+ Binding-first runtime resolution
  The rule that any command acting on a **Task set** resolves its **Runtime path** from the set's **Worktree binding** when bound, and only falls back to the **current checkout** when unbound — the same law the **Drain** already follows. It governs `pop tasks verify` (**Accept**, **Remediate**, re-run), `pop tasks status`, and the **Assist session**, so every surface reads and writes verdicts at one checkout's HEAD and cannot disagree with the **Queue dashboard**.
  avoid: cwd resolution, current-directory routing
  under: Verification

+ Assist session
  A human-in-the-loop session opened on an arbitrary **Task set** at its current derived status, without draining or re-running the **Verifier**. It presents the gate menu that status calls for — the **HITL gate prompt** for a **Human-blocked** or **Awaiting-approval Task set**, the **Verify-fail gate prompt** for a **Verify-failed Task set**, the **Failed gate prompt** for a failed one, and a generic assistance menu otherwise — so the dispositions available outside a drain are exactly those available inside one. Entered from `pop tasks assist <set>` inline in the current terminal, or from the **Queue dashboard**, which spawns a `<task-set>-assist` pane in the **pop-queue** window (one per set — an existing pane is jumped to, never twinned). It runs under **Binding-first runtime resolution**, refuses while the set's drain is live, and registers a non-claiming **Checkout gate hold** for its duration. Being a human session it requires a TTY, and refuses headless rather than degrading — a **Missing** or **Archived Task set**, or a mismatch between the **current checkout**'s repository and the set's, refuses likewise.
  avoid: HITL session, gate command, set console
  under: Task execution

+ Assist prompt
  The context loaded into an **Assist session**'s agent assistance. It identifies the **Task set** and its **Task storage** path, its derived status, the manifest listing (per-task status, type, effort, and blockers), the **Worktree binding** and **Runtime path**, recent progress, the latest **Verify verdict** findings, the task contract the agent must respect, and the operations it may perform. Task bodies are not inlined — the agent reads them from **Task storage**.
  avoid: set dump, full context load
  under: Task execution

~ Verify-fail gate prompt
  The interactive choice shown when a **Drain** or an **Assist session** reaches a **Verify-failed Task set** on a TTY — the verify counterpart of the **HITL gate prompt** and **Failed gate prompt**. It offers Accept (record an **Accepted verdict** with a note), Remediate (spawn a **Remediation task** with a note), agent assistance, open a **Runtime shell**, or exit; `0` is exit. Assistance is advisory — it reads the findings and diff and returns to the menu without dispositioning the set, matching the attended assistance the HITL and Failed gates already offer. Headless runs use `pop tasks verify <set> --accept` / `--remediate "<note>"` instead. Re-verify is not offered here — re-running the **Verifier** is a separate force action, not a response to findings.
  was: The interactive choice shown when a **Drain** reaches a **Verify-failed Task set** on a TTY — the verify counterpart of the **HITL gate prompt** and **Failed gate prompt**. It offers Accept (record an **Accepted verdict** with a note), Remediate (spawn a **Remediation task** with a note), open a **Runtime shell**, or exit; `0` is exit. Headless runs use `pop tasks verify <set> --accept` / `--remediate "<note>"` instead. Re-verify is not offered here — re-running the **Verifier** is a separate force action, not a response to findings.

~ Checkout quiescence
  The state of a checkout with no **Checkout claim** and no *foreign* **Checkout gate hold** registered. Precondition for any **Out-of-band mutation**. A hold owned by the mutating process itself does not occupy the checkout — the human sitting at a gate is the one the hold exists to protect, so their own Accept or Remediate proceeds while every other occupant still refuses. A refusal names the occupant; when the occupant is a **Recovery waiter**, it also reports whether that waiter is next under **Recovery turn ordering**.
  was: The state of a checkout with no **Checkout claim** and no **Checkout gate hold** registered. Precondition for any **Out-of-band mutation**. A refusal names the occupant; when the occupant is a **Recovery waiter**, it also reports whether that waiter is next under **Recovery turn ordering**.
