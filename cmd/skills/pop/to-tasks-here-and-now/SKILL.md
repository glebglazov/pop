---
name: to-tasks-here-and-now
description: Break a plan into tasks AND bind the set to run unattended, right here — wraps to-tasks, then forces auto-drain and binds the set to the feature worktree you run it in. Use when the user wants the tasks to drain automatically in the current checkout ("plan this and let it run here", "tasks with auto-drain in this worktree", "here and now").
---

# To Tasks — Here and Now

A thin wrapper over the `to-tasks` skill. It runs the task-drafting part of
`to-tasks`, then **hard-writes** two manifest decisions so the set drains
itself, unattended, in the checkout you invoked it from:

- `auto_drain: true` — the Queue picks the set up on its own (ADR-0047).
- `worktree: { "name": "<current-worktree>" }` — pop adopts *this* checkout as
  the drain worktree. "Here" is resolved at the skill layer (this file), so the
  drain engine needs no change: you read the current worktree's operator-facing
  name and write the existing `{ "name": ... }` manifest arm (ADR-0113).

**This skill never prompts about the worktree or auto-drain.** Both keys are
decided here — they are not questions for the user. In particular, do **not**
surface `to-tasks`'s own worktree/auto-drain offer (its step 5): that skill
asks the operator to pick a worktree arm and whether to enable auto-drain, but
here both are already answered — bind the current checkout by name, always. The
only thing that can stop the flow is the guard below (an unadoptable checkout);
that is a refusal, never a menu.

Because pop excludes the trunk, its own managed worktrees, and already-bound
checkouts from what a set may adopt, this wrapper **refuses** when the current
checkout is not a plain, unbound feature worktree — rather than writing a
binding that silently won't drain. See the guard below.

## Guard — the current checkout must be an adoptable feature worktree

Run these checks **in the current checkout, before writing anything**. If any
is true, do **not** author the set. Stop and tell the user (see "If the guard
refuses").

1. **Trunk / main working tree.** Run `git rev-parse --git-dir` and
   `git rev-parse --git-common-dir`. If they resolve to the same path, this is
   the repository's main working tree (the trunk) — refuse. A trunk drain is
   never worktree-bound, so a `{ "name": <trunk> }` binding would never be
   Queue-drainable.

2. **pop-managed worktree.** Resolve `git rev-parse --show-toplevel`. If it
   lives under pop's managed-worktree root — `<pop data dir>/queue/worktrees/`
   (i.e. `${XDG_DATA_HOME:-$HOME/.local/share}/pop/queue/worktrees/…`) — this is
   a checkout pop provisioned for the Queue. Refuse: pop owns its teardown, and
   a second set must never adopt it.

3. **Already bound.** This worktree is already serving as the drain checkout for
   another set. Check `pop tasks status` (and the manifests it lists); if the
   current worktree's name is already bound to a registered set, refuse — two
   sets draining the same checkout is exactly what the adopt list excludes.

Otherwise the checkout is a plain, unbound feature worktree — proceed.

### If the guard refuses

Explain which case tripped, and offer the two ways forward — do not silently
fall back:

- **Re-run from a plain feature worktree.** Point the user at `pop worktree` to
  create/switch to an unbound feature checkout, then run this skill there.
- **Ask for a pop-managed worktree instead.** If they want unattended drain
  from the trunk or in isolation, run the plain `to-tasks` skill and choose the
  `{ "managed": true }` worktree arm — pop provisions its own isolated checkout
  on the first Queue drain. That is the safer isolated default anyway.

Never write `{ "name": <trunk> }` or `{ "name": <managed> }` to route around
the guard.

## Process

1. **Guard first.** Run the checks above. Refuse if any trips.

2. **Resolve "here".** Capture the current worktree's operator-facing name:
   `basename "$(git rev-parse --show-toplevel)"`. This is a *name*, never a
   path — the manifest is portable across machines.

3. **Run `to-tasks`'s drafting steps** to produce the vertical slices, the task
   markdown files, and `index.json`: context gathering, slicing, the task
   template, the manifest schema, the filename scheme, and keeping markdown and
   manifest in sync. **Skip `to-tasks`'s step-5 prompting about `worktree` and
   `auto_drain`** — do not read its arms to the user, do not offer a choice.
   Those two keys are decided here and written verbatim, never negotiated:

   - Always write top-level `"auto_drain": true` (this skill's whole point;
     don't ask whether to enable it).
   - Always write top-level `"worktree": { "name": "<current-worktree>" }` using
     the name from step 2. Never `{ "managed": true }` here — this skill binds
     *here*, and the guard has already established that "here" is adoptable.

   The manifest top-level therefore looks like:

   ```json
   {
     "auto_drain": true,
     "worktree": { "name": "<current-worktree>" },
     "tasks": [ ... ]
   }
   ```

4. **Register the set** exactly as `to-tasks` describes: run
   `pop tasks register <task-set-name>`, confirm it is `READY` (or `DEFERRED`
   if every open task is HITL) and not `MALFORMED`/`MISSING`, and fix and
   re-register if it reports `MALFORMED`.

5. **Tell the user** the task-set name, that it is auto-drain **bound to the
   `<current-worktree>` worktree** (so the Queue will drain it there
   unattended), its status, and how many tasks are open.

   The set drains via the **Queue**, which honours the worktree directive and
   runs it in `<current-worktree>` (ADR-0113). If the Queue supervisor is not
   already running, start it with `pop queue run`; watch progress in
   `pop queue dashboard`.

   **Do not run — or suggest — `pop tasks implement` for a here-and-now set.** A
   foreground `pop tasks implement` always rebinds to the checkout it runs in
   and **ignores** the authored directive (ADR-0072); run from any other
   checkout it silently clobbers the `{ "name": "<current-worktree>" }` binding,
   and the set then drains in the wrong worktree. The Queue is the only path
   that respects the directive — let it do the draining.
