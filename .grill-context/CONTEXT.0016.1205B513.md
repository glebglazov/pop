---
fragment: 1205B513
generation: 0016
branch: master (grill: Matt Pocock skills migration + to-tasks-here-and-now)
---

+ Here-and-now
  A `to-tasks` registration mode — surfaced as the `to-tasks-here-and-now` planning skill — that authors a **Task set** and pre-seeds two manifest decisions before registering: **Auto-drain** on, and a **Worktree binding** to the checkout the skill was run in ("here"). The binding is resolved at the skill layer by reading the current worktree's operator-facing name and writing the existing `{ "name": "<worktree>" }` manifest arm, so pop's drain engine is unchanged. It **refuses** when the current checkout is not a plain, unbound feature worktree — i.e. the trunk, a pop-managed worktree, or one already bound to another set — pointing the user at `{ "managed": true }` instead, rather than writing a binding that would never drain. "Here" is the worktree; "now" is the auto-drain that lets the **Queue** pick the set up unattended.
  avoid: here-now, bind-and-drain
  under: Tasks
