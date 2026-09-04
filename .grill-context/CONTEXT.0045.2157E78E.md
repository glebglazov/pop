---
fragment: 2157E78E
generation: 0045
branch: master
---

+ AFK standstill
  The point in a **Drain** where a **Task set**'s AFK work has stopped — every AFK task
  either done or held behind a gate — which is when **Refine** and **Agent verification**
  fire. Resolved by `AFKStandstill(m)`, deliberately a second predicate beside
  `TerminalStatus` rather than a widening of it: the terminal zone still governs verdict
  gating and the **Human completion** bit's lifetime, and those must not move. A
  standstill is not the terminal zone — a set **BLOCKED** behind a mid-ladder HITL gate is
  at one, its done-AFK work complete and a human about to sign off on it — and not
  exhaustion either, since the gated AFK tasks still exist and will resume. An
  interleaved ladder reaches one standstill per gate, of which only the last is terminal.
  DEFERRED is excluded by construction: a deferral is the human saying "not now".
  avoid: AFK quiescence, AFK exhaustion, terminal zone, drain tail, judgment point

~ Refine
  The **Drain** step, formerly Code review, that holds a **Task set**'s
  accumulated changeset to the resolved **Implementation convention**: a fresh
  **Refiner** researches the changeset, fixes in place what its licence allows,
  and writes the **Refine report**. Its procedure is pop's own and not a
  **Convention kind** (ADR-0247) — a repository steers it with an **Overlay**
  rather than replacing it, while what counts as a problem stays entirely the
  repository's, in the convention. It runs at every **AFK standstill** *before* the
  verify phase — including a set BLOCKED behind a mid-ladder gate, not only a terminal
  one — so its edits are judged by the same **Agent verification** pass as the work
  they refine. Set-scoped, gated by `[work.refine].enabled` (off by
  default); it reaches no verdict and spawns no tasks — its outputs are the
  **Refine commit** and the report. Automatic Refine skips a **Human
  completion**; `pop tasks refine` runs a full pass by hand on any eligible set.
  avoid: Code review, code quality check, review step, polish, lint step, QA
  was: The **Drain** step, formerly Code review, that holds a **Task set**'s accumulated changeset to the resolved **Implementation convention**: a fresh **Refiner** researches the changeset, fixes in place what its licence allows, and writes the **Refine report**. Its procedure is pop's own and not a **Convention kind** (ADR-0247) — a repository steers it with an **Overlay** rather than replacing it, while what counts as a problem stays entirely the repository's, in the convention. It runs at AFK quiescence *before* the verify phase, so its edits are judged by the same **Agent verification** pass as the work they refine. Set-scoped, gated by `[work.refine].enabled` (off by default); it reaches no verdict and spawns no tasks — its outputs are the **Refine commit** and the report. Automatic Refine skips a **Human completion**; `pop tasks refine` runs a full pass by hand on any eligible set.

~ Agent verification
  An independent **Verifier** agent's judgment of a **Task set**'s completed AFK work.
  Its verdict scope is only the set's `done` AFK tasks — the prompt carries their bodies
  and acceptance criteria, the **Work diff view** of the accumulated work, and the
  optional co-located `spec.md`; open/not-`done` AFK tasks and HITL tasks (any status)
  are excluded so the Verifier never fails a set on work it isn't equipped to judge (a
  not-yet-run HITL sign-off is not an unmet criterion). Gated by user config, off by
  default. When enabled it fires at every **AFK standstill**: on a DONE set, and before
  *any* HITL sign-off gate the set is sitting at — the terminal one on an
  **Awaiting-approval Task set**, and equally a mid-ladder gate on a BLOCKED set with
  AFK work still behind it — so cheap agent checking always precedes expensive human
  time. That scope rule and this trigger are what make an interleaved ladder safe: each
  gate is verified against the increment done by then.
  avoid: review, QA, human verification, Completion sentinel
  was: An independent **Verifier** agent's judgment of a **Task set**'s completed AFK work. Its verdict scope is only the set's `done` AFK tasks — the prompt carries their bodies and acceptance criteria, the **Work diff view** of the accumulated work, and the optional co-located `spec.md`; open/not-`done` AFK tasks and HITL tasks (any status) are excluded so the Verifier never fails a set on work it isn't equipped to judge (a not-yet-run HITL sign-off is not an unmet criterion). Gated by user config, off by default. When enabled it fires as the tail of a **Drain**: on a DONE set, and on an **Awaiting-approval Task set** it runs *before* the terminal HITL sign-off gate — a PASS then opens that gate, so cheap agent checking precedes expensive human time.

~ Verified status resolution
  The single read-side derivation that layers **Verify verdict**s onto a manifest to
  produce a **Task set status** *and* a **Verification mark** — the shared core every
  surface routes through. Its inputs are a manifest, the set's current work SHA, and the
  current-at-SHA and latest-PASS verdicts. The two answers key on different zones. The
  *status* gates on the terminal zone only: a current PASS lets it stand, any non-PASS
  current forces VERIFY-FAILED, an older PASS immunizes against later commits (ADR-0096)
  and surfaces that PASS's SHA, and no PASS in the episode regresses to NEEDS-VERIFY —
  except on a **Human completion**, whose status stands whatever the verdict says. The
  *mark* resolves at every **AFK standstill**, so a BLOCKED set verified at a mid-ladder
  gate carries its badge like any other; a verdict pop ran and stored but showed nowhere
  would be worse than not running it. The mark derives from the verdicts alone,
  identically either way; human completion changes only whether that answer is also
  allowed to *be* the status. Read-only and side-effect free — deciding whether to *run*
  the **Verifier** belongs to the **Drain** phase.
  avoid: status gate, verdict overlay
  was: The single read-side derivation that layers **Verify verdict**s onto a manifest to produce a **Task set status** *and* a **Verification mark** — the shared core every surface routes through (`pop tasks status`, the **Work dashboard**, `pop work status`/daemon scan, and the pre-approval **Drain** phase). Its inputs are a manifest, the set's current work SHA, and two verdicts: the current-at-SHA verdict and the latest-PASS verdict. It gates only the terminal zone (a DONE or AWAITING-APPROVAL manifest status): a current PASS lets the terminal status stand, any non-PASS current forces VERIFY-FAILED, an older PASS immunizes against later commits (ADR-0096) and surfaces that PASS's SHA, and no PASS in the episode regresses to NEEDS-VERIFY — except on a **Human completion**, whose terminal status stands whatever the verdict says. The mark is derived from the verdicts alone, identically either way; human completion changes only whether that answer is also allowed to *be* the status. It is read-only and side-effect free — the decision to *run* the **Verifier** on a cache miss belongs to the **Drain** phase, not here — so it is exercised without a store or git. Callers hold the verdicts they pass in; the resolution echoes none back.

- Checkout quiescence

+ Checkout vacancy
  Formerly Checkout quiescence, renamed because one hard word carried two unrelated
  meanings and this is the one whose prose already had a plainer vocabulary: the entry
  reasons in *occupants*, so the state of having none is vacancy. The precondition for
  any **Out-of-band mutation**: no **Checkout claim** on the checkout, and no live
  **Checkout gate hold** *for the set being mutated* held by a foreign process. Asked per
  set, not per checkout — a human parked at one set's gate has no standing to block a
  disposition of a different set sharing the tree. A hold owned by the mutating process
  itself does not occupy: the human sitting at the gate is the one the hold protects, so
  their own Accept or Remediate proceeds. A refusal names the occupant *and where to
  reach it* — PID, controlling tty, and drain pane where known — because the resolution
  is almost always "answer the prompt that is still open"; when the occupant is a
  **Recovery waiter**, it also reports whether that waiter is next under **Recovery turn
  ordering**. Not to be confused with an **AFK standstill**, which is about a drain
  having no runnable work rather than about who holds the tree.
  avoid: Checkout quiescence, idle checkout, unclaimed checkout, unlocked

~ Out-of-band mutation
  A change to a **Task set**'s verdicts or manifest made from outside a drain — e.g. the
  **Accept** or **Remediate** disposition issued from the standalone CLI. Permitted only
  under **Checkout vacancy**.
  avoid: external mutation, offline edit
  was: A change to a **Task set**'s verdicts or manifest made from outside a drain — e.g. the **Accept** or **Remediate** disposition issued from the standalone CLI. Permitted only under **Checkout quiescence**.

~ Checkout gate hold
  A registration naming the task set, **Runtime path**, and holder PID + start token,
  taken when implement parks at a **Failed gate prompt**, **HITL gate prompt**, or
  **Verify-fail gate prompt** (runtime lock released per ADR-0067). It exists in **two
  scopes**, and which one applies is decided by whether the parked human's work lives in
  the tree. As **set-scoped occupancy** — the default for every gate — it is keyed on
  (**Runtime path**, task set) and occupies only *that set* for **Checkout vacancy**: it
  keeps an **Out-of-band mutation** from racing the human's own disposition of the set
  they are sitting on, and is invisible to every other set on the same checkout. As a
  **checkout-scoped Checkout claim** — a Failed-gate park with uncommitted files in the
  tree, dirtiness snapshotted at park time — it occupies the whole checkout, blocking
  admission; at most one claiming hold per **Runtime path**, an invariant the schema
  enforces. Registration never replaces a different live owner's hold for the same set,
  and release removes only the holder's own row. Liveness is the real release: a hold
  whose owner PID is dead is ignored and replaceable, so a crashed gate never wedges
  anything.
  was: A registration naming the task set, **Runtime path**, and holder PID + start token, taken when implement parks at a **Failed gate prompt**, **HITL gate prompt**, or **Verify-fail gate prompt** (runtime lock released per ADR-0067). It exists in **two scopes**, and which one applies is decided by whether the parked human's work lives in the tree. As **set-scoped occupancy** — the default for every gate — it is keyed on (**Runtime path**, task set) and occupies only *that set* for **Checkout quiescence**: it keeps an **Out-of-band mutation** from racing the human's own disposition of the set they are sitting on, and is invisible to every other set on the same checkout. As a **checkout-scoped Checkout claim** — a Failed-gate park with uncommitted files in the tree, dirtiness snapshotted at park time — it occupies the whole checkout, blocking admission; at most one claiming hold per **Runtime path**, an invariant the schema enforces. Registration never replaces a different live owner's hold for the same set, and release removes only the holder's own row. Liveness is the real release: a hold whose owner PID is dead is ignored and replaceable, so a crashed gate never wedges anything.
