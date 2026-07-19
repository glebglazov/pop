---
fragment: 5eb54a57
generation: 0031
branch: master (wayfinder-integration grilling)
---

+ Wayfinding
  The search phase of a large effort: resolving decision tickets one at a time until the way to a destination is clear. Produces decisions, not deliverables; implementation happens in Task sets a Map spawns. Driven by the wayfinder skill (forked from mattpocock/skills).
  avoid: exploration, discovery (code term), planning effort, effort (that is the task strength knob)
  under: Wayfinder

+ Map
  The canonical artifact of one Wayfinding effort: a folder holding `map.md` (destination, notes, decisions-so-far index, fog, out-of-scope) plus its Decision tickets. A first-class concept beside Task sets, not a Task set kind — it never registers, never drains, and its membership grows and shrinks as fog graduates. Stored per-repository in Task storage under a `wayfinder/` sibling of `tasks/`; a Map exists because its folder exists.
  avoid: wayfinder task set, plan, chart
  under: Wayfinder

+ Decision ticket
  One unit of a Map: a question whose resolution is a decision, recorded as `issues/NN-<slug>.md` with `Type:` (research/prototype/grilling/task), `Status:`, and `Blocked by:` lines, its answer appended under `## Answer` on resolution. Distinct from a task: no acceptance criteria, no agent commit, and a claimed state exists (persisted in_progress stays malformed for tasks).
  avoid: task (the Task-set unit), issue, question file
  under: Wayfinder

+ Map status
  The `Status:` line in `map.md` — `active` (default), `done` (way found; skill writes it at handoff), or `abandoned` (closed without reaching the destination). Declared, not derived: fog is prose, so "way is clear" is a judgment the session records. Orthogonal to a pop-side reversible Archive flag (same shape as Task-set Archive) that hides old Maps from default views without deleting; deletion stays manual.
  avoid: map state, derived map status
  under: Wayfinder

+ Work dashboard
  The unified `pop work dashboard` TUI: one machine-global table of what you are doing or planning, interleaving Task set rows (unchanged behaviour and keys) with Map rows per project. On a Map row `i` spawns an attended wayfinder session for the next frontier ticket (new window in the repo's tmux session, named after the Map); Enter/`l` opens the Map detail view. Subsumes the Queue dashboard; `pop queue dashboard` stays a hidden compatibility alias. The Queue daemon is untouched — Maps are invisible to it.
  avoid: queue dashboard, work board, working (that is the pane status)
  under: Wayfinder

~ Queue dashboard
  Retired name for the Work dashboard — the surface was renamed when Wayfinding Maps joined Task sets on one table; `pop queue dashboard` survives only as a hidden alias. The Queue concept itself (daemon, status, log — the scheduling concern) keeps its name and scope.
  was: The interactive `pop queue dashboard` TUI — the primary hands-on surface for starting and managing Queue work, sibling to the Project picker and Worktree picker. (full entry: machine-global task-set table with drain/bind/auto-drain keys)

+ Map detail view
  The drill-down entered with Enter/`l` from a Work dashboard Map row — mirror of the Task set detail view: the Map's Decision tickets with the frontier highlighted; `i`/Enter on a frontier ticket spawns an attended wayfinder session for that specific ticket.
  avoid: ticket list, map inspector
  under: Wayfinder

+ Spawned set
  A Task set created from a Map's resolved decisions (via to-prd/to-tasks) once the way — or an early-splittable chunk — is clear. The forward link between the two concepts: the Map records the ids of sets it spawned; a spawned set's prd.md records its source Map. One Map may spawn many sets.
  avoid: child set, output set
  under: Wayfinder
