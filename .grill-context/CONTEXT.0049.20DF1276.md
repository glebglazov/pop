---
fragment: 20DF1276
generation: 0049
branch: master
---

+ Exploration report
  The single document an **Explore phase** writes for a **Task set**, recording the code as
  found — where things live, which seam owns what — so every task in the set builds against
  one picture instead of each attempt re-deriving its own. A **Task artifact** beside the
  set's `spec.md`, and the sibling of the **Refine report** and the **Verify report**: it is
  what exists, where the spec is what was decided and the **Progress record** is what
  happened. Single-writer — the phase owns it — with one correction duty on a builder that
  *falsifies* a recorded line (a moved file, a retired seam): it fixes that line and nothing
  else, because a false map is worse than no map, while a journal of the set's own progress
  duplicates the Progress record and drags the document toward a scratch pad. It does not
  carry the **Prior-art check**: ADR-0259 refused a further rank of prose as the answer to
  that gap, and the check's trigger is mechanism a builder is about to write, unknowable
  before the attempt.
  avoid: research doc, findings, exploration notes, map, orientation
  under: Tasks

+ Explore phase
  The drain step that writes a set's **Exploration report**, joining implement, verify and
  refine in the phase vocabulary. It runs once, at the head of a drain, and only when the
  report is absent — a set that already has one reuses it, no freshness comparison. Unlike
  every other agent phase it is a *gate*: implementation waits for it, and a pass that
  cannot produce the report parks the set, because a set whose author judged the tasks
  interrelated is one whose builders must not start from separate maps. Config-gated like
  the **Refine** step, defaulting on — the first agent phase that runs unasked — but the
  trigger is the author's: an **Explore directive** the set carries, with no task-count
  floor, since a count is pop guessing how interrelated a set's code is and pop computes
  nothing.
  avoid: research phase, exploration pass, discovery run
  under: Tasks

+ Exploration mark
  Whether a **Task set** that asked for exploration has its **Exploration report**, carried
  beside its status as the **Refine mark** and **Verification mark** are, with the reason
  (not yet run, pass failed) as detail beside the mark rather than a value inside it. Where
  the Refine mark deliberately gates nothing, this one reports a park: a set that asked for
  exploration and has none is not drained, so the mark is the human's account of why the
  work stopped rather than a note beside work that went ahead.
  avoid: exploration status, research badge, stale-doc warning
  under: Tasks

+ Explore directive
  The set-level `"explore": true` key by which a **Task set**'s author states that the set
  needs an **Exploration report** — set when two or more AFK tasks touch the same seam and
  a later one depends on how an earlier one shapes it, never on a set's size or on the area
  being unfamiliar. It is the participation trigger, so it sits outside the **Agent
  directive** object family whose invariant is that a directive steers how a phase runs and
  never opts a set in; an optional `"explorer"` object beside it steers agents and effort as
  `"verifier"` and `"refiner"` do. A set carrying no directive never explores, which is why
  a config default of on costs nothing on the sets that predate it.
  avoid: explore_needed, exploration flag, explore opt-in
  under: Tasks
