---
fragment: 7d26b7b0
generation: 0052
branch: master
---

+ Yank
  Sending a picker's selected value into a target tmux pane as typed input (`--yank-target <pane>`, SendKeys without Enter) rather than writing it to the system clipboard — the project and worktree pickers' delivery mode when the caller names an origin pane. Distinct from a **Clipboard copy**: the value lands at a shell prompt ready to edit, not in a paste buffer.
  avoid: copy, paste, clipboard

+ Clipboard copy
  Writing a value to the system clipboard through the shared tmux/OSC52 helper — tmux `load-buffer` inside tmux, an OSC 52 escape to `/dev/tty` otherwise. The delivery mode every dashboard copy verb uses, because a dashboard opened in a tmux popup has no origin pane to **Yank** into.
  avoid: yank, pbcopy

+ Remediation gate blocker
  The `blocked_by` edge added from every **open** HITL task in a **Task set** to a newly spawned **Remediation task**, so the set's human gates cannot be signed off while agent remediation work is still pending. It changes no derived **Task set status** (an open remediation already makes the set READY): what it buys is that a manual **Complete task** on a gate is refused until remediation lands, and the gate's declared scope names the remediation work. HITL tasks already Done or Skipped are never rewired, and existing sets are not backfilled.
  avoid: gate dependency, approval lock

+ Verifier summary line
  An optional `SUMMARY: <one line>` in the **Verifier**'s response contract, naming in one line why remediation is needed. It becomes the spawned **Remediation task**'s title as `Remediation <cycle>: <summary>` — single line, sanitized like a human note, capped around 72 characters. A human-origin remediation has no Verifier line, so its title comes from the first line of the human's `--remediate` note under the same cap. When neither source exists the task falls back to a generic title, because an unparseable verdict is a far worse failure than a vague title. Distinct from the **Completion sentinel**'s summary, which reports post-hoc what a remediation attempt actually did.
  avoid: findings headline, verdict summary

+ Remediation review block
  The section printed on-screen at a **HITL gate prompt** and a **Verify-fail gate prompt** listing each done **Remediation task** in the set by title with its **Completion sentinel** summary — what the agent claims it fixed. It exists because the human at a gate previously saw none of this: the summaries reached only the assisting agent's **HITL assistance prompt**, never the terminal. Scoped to remediation work alone, so it never buries the gate task body the human must act on.
  avoid: completed work dump, progress log

+ Remediation history block
  The prompt section carrying every done **Remediation task** in the set — title plus its capped **Completion sentinel** summary — into two consumers: each later AFK task attempt in that set (so a second remediation never re-treads the first blind, and a reopened task knows where the **Verifier** already caught the set), and the **Verifier** prompt itself. In the Verifier prompt it is framed like the prior human note: always present when remediations exist, labelled as the implementer's unverified claims with the work diff authoritative. It is pop's one cross-task prompt channel — every other history a task attempt sees is scoped to that task alone. The terminal-facing counterpart the human reads at a gate is the **Remediation review block**.
  avoid: progress digest, prior attempt digest, self-report

+ Copy-name verb
  The `y` verb on every **Queue dashboard** level, copying the cursored row's identifier via **Clipboard copy** and always reporting a transient status confirmation. Payload follows the level: a bare **Task set identifier** on a task-set table row, the map id on a Wayfinder Map row, a **Task target reference** (`<task-set>/<file>.md`) in the **Task set detail view** and **Task text peek**, and a bare ticket id on a Map ticket. Bound both as a direct keypress and as an action-menu entry, so it is discoverable without being slow to reach.
  avoid: yank verb, copy id, clipboard verb
