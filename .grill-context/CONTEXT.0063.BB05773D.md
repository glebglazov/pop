---
fragment: BB05773D
generation: 0063
branch: master
---

+ Planning source
  An artifact a Task set was planned from and is answerable to — an external
  tracker item (Jira, Linear, Basecamp) or an internal one (a Map, a spec).
  Declared as a set-level list in the manifest with each entry carrying an id,
  and claimed per task the way `blocked_by` names task ids, so a source no task
  claims is planning having silently dropped one. It generalises today's inert
  singular `source_map` key, which becomes one entry among others. Where a set
  declares any, Agent verification must read them: a source it cannot reach is
  an unrunnable gate, not a pass, and parks the set at NEEDS-HUMAN.
  avoid: source ticket, task artifact, origin, upstream issue
  under: Tasks

+ Intent drift
  A divergence between a Task set's Acceptance criteria and its Planning
  sources: the contract and the intent it was derived from no longer agree.
  Directionless by design — either planning lost the source's intent, or a later
  grilling deliberately moved the design and left the source stale — and pop
  presumes neither side correct. Detected by Agent verification, it reaches
  NEEDS-HUMAN and never FIXABLE: a Remediation task spawned from the criteria
  cannot repair the criteria, and only a human can pick the repair — remediate
  the implementation, or amend the Planning source and Accept. It is the one
  thing a Verifier may fail a set for while every Acceptance criterion is met.
  avoid: business context check, spec drift, missing requirements
  under: Verification

+ Planning-sources convention
  The `planning-sources` Convention kind: where this repository's work
  originates and how an agent reads one — which trackers are in play, the tool
  or CLI that fetches an item, how a bare key resolves to a URL. Step-informing,
  so it rides as a short labelled block behind a config toggle, as the
  implementation convention does. It is deliberately not part of the
  issue-tracker convention, which answers where work is *filed*: the two are
  different systems in a team that plans in a tracker and runs in pop's Work
  store, and pop's shipped issue-tracker answer is 17KB of publish instructions
  no prompt can carry.
  avoid: source convention, tracker convention
  under: Conventions
