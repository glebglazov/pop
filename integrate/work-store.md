# pop Work store

This document adapts the planning skills' publish steps to **pop's Work store** —
pop's own Task storage, reached through the `pop` CLI. A planning skill that only
knows "consult the Work store doc for this operation" can publish correctly using
the per-operation sections below; nothing here refers back into a skill body.

Resolution is two-layer. A repo-level `docs/agents/issue-tracker.md` (the upstream
tracker-doc convention) wins when present. Absent that, skills read *this* file,
pop's Shipped asset at `${XDG_DATA_HOME:-~/.local/share}/pop/work-store.md`.
pop rewrites it on every Integration refresh whenever its bytes differ from the
embedded copy — so to change publish behaviour, write the repo doc at
`docs/agents/issue-tracker.md`.

All paths resolve through one command. Run it once per session:

```bash
pop work show-path
```

It prints this repository's Task-storage root — an absolute path in pop's data
directory, outside the repo tree — and creates it on demand. The tasks base
directory (`<tasks-dir>` below) is `$(pop work show-path)/tasks`; `pop tasks
show-path` prints that same `tasks/` directory directly (ADR-0130 alias).

---

## Publishing a spec

A spec is co-located inside its own task-set folder, so the set's whole context
lives in one directory:

```
<tasks-dir>/<task-set-name>/spec.md
```

- `<task-set-name>` is `<timestamp>-<slug>`. `<slug>` is a short hyphen-delimited
  descriptive name (e.g. `user-auth`); it carries over to the task breakdown, so
  the spec and its tasks share one folder and one name.
- `<timestamp>` is a human-readable local date/time prefix so task sets sort
  chronologically:
  - Default: `YYYY-MM-DD` (e.g. `2026-05-31`).
  - If a folder with the same date and slug already exists: `YYYY-MM-DD-HHMM`
    (24-hour local time, e.g. `2026-05-31-2036`).
  - Examples: `2026-05-31-user-auth/spec.md`, `2026-05-31-2036-user-auth/spec.md`.

Create the `<tasks-dir>/<task-set-name>/` directory now and write `spec.md` into
it. At this stage the folder holds only `spec.md`; the set is **inert** —
invisible to the dashboard and never scheduled — until it is later registered
with `pop tasks register` (see *Publishing tickets*). The spec artifact is named
`spec.md`; there is no backward-compatible read of any other filename.

**Map back-link (ADR-0129).** When the source is a Wayfinder Map, record the
forward link both ways:

1. Include `Source map: <map-id>` as the **first line** of `spec.md`, before the
   template headings.
2. Append `<task-set-name>` under the map's `## Spawned sets` section in `map.md`
   (create the section if absent).

---

## Publishing tickets

Tickets are the task markdown files plus a sidecar `index.json` manifest, all
inside the same `<tasks-dir>/<task-set-name>/` folder. When breaking down a
co-located `spec.md`, reuse that folder and its `<task-set-name>` — write the
task files alongside the spec, do not mint a new folder. Write files in
dependency order (blockers first) so "Blocked by" can name real identifiers.

### Task markdown template

Each slice is one file named `<number>-<task-name>.md` (e.g. `01-login-form.md`).
The set-relative target reference is `<task-set-name>/<number>-<task-name>.md`.

```markdown
## Parent

A reference to the parent item (omit this section if there is no parent file).

## What to build

A concise description of this vertical slice — the end-to-end behavior, not a
layer-by-layer implementation. Avoid specific file paths or code snippets; they
go stale fast. Exception: a prototype-derived snippet that encodes a decision
more precisely than prose (state machine, reducer, schema, type shape) may be
inlined, trimmed to the decision-rich parts, noting it came from a prototype.

## Type

HITL or AFK.

## Acceptance criteria

- [ ] Criterion 1
- [ ] Criterion 2

## Blocked by

- A reference to the blocking item (if any)

Or "None - can start immediately" if no blockers.
```

Do NOT close or modify any parent (spec or source) file while writing tickets.

### HITL / AFK typing rules

Every slice is either **HITL** or **AFK**; prefer AFK wherever possible.

- **AFK** slices can be implemented and merged without human interaction.
- **HITL** slices contain ONLY human work — verification, decisions, manual
  testing; the executor never runs them. Write the body as instructions to the
  human: the exact steps to perform. A HITL slice whose "What to build"
  describes software is mis-typed.

If a HITL slice needs an artifact built first (a report command, test data, a
harness), split it: the build goes in an AFK slice, and the HITL slice depends on
it via "Blocked by" and holds only the human steps.

HITL slices have two roles, at opposite ends of a set:

- **Approval at the end** — agents are done and have verified their own work; the
  human signs off. Nothing depends on it, so the set reaches `AWAITING-APPROVAL`.
  This is the common, expected HITL.
- **Setup at the bottom** — the human provisions something the agent genuinely
  cannot (mainly accounts and secrets the agent can't self-issue) before agents
  can run; AFK slices depend on it and the set sits `BLOCKED` until the human
  acts. Create a setup HITL only when *absolutely necessary* — never for things
  the model can discover or do itself (devices, environment details, readable
  config).

A HITL in the middle is valid (a genuine mid-flow human decision) but parks the
set at `BLOCKED` mid-drain — the correct signal that agent work waits behind a
human.

### Effort named-signal heuristic

Assign an `effort` to every slice, written explicitly:

- `heavy` — architectural or cross-cutting refactors, or genuinely tricky
  algorithms.
- `light` — large but mechanical work: renames, codemods, config, boilerplate.
- `standard` — everything else. The default when no named signal clearly applies.

Effort is model-strength intent, not an agent choice; do not consult
`pop tasks agents` in the default flow.

> **Artifacts must already be committed.** Task sets are often worked in a fresh
> worktree forked from the current branch's HEAD, so any CONTEXT/ADR/code a prior
> session generated must already be on HEAD for the worktree to carry it. Publishing
> does **not** commit — committing belongs to the session that produced the
> artifacts. If you spot uncommitted session artifacts, flag them; don't commit.

### `index.json` manifest

Write `<tasks-dir>/<task-set-name>/index.json` alongside the markdown, one entry
per file. The manifest carries **only** the `tasks` array — no `worktree` or
`auto_drain` key (ADR-0115); binding and auto-drain are `register` flags (below),
never written here.

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

Field rules:

- `id` — the filename stem (`<number>-<task-name>`); the stable identifier
  referenced by `blocked_by`.
- `file` — the task's markdown filename.
- `title` — a short human label.
- `type` — `HITL` or `AFK`, matching the markdown.
- `effort` — `light` | `standard` | `heavy`, per the heuristic above. Absent in
  an existing manifest means `standard`.
- `status` — `open` | `done` | `failed` | `skipped`. Always initialize to `open`.
  Never write `in_progress`; a persisted `in_progress` is malformed.
- `blocked_by` — array of blocker `id`s; empty array if none.
- `agent` — optional escape hatch (ADR-0018). Fill it only when the user
  explicitly asks for a specific agent or model; not part of the default flow.
- `failed_after` — optional integer; attempts after which a runner gave up.
  Written only when `status` becomes `failed`.

The JSON is the source of truth for automation. The eligibility condition
(`status == "open"` and every `blocked_by` id is satisfied by a task whose status
is `done` or `skipped`, preferring `AFK` over `HITL` among eligible tasks), the
done-condition (all `## Acceptance criteria` boxes checked), and the commit format
`tasks(<task-set-slug>): <id>` (the set name without its timestamp prefix) are the
contract implemented by `pop tasks implement`. Keep `index.json` and the markdown
in 1:1 sync — every markdown file has exactly one manifest entry and vice versa.

### Register the set

Registering is an explicit verb — writing the files only *drafts* the set (inert
until registered). Run:

```bash
pop tasks register <task-set-name>
```

Add flags per the invocation arguments:

- plain (no flags) by default;
- `--managed` for the `managed` / `isolated` argument;
- `--auto-drain` for the `auto-drain` / `drain` argument;
- both together for `managed auto-drain`.

`managed` / `auto-drain` are **pop-store-only**. (When the resolved Work store is
not pop, a skill warns and ignores them, then publishes to the configured store.)

Semantics:

- Plain `register` eagerly binds the set to the **current** checkout the moment it
  registers — visible immediately, not deferred.
- `--managed` provisions immediately instead: it forks an isolated worktree from
  the Trunk worktree and binds the set to it the moment it registers. A repo with
  no resolvable trunk refuses the registration; `--trunk <path>` names one (needed
  once per bare repo).
- `--auto-drain` lets the Queue daemon drain the set unattended. Only the literal
  keywords enable it — there is no "here and now" phrasing.
- `managed auto-drain` → `pop tasks register --managed --auto-drain <task-set-name>`,
  the safest unattended combo (isolated worktree, drained unattended).
- Re-registering an already-registered set never rebinds it. To move it to a
  different checkout, run `pop tasks bind-worktree <task-set-name> --force` from
  inside the target checkout.

pop prints `Registered new task set(s): <task-set-name>` on first registration.
Reads like `pop tasks status` never register.

### The MALFORMED fix loop

Check the output. The set should appear with status `READY` (or `DEFERRED` if
every open task is HITL) — **not** `MALFORMED` or `MISSING`. If `MALFORMED`, read
the diagnostics, fix the markdown/manifest issues they name, and re-run
`pop tasks register <task-set-name>` until the set is `READY` or `DEFERRED`.

### Report and suggest the whole-set drain

Tell the user the task-set name, its status, and how many tasks are open. Then
suggest draining the **whole set**:

```bash
pop tasks implement <task-set-name>
```

Do not suggest implementing a single task (e.g. the first file). `pop tasks
implement` drains the entire set in dependency order on its own, and that whole-set
drain is the intended entry point. The targeted single-task form
(`<task-set-name>/<file>.md`) exists only for re-running one specific task.

---

## Wayfinding operations

A Wayfinder Map is plain markdown in Task storage — no issue tracker, no labels,
no `/setup-matt-pocock-skills`. A map exists because its folder exists; there is
no registration step.

### Storage layout

Maps live under a `wayfinder/` sibling of `tasks/`:

```
$(pop work show-path)/wayfinder/<YYYY-MM-DD-slug>/
├── map.md
└── issues/
    ├── 01-<slug>.md
    ├── 02-<slug>.md
    └── ...
```

`<YYYY-MM-DD-slug>` is the map id (e.g. `2026-07-19-wayfinder-work-dashboard`).
Ticket files are `NN-<slug>.md`, where `NN` is a zero-padded ticket number.

`map.md` follows the upstream section shape with pop additions at top and bottom:

```markdown
Status: active

## Destination

<one or two lines — every session orients here first>

## Notes

<domain; skills to consult; standing preferences>

## Decisions so far

- [01-first-ticket](issues/01-first-ticket.md) — one-line gist of the answer

## Not yet specified

<fog — graduates into tickets as the frontier advances>

## Out of scope

<work ruled beyond the destination>

## Spawned sets

<!-- forward links to Task sets this map spawned via to-spec / to-tasks -->

- <task-set-id>
```

The `Status:` line (top of `map.md`, before headings) is `active` (default while
wayfinding), `done` (way found — write at handoff), or `abandoned` (closed without
reaching the destination). Charting writes `Status: active`. Open tickets are
**not** listed in `map.md`; they are files under `issues/`, discovered by reading
the directory.

Ticket files (`issues/NN-<slug>.md`) put metadata lines first (parsed by
`pop wayfinder` and the Work dashboard):

```markdown
Type: research|prototype|grilling|task
Status: open|claimed|resolved
Blocked by: 01, 02

## Question

<the decision or investigation this ticket resolves>

## Answer

<written on resolution — prose answer, links to assets>
```

- `Type:` — one of `research`, `prototype`, `grilling`, `task`.
- `Status:` — `open` (default; omitting the line means open), `claimed` (this
  session owns it), or `resolved` (decision recorded).
- `Blocked by:` — comma-separated blocker ticket numbers (e.g. `01, 02`).

### Claiming

Set `Status: claimed` in the ticket file **first**, before any investigation or
conversation. Concurrent sessions must skip claimed tickets. An open, unclaimed
ticket is takeable. A ticket is **unblocked** when every blocker is `resolved`;
the **frontier** is the open, unblocked, unclaimed tickets — the edge of the known.

### Resolution

To resolve a ticket:

1. Write the decision under `## Answer` in the ticket file (link assets, don't
   paste them in full).
2. Set `Status: resolved`.
3. Append one line to the map's **Decisions so far**:
   `[<ticket title>](issues/NN-<slug>.md) — <one-line gist>`.

**Out of scope:** for a mis-scoped ticket, set `Status: resolved` with a brief
answer explaining why, and add one line to the map's **Out of scope** section
(not Decisions so far).

**Ticket-type overrides:**

- **Grilling** (HITL): run the `grill-with-docs` skill (not `/grilling` or
  `/domain-modeling`). One question at a time with the human; never answer your
  own grilling questions.
- **Research** (AFK): run the `research` skill. Record findings in the ticket's
  `## Answer` with source citations — do **not** open a throwaway `research/<name>`
  branch or any side branch for research output.
- **Prototype** (HITL): run the `prototype` skill. Link the prototype's path and
  record the verdict in the ticket's `## Answer`; the artifact stays in the working
  tree (this overrides upstream prototype's "commit to a throwaway branch").
- **Task** (HITL or AFK): manual work that unblocks a decision — provisioning
  access, signing up for a service, moving data so its shape can be seen. The
  agent drives it alone where it can; otherwise it hands the human a precise
  checklist. The answer records what was done and any facts later tickets depend on.

Never resolve more than one non-research ticket per session; research tickets may
be burned down in parallel. Expect parallel sessions — re-read ticket status
before claiming.

### Handoff to implementation

When the way to the destination is clear — or an early-splittable chunk is —
suggest `to-spec` and/or `to-tasks`. Wayfinding produces decisions; implementation
happens in ordinary registered Task sets. Record the forward link both ways:

1. **On the map:** append each spawned task-set id under `## Spawned sets` in
   `map.md`.
2. **On the set:** `to-spec` writes a `Source map: <map-id>` line as the first
   line of `spec.md`.

Then set `Status: done` in `map.md`. One map may spawn many sets over time; only
mark `done` when wayfinding for this destination is finished (individual chunks
may hand off earlier while the map stays `active` if fog remains).
