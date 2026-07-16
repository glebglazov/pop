---
name: to-tasks
description: Break a plan, spec, or PRD into independently-grabbable work items written as local markdown files, binding the set to the worktree you run it in. Use when the user wants to convert a plan into tasks, create implementation tickets, or break down work into actionable items. Accepts `managed` (isolated pop-owned worktree) and `auto-drain` (queue drains it unattended) arguments.
---

# To Tasks

Break a plan into independently-grabbable work items using vertical slices (tracer bullets).

## Arguments

`to-tasks` reads two optional arguments from the invocation; both default off. They are independent and may be combined.

- **`managed`** / **`isolated`** — write the **Worktree directive** as `{ "managed": true }` (pop forks its own isolated worktree from the Trunk worktree on the first Queue drain) instead of the default `{ "name": "<current-worktree>" }`.
- **`auto-drain`** / **`drain`** — also set `"auto_drain": true`, so the Queue daemon drains the set unattended. Only these literal keywords enable it — there is no "here and now" phrasing.

With no arguments, the set is bound to the **current** worktree by name and left for manual/foreground draining. `managed auto-drain` gives an isolated worktree that drains unattended — the safest unattended combo.

## Process

### 1. Gather context

Work from whatever is already in the conversation context. If the user passes a task reference (task id, URL, or path) as an argument, fetch it and read its full body.

If a PRD authored by to-prd is being broken down, it lives **co-located** in its task-set folder as `<tasks-dir>/<task-set-name>/prd.md` (ADR-0088) — not in a separate `prds/` sibling. That folder already exists (to-prd created it early, containing only `prd.md`); read that `prd.md` for context and write the task files into the **same** folder, reusing its `<task-set-name>` so the PRD and its tasks share one directory.

### 2. Explore the codebase (optional)

If you have not already explored the codebase, do so to understand the current state of the code. Use the project's domain glossary vocabulary throughout, and respect ADRs in the area you're touching.

### 3. Draft vertical slices

Break the plan into **tracer bullet** slices. Each slice is a thin vertical slice that cuts through ALL integration layers end-to-end, NOT a horizontal slice of one layer.

Slices may be 'HITL' or 'AFK'. HITL slices contain ONLY human work — verification, decisions, manual testing; the executor never runs them. AFK slices can be implemented and merged without human interaction. Prefer AFK over HITL where possible.

If a HITL slice needs an artifact built first (a report command, test data, a harness), split it: the agent work goes in an AFK slice, and the HITL slice depends on it via "Blocked by" and contains only the human steps. A HITL slice whose "What to build" describes software is mis-typed. Write the HITL body as instructions to the human — the exact steps to verify.

HITL slices have two roles, at opposite ends of a set. **Approval at the end** — the agents are done (and have already verified their own work) and the human signs off; nothing depends on it, so the set reaches `AWAITING-APPROVAL`. This is the common, expected HITL. **Setup at the bottom** — the human provisions something the agent genuinely cannot, before agents can run; AFK slices depend on it, so the set sits `BLOCKED` until you act. Create a setup HITL only when *absolutely necessary*: mainly accounts and secrets the agent can't self-issue. It is **not** for things the model can discover or do itself — devices, environment details, config it can read — so don't manufacture a setup HITL for those. A HITL in the middle is still valid (a genuine mid-flow human decision), but the set will park at `BLOCKED` mid-drain — the correct signal that real agent work waits behind a human.

Assign an `effort` to every slice using this named-signal heuristic:

- `heavy` — architectural or cross-cutting refactors, or genuinely tricky algorithms.
- `light` — large but mechanical work such as renames, codemods, config, or boilerplate.
- `standard` — everything else.

Write an explicit effort value for each task. Default to `standard` when no named signal clearly applies. Do not consult `pop tasks agents` in the default flow: effort is model-strength intent, not an agent choice.

<vertical-slice-rules>
- Each slice delivers a narrow but COMPLETE path through every layer (schema, API, UI, tests)
- A completed slice is demoable or verifiable on its own
- Prefer many thin slices over few thick ones
</vertical-slice-rules>

> **Artifacts must already be committed.** Task sets are often worked in a fresh git worktree forked from the current branch's HEAD, so any CONTEXT/ADR/code a prior session generated must already be on HEAD for the worktree to carry it. This skill does **not** commit — committing belongs to the session that produced the artifacts (e.g. `grill-with-docs` offers it at the close of a grilling session). Assume that has happened; if you spot uncommitted session artifacts, flag them, but don't commit here.

### 4. Write the work items to the local filesystem

Resolve the tasks base directory, `<tasks-dir>`, by running `pop tasks show-path` — it prints the absolute path to this repository's task storage (in pop's data dir, outside the repo tree) and creates it on demand.

For each slice, write a markdown file to the `<tasks-dir>/<task-set-name>/` directory (create the subdirectory if it doesn't exist; when breaking down a co-located PRD it already exists and holds `prd.md` — write the task files alongside it). `<task-set-name>` is `<timestamp>-<slug>`, where `<slug>` is either the source PRD slug (without its timestamp prefix) or a hyphen-delimited string summarising what you intend to do (infer from context). When a co-located `prd.md` is the source, reuse its existing folder's `<task-set-name>` rather than minting a new one. Use the following template. Write them in dependency order (blockers first) so you can reference real identifiers in the "Blocked by" field.

<naming-convention>
`<timestamp>` is a human-readable local date/time prefix so task sets sort chronologically:

- Default: `YYYY-MM-DD` (e.g. `2026-05-31`)
- If a folder with the same date and slug already exists: `YYYY-MM-DD-HHMM` (24-hour local time, e.g. `2026-05-31-2036`)

Examples: `2026-05-31-user-auth`, `2026-05-31-2036-user-auth`
</naming-convention>

<task-template>
## Parent

A reference to the parent item (if the source was an existing file, otherwise omit this section).

## What to build

A concise description of this vertical slice. Describe the end-to-end behavior, not layer-by-layer implementation.

Avoid specific file paths or code snippets — they go stale fast. Exception: if a prototype produced a snippet that encodes a decision more precisely than prose can (state machine, reducer, schema, type shape), inline it here and note briefly that it came from a prototype. Trim to the decision-rich parts — not a working demo, just the important bits.

## Type

HITL or AFK.

## Acceptance criteria

- [ ] Criterion 1
- [ ] Criterion 2
- [ ] Criterion 3

## Blocked by

- A reference to the blocking item (if any)

Or "None - can start immediately" if no blockers.

</task-template>

Use a consistent filename scheme: `<number>-<task-name>.md`, e.g. `01-login-form.md`. The set-relative task target reference for that task is `<task-set-name>/<number>-<task-name>.md`, e.g. `2026-05-31-user-auth/01-login-form.md`.

Do NOT close or modify any parent file.

### 5. Write the sidecar JSON manifest

Alongside the markdown files, write `<tasks-dir>/<task-set-name>/index.json` — a machine-readable manifest that a ralph loop (or any automation) can rely on to track completion and unblock ordering. Each entry mirrors one markdown file.

<manifest-schema>
```json
{
  "tasks": [
    {
      "id": "01-login-form",
      "file": "01-login-form.md",
      "title": "Login form",
      "type": "AFK",
      "effort": "standard",
      "status": "open",
      "blocked_by": []
    }
  ]
}
```

**Always** add a top-level `"worktree"` key — `to-tasks` binds every set to a checkout; there is no "unbound" default. Two arms, exactly one:

```json
{
  "worktree": { "name": "<current-worktree>" },
  "tasks": [ ... ]
}
```

- `{ "name": "<current-worktree>" }` — **the default.** Resolve the current checkout's operator-facing name with `basename "$(git rev-parse --show-toplevel)"` and write it verbatim — a *name*, never a path (the manifest is portable across machines), never the literal `current`. Write it **uniformly** for whatever checkout you are in — feature worktree, Trunk worktree, pop-managed, or already-bound — with no guard, warning, or refusal. pop adopts (never deletes) that worktree the first time the Queue drains the set.
- `{ "managed": true }` — written **only** for the `managed`/`isolated` argument. pop provisions its own worktree (and a branch named after the set) forked from the Trunk worktree the first time the Queue drains the set.

When the `auto-drain`/`drain` argument is given, also add a top-level `"auto_drain": true` key (omit it otherwise):

```json
{
  "auto_drain": true,
  "worktree": { "name": "<current-worktree>" },
  "tasks": [ ... ]
}
```

</manifest-schema>

Field rules:

- `auto_drain` — optional top-level boolean. **Omit by default.** Write `"auto_drain": true` only when the `auto-drain`/`drain` argument is given. Never infer it from task content; do not write `"auto_drain": false`. Written **silently regardless of checkout** — even trunk, pop-managed, or already-bound (no guard). Note the consequence on trunk: the Queue then commits task work onto your main branch unattended. Pop seeds the Auto-drain bit in Task state once at first registration; the Queue dashboard toggle remains authoritative afterward.
- `worktree` — **always written** — top-level object, `{ "name": "<current-worktree>" }` by default or `{ "managed": true }` for the `managed` argument (never both, never omitted). It is a one-time registration seed: pop reads it once at first registration and provisions/adopts lazily on the first Queue drain — editing it later does nothing. A foreground `pop tasks implement` ignores it entirely and binds the current checkout, so it routes **only** Queue drains. The default `{ "name": ... }` names the checkout you ran `to-tasks` in; combine with `auto_drain` to have the Queue drain unattended there. Writing `{ "name": ... }` for a checkout another set already uses is allowed — pop reference-counts managed-worktree teardown so sharing never strands a set (ADR-0116).
- `id` — the filename stem (`<number>-<task-name>`), stable identifier referenced by `blocked_by`.
- `status` — one of `open` | `done` | `failed` | `skipped`. Always initialize to `open`. Do not write `in_progress`; persisted `in_progress` is malformed.
- `blocked_by` — array of `id`s of blocking tasks. Empty array if none.
- `type` — `HITL` or `AFK`, matching the markdown.
- `effort` — one of `light` | `standard` | `heavy`. Write it explicitly for new tasks using the named-signal heuristic above. If absent in an existing manifest, it means `standard`.
- `agent` — optional escape hatch from ADR-0018. Fill it only when the user explicitly asks for a specific agent or model for a task; it is not part of the default planning flow.
- `failed_after` — optional integer; the number of attempts after which a runner gave up. Written only when `status` becomes `failed`.

The JSON is the source of truth for automation. The rules above — the eligibility condition (`status == "open"` and every `blocked_by` id is satisfied by a task whose status is `done` or `skipped`, preferring `AFK` over `HITL` among eligible tasks), the done-condition (all `## Acceptance criteria` boxes checked), and the commit format `tasks(<task-set-slug>): <id>` (set name without its timestamp prefix) — are the **contract** implemented by `pop tasks implement`, which drains the whole set or runs one task when given a `<task-set>/<file>.md` target.

Keep `index.json` and the markdown files in sync — every markdown file has exactly one manifest entry and vice versa.

### 6. Register the set

Run `pop tasks register <task-set-name>` to register the set and confirm it activated correctly. Registration is an explicit verb — writing the set files only *drafts* it; until you `register`, the set is inert (invisible to the dashboard, never scheduled, never auto-drained). Reads like `pop tasks status` never register. Pop prints `Registered new task set(s): <task-set-name>` on first registration.

Check the output:

- The task set appears in the table with status `READY` (or `DEFERRED` if every open task is HITL).
- It is **not** `MALFORMED` or `MISSING`.

If `MALFORMED`, read the diagnostics, fix the markdown/manifest issues they name, and re-run `pop tasks register <task-set-name>` until the set is `READY` or `DEFERRED`.

Tell the user the task-set name, its status, and how many tasks are open.

Then suggest draining the **whole set**: `pop tasks implement <task-set-name>` (no file target). Do not suggest implementing a single task such as the first file in the list — `pop tasks implement` drains the entire set in dependency order on its own, and that whole-set drain is the intended entry point. The targeted single-task form (`<task-set-name>/<file>.md`) exists only for re-running one specific task, not for kicking off the set.
