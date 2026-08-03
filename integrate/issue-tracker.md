# pop Work store

This document adapts the planning skills' publish steps to **pop's Work store** —
pop's own Task storage, reached through the `pop` CLI. A planning skill that only
knows "consult the Work store doc for this operation" can publish correctly using
the per-operation sections below; nothing here refers back into a skill body.

Resolution is two-layer. A repo-level `docs/agents/issue-tracker.md` wins when
present. Absent that, skills read the user-level `~/.agents/docs/issue-tracker.md`
— which is where *this* file is reached from. To change publish behaviour for one
repository, write the repo doc at `docs/agents/issue-tracker.md`.

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

**Map back-link (ADR-0129).** When the source is a Wayfinder Map, include
`Source map: <map-id>` as the **first line** of `spec.md`, before the template
headings. That line is human-facing prose — nothing parses it, and nothing
derives from it. The machine-readable halves of the link are written at ticket
time, by *Publishing tickets* → *Map-sourced sets*: `source_map` on the set's
`index.json`, and the map's own record written by `pop map spawned`. Never append
to `## Spawned sets` in `map.md` by hand; it is generated.

---

## Publishing tickets

Tickets are the task markdown files plus a sidecar `index.json` manifest, all
inside the same `<tasks-dir>/<task-set-name>/` folder. When breaking down a
co-located `spec.md`, reuse that folder and its `<task-set-name>` — write the
task files alongside the spec, do not mint a new folder. Write files in
dependency order (blockers first) so "Blocked by" can name real identifiers.

When the work came from a Wayfinder Map — directly or through its `spec.md` —
read *Map-sourced sets* at the end of this section **before** writing the
slices: it adds acceptance criteria to some of them.

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

## Orientation

Perishable pointers, as of authoring: the files to touch, the symbols and types
involved, and the exact build/test command that proves the slice. Verify before
trusting — anything here may have moved.

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

**Orientation is the one place paths belong.** "What to build" stays path-free
because durable intent outlives the tree; Orientation is explicitly the opposite
— a hint, stamped and labelled as stale-able, so the executor stops re-deriving a
map the author already had. Unattended drains spend a large share of their tool
calls on that rediscovery, and the author writes it for free from the context they
just used to slice the work. Fill it from what you actually know: name only the
files and symbols you touched or read while planning, plus the command that
verifies the slice. Omit the section only for a slice with no code surface (a
HITL sign-off); never pad it with guesses, because a wrong pointer costs more
than a missing one.

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
per file. The manifest carries the `tasks` array, plus `source_map` for a
Map-sourced set (see below) — and no `worktree` or `auto_drain` key (ADR-0115);
binding and auto-drain are `register` flags (below), never written here.

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

Set-level key:

- `source_map` — the map id this set was spawned from. Written on **every**
  Map-sourced set, spec or no spec, so the back-link is never half-built for a
  spec-less one. Absent otherwise.

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

#### The default routes on where you are standing

With no invocation arguments at all, the flags depend on the **checkout
locality** — whether the current directory is the Trunk worktree or a linked
worktree. Ask pop, do not derive it:

```bash
pop tasks checkout --locality      # prints exactly one word: trunk | worktree
```

| locality | default flags | effect |
| --- | --- | --- |
| `trunk` | `--managed --auto-drain` | forks an isolated worktree from the Trunk worktree, drained unattended |
| `worktree` | `--auto-drain` | plain register bound to **this** checkout — no new worktree — drained unattended |

The rule is that a human who has already switched into a worktree and then
breaks work down there is asking for the work to happen *here*. Stacking a
managed worktree on top of the checkout they chose would open the set's panes
somewhere they are not.

`pop tasks checkout` is read-only and needs no registered set, so it answers in
a repository that has never published one. Its `--locality` word is derived from
git alone — the same linked-worktree predicate a drain routes on — so it can
never disagree with where the work lands. A checkout declared trunk in config
that is nonetheless a linked worktree reads `worktree`, and **a bare repository
reads `worktree` in every checkout**, including the bare directory itself.
`--json` adds `path`, `branch`, `trunk_path` (omitted when unresolvable), `bare`
and `managed` for a caller that wants more than the branch.

Registering from the `worktree` branch may bind into a checkout that already
holds another set — including another set's managed worktree. That is allowed:
the second set binds alongside the first, and teardown stays reference-counted.

#### Keywords override the default

- `managed` / `isolated` → `--managed`. **Beats detection**: typed from inside a
  worktree it still provisions a fresh worktree forked from the Trunk worktree.
- `auto-drain` / `drain` → `--auto-drain`. Agrees with the default in both
  localities, so it changes nothing on its own.
- `no-drain` / `manual` → drops `--auto-drain` in both localities. From `trunk`
  that leaves `--managed` alone; from `worktree`, plain register with no flags.
  Combined with `managed` / `isolated` anywhere, it registers `--managed` only.

`managed` / `auto-drain` / `no-drain` / `manual` are **pop-store-only**. (When
the resolved Work store is not pop, a skill warns and ignores all of them, then
publishes to the configured store.)

Semantics:

- Plain `register` eagerly binds the set to the **current** checkout the moment it
  registers — visible immediately, not deferred.
- `--managed` provisions immediately instead: it forks an isolated worktree from
  the Trunk worktree and binds the set to it the moment it registers. A repo with
  no resolvable trunk refuses the registration; `--trunk <path>` names one (needed
  once per bare repo).
- **The trunk-less fallback is reachable only in the `trunk` branch.** When the
  default asked for `--managed` and no trunk resolves, retry the registration
  plain and warn the user. It cannot fire in the `worktree` branch, where the
  default is already plain and no trunk is resolved at all. Against an *explicit*
  `managed`/`isolated`, the refusal is reported as-is in either locality.
- `--auto-drain` lets the Work daemon drain the set unattended. It is independent
  of `--managed` — it only sets the set's consent bit — and is the default in
  both localities. Standing in a deliberately-chosen non-trunk worktree is the
  isolation an unattended drain needs; the case worth avoiding is draining the
  Trunk worktree unattended, which the `trunk` branch's `--managed` prevents.
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

### Map-sourced sets

A set broken down from a Wayfinder Map — directly, or through the `spec.md` a map
produced — carries two extra obligations. Read the map's
`$(pop work show-path)/maps/<map-id>/index.json`: it is where ticket status, type
and the decision **drafts** live.

**Mint the drafts through the slices that implement them (ADR-0171).**
Wayfinding writes nothing into the repository, so each resolved ticket's
`adr_drafts` and `context_drafts` are still sitting in the map's folder. They mint
in the slice that implements that ticket's subject, as acceptance criteria that
are pure file operations:

```markdown
- [ ] docs/adr/NNNN-<slug>.md created from
      <map-dir>/adrs/<8hex>-<slug>.md (next free ADR number)
- [ ] .grill-context/CONTEXT.<gen>.<HASH>.md created from
      <map-dir>/context/NN-<slug>.md
```

Write `<map-dir>` out in full — `$(pop work show-path)/maps/<map-id>` — so the
draining agent can open the draft without resolving anything.

Attribution needs no inference — the slice's `## Parent` names the map ticket. A
slice implementing no decision gets the parent reference and no minting checkbox.
Each draft mints **exactly once** across every set a map spawns, so a second
handoff carries only the drafts the first one left. Do not mint them yourself:
publishing does not commit, and an artifact written now would sit uncommitted
while the set's worktree forks past it.

**Record the link both ways.** On the set, write `"source_map": "<map-id>"` in
its `index.json` — always, spec or no spec. On the map, after `pop tasks register`
has succeeded:

```bash
pop map spawned <map-id> <task-set-name>
```

That verb is the only writer of the map's lineage: it appends the id to the map
manifest's `spawned_sets` and regenerates `## Spawned sets` in `map.md`. It is
idempotent, so a re-registered set is recorded once. Never edit the section or the
array by hand — the section is generated, and a hand-written line is lost on the
next resolve. There is no reverse flag on `pop tasks register`; the two halves are
written by the two sides that own them.

---

## Wayfinding operations

A Wayfinder Map is plain markdown in Task storage — no issue tracker, no labels,
no `/setup-matt-pocock-skills`. A map exists because its folder exists; there is
no registration step.

### Storage layout

Maps live under a `maps/` sibling of `tasks/`:

```
$(pop work show-path)/maps/<YYYY-MM-DD-slug>/
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

<!-- pop:generated decisions -->

- [01-first-ticket](issues/01-first-ticket.md) — one-line gist of the answer

<!-- /pop:generated decisions -->

## Not yet specified

<fog — graduates into tickets as the frontier advances>

## Out of scope

<!-- pop:generated out-of-scope -->
<!-- /pop:generated out-of-scope -->

## Spawned sets

<!-- pop:generated spawned-sets -->
<!-- /pop:generated spawned-sets -->
```

**`map.md` has two writers.** `Destination`, `Notes` and `Not yet specified` are
yours. `Decisions so far`, `Out of scope` and `Spawned sets` are pop's: it
rebuilds everything between the `pop:generated` markers from `index.json` and the
ticket answers on every resolve, so anything you write inside a region is lost on
the next one. Write prose outside the markers — it survives — and never hand-edit
a decision line.

The `Status:` line (top of `map.md`, before headings) is `active` (default while
wayfinding), `arrived` (destination reached — written by `pop map arrive`, never by
hand), or `abandoned` (closed without reaching the destination). Any other value
renders the Map BROKEN with the fix printed; `done` is retired and does not fold.
Charting writes `Status: active` and ends with
`pop map register <map-id>`, which validates the Map's `index.json` and makes it
registered Work; it prints every problem it finds and is re-runnable until clean.
Open tickets are **not** listed in `map.md`; they are files under `issues/`,
discovered by reading the directory.

Ticket files (`issues/NN-<slug>.md`) hold prose only — id, title, type, status
and blockers live in the Map's `index.json`, which every consumer reads:

```markdown
## Question

<the decision or investigation this ticket resolves>

## Answer

<!-- pop:generated answer -->

<written by `pop map resolve` — prose answer, links to assets>

<!-- /pop:generated answer -->
```

The answer body between those markers is pop's: `pop map resolve` replaces it
whole on every run, headings and all, so edit the answer file and resolve again
rather than hand-editing the ticket.

Per ticket, `index.json` carries `id`, `file`, `title`, `type`
(`research|prototype|grilling|task`), `status` (`open|resolved`), `blocked_by`
(blocker ids, e.g. `["01"]`) and `out_of_scope`.

### Claiming

Take a ticket with `pop map next [<map-id>]` — it claims the first frontier
ticket atomically and prints `<id>\t<path>` — or `pop map claim <map-id> <NN>`
when you are naming one. A claim is a pop.db row owned by your tmux pane, not a
file state, so never write one into the markdown; it frees itself after four
hours. A ticket is **unblocked** when every blocker is `resolved`; the
**frontier** is the open, unblocked, unclaimed tickets — the edge of the known,
and the only thing `next` hands out.

`next` also spawns the ticket's grilling window inside the map's own tmux session
`pop-map-<map-id>` — window 1 there runs `pop map show` — and switches you to it.
`pop map open <map-id>` creates or attaches that session on its own. The other
writes (`register`, `claim`, `resolve`, `out-of-scope`) run **in place**: they
ensure the session exists, tell you where it is, and never move you, so calling
one from a task-set pane is safe. Reads create no tmux state at all. The session
is rooted at the repository's trunk worktree; pass `--trunk <path>` if pop cannot
work out which checkout that is.

### Resolution

Write the decision to a file, then hand it to pop:

```bash
pop map resolve <map-id> <NN> --answer-file <path>
```

One call writes `## Answer`, flips the manifest entry to `resolved`, re-renders
the generated regions of `map.md` and releases your claim. It is re-runnable: a
second run **replaces** the answer, so fix a mistake by resolving again — never
by editing the ticket or the index by hand. Link assets from the answer; don't
paste them in full.

**Out of scope:** for a mis-scoped ticket, run
`pop map out-of-scope <map-id> <NN> --reason "<why>"`. It resolves the ticket the
same way but renders it under `Out of scope`, never into the decision index.

**Ticket-type overrides:**

- **Grilling** (HITL): run the `grill-with-map` skill (not `grill-with-docs`,
  `/grilling` or `/domain-modeling`) — it writes ADR and glossary drafts into
  the map's own directory and never into the repository. Work with the human;
  never answer your own grilling questions.
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

**A map hands off whole, once.** When the way to the destination is clear, suggest
`to-spec` (then `to-tasks` over its `spec.md`), or `to-tasks` directly for a small
map whose ticket answers already *are* the spec. Never pre-split a map into
per-area sets: the sequencing constraints that matter already live inside the
answers and travel with the map for free, while a chunk boundary has to be invented
by a session that has read fewer answers than `to-spec` will. Wayfinding produces
decisions; implementation happens in ordinary registered Task sets. Record the
forward link both ways:

1. **On the map:** `pop map spawned <map-id> <task-set-name>`, run after the set
   registers. It appends the id to the `spawned_sets` array in the map's
   `index.json` and regenerates `## Spawned sets` in `map.md`; it is idempotent,
   and it is the only writer of either. Appending to the section by hand is lost
   on the next resolve.
2. **On the set:** `source_map` in the set's `index.json`, always — plus, where a
   spec exists, the `Source map: <map-id>` line `to-spec` writes as the first
   line of `spec.md`. That line is prose for a human; nothing parses it.

Then declare arrival: `pop map arrive <map-id>` writes `Status: arrived` and tears
down the map's tmux session. The gate is the **destination**, not empty fog — a map
may carry deliberately non-prerequisite fog forever — so arrival lists open or
claimed tickets and proceeds anyway; never resolve a ticket just to clear the gate.
`pop map open <map-id>` reverses it when fog reopens, putting you back in a fresh
session for the map. One map may still spawn a
further set later: a remediation set, or a second handoff once fog that was open at
the first handoff has cleared. An arrived map stays visible in the Work dashboard —
it is the lineage view for the sets it spawned; `pop map archive` is what files it
away.
