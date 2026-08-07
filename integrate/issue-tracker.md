# pop Work store

This document adapts the planning skills' publish steps to **pop's Work store** —
pop's own Task storage, reached through the `pop` CLI. A planning skill that only
knows "consult the Work store doc for this operation" can publish correctly using
the per-operation sections below; nothing here refers back into a skill body.

Resolution is two-layer. A repo-level `docs/agents/issue-tracker.md` wins when
present. Absent that, skills read the user-level `~/.agents/docs/issue-tracker.md`
— which is where *this* file is reached from. To change publish behaviour for one
repository, write the repo doc at `docs/agents/issue-tracker.md`.

**The override is skill convention, not something pop's Go reads.** No code
consults the repo doc; only a skill resolving "which issue-tracker doc applies"
does. It governs **store choice and behavioural convention** — which register
flags a repository defaults to, how the report-and-suggest step is worded, and
so on — never authoring shape. It never really governed shape either: a
validator compiled into the binary enforces the file templates and enums
regardless of what any doc says, so a repo doc that redefined one only ever
produced a validation error. Authoring shape now has one owner and it isn't this
file — see the pointers below.

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
lives in one directory. Run `pop tasks authoring-guide` for the folder layout,
the `<task-set-name>` naming convention, and the `spec.md` template — including
the `Source map:` back-link line for a Map-sourced spec. Write the folder now;
at this stage it holds only `spec.md`, and the set stays **inert** — invisible
to the dashboard and never scheduled — until it is registered (see *Publishing
tickets*).

---

## Publishing tickets

Tickets are the task markdown files plus a sidecar `index.json` manifest, all
inside the same `<tasks-dir>/<task-set-name>/` folder — reuse a co-located
spec's folder rather than minting a new one. Run `pop tasks authoring-guide` for
the file template, the manifest field list and allowed values, the HITL/AFK
typing rules (including the split-the-slice rule and both legitimate HITL
positions), the effort heuristic, the vertical-slice framing, and the
Orientation rule. It is authoritative for all of it, generated from the same
constants `pop tasks register` enforces, so it cannot disagree with what gets
validated.

When the work came from a Wayfinder Map — directly or through its `spec.md` —
read *Map-sourced sets* at the end of this section **before** writing the
slices: it adds acceptance criteria to some of them.

> **Artifacts must already be committed.** Task sets are often worked in a fresh
> worktree forked from the current branch's HEAD, so any CONTEXT/ADR/code a prior
> session generated must already be on HEAD for the worktree to carry it. Publishing
> does **not** commit — committing belongs to the session that produced the
> artifacts. If you spot uncommitted session artifacts, flag them; don't commit.

### Register the set

Registering is an explicit verb — writing the files only *drafts* the set (inert
until registered). Run:

```bash
pop tasks register <task-set-name>
```

#### The default binds the current checkout

With no invocation arguments at all, the flags are the same in every checkout:
plain `register` plus `--auto-drain`. The set binds to the checkout you are
standing in — no new worktree — and the Work daemon may drain it unattended.

The rule is that a human who breaks work down is asking for the work to happen
*here*. That holds standing on the Trunk worktree exactly as it holds in a linked
worktree, so the default does not read where you are standing at all. Isolation
is a thing to ask for, not a thing to receive: a managed worktree is provisioned
only when someone types `managed` / `isolated`.

Registering may bind into a checkout that already holds another set — including
another set's managed worktree. That is allowed: the second set binds alongside
the first, and teardown stays reference-counted.

#### Keywords override the default

- `managed` / `isolated` → adds `--managed`.
- `auto-drain` / `drain` → adds `--auto-drain`. The default already passes it, so
  it changes nothing on its own.
- `no-drain` / `manual` → drops `--auto-drain`. Typed alone it leaves a plain
  register with no flags; combined with `managed` / `isolated` it registers
  `--managed` only.

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
- **`--auto-drain` is on unless `no-drain` / `manual` turns it off.** No other
  keyword affects it: `managed` / `isolated` explicitly keeps it. The flag only
  sets the set's consent bit, letting the Work daemon drain the set unattended,
  and is independent of `--managed`. An unattended drain therefore runs in the
  checkout the set is bound to — the Trunk worktree included, which is the
  deliberate consequence of binding here by default.
- Re-registering an already-registered set never rebinds it. To move it to a
  different checkout, run `pop tasks bind-worktree <task-set-name> --force` from
  inside the target checkout.

pop prints `Registered new task set(s): <task-set-name>` on first registration.
Reads like `pop tasks status` never register.

Check the output. The set should appear with status `READY` (or `DEFERRED` if
every open task is HITL) — **not** `MALFORMED` or `MISSING`. If `MALFORMED`, read
the diagnostics it prints, fix what they name, and re-run
`pop tasks register <task-set-name>` until the set is `READY` or `DEFERRED`; run
`pop tasks authoring-guide` (or `pop tasks register -h`) for the full contract
being enforced.

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
no registration step. Run `pop map authoring-guide` for the folder layout, the
`map.md` and ticket templates, the `Status:` vocabulary, the `pop:generated`
region markers and which sections they cover, and the `index.json` field list —
it is generated from the same constants `pop map register` enforces, so it
cannot disagree with what gets validated.

### Claiming

Take a ticket with `pop map next [<map-id>]` — it claims the first frontier
ticket atomically and prints `<id>\t<path>` — or `pop map claim <map-id> <NN>`
when you are naming one. `pop map fan-out [<map-id>]` does `next` for every
frontier ticket at once and prints the same block per ticket plus a total; an
empty frontier is a message and exit 0. A claim is a pop.db row owned by the tmux
pane running the agent, not a file state, so never write one into the markdown; it
frees itself after four hours. A ticket is **unblocked** when every blocker is
`resolved`; the **frontier** is the open, unblocked, unclaimed tickets — the edge
of the known, and the only thing `next` and `fan-out` hand out.

Both spawn the ticket's grilling pane inside the map's own tmux session
`pop-map-<map-id>`, whose single `map` window holds one tiled pane per ticket
tagged with the ticket id — the same window the map's own `assist` pane sits in.
Neither moves you unless you pass `--focus`, and a pane
whose agent is still alive is a jump target rather than being sent work twice.
`pop map status <map-id>` is the render you type when you want to see the Map;
`pop map open <map-id>` creates or attaches the session on its own. The other
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

**An assist session resolves nothing.** `pop map assist [<map-id>]` opens a
session scoped to the whole map rather than to a ticket — for the idea that
arrives with no ticket in hand: new scope for an existing ticket, a fresh ticket,
a patch of fog, or the realisation that something sits past the destination. It
claims nothing, and it must not run `pop map resolve`: resolving belongs to the
ticket's own claimed session, which is what makes the
one-non-research-ticket-per-session rule above traceable. When a decision is what
the conversation actually wants, hand the human `pop map next <map-id> <NN>` and
stop. `pop map out-of-scope` is the one exception, and it is not a resolution: it
is a scoping act, it renders under `Out of scope` and never into the decision
index, and "that is past the destination" is exactly the ticketless realisation
assist exists for. Redrawing the map's `Destination` is the same act one level up,
so an assist session may do that too. Run `pop map authoring-guide` for what an
assist session may write.

**An assist session closes by re-validating.** Its writes are hand-written files,
so end the session with `pop map register <map-id>` and work the fix list it
prints until it is clean — the same re-runnable loop charting ends on.

One pane per map, reused: a second `pop map assist` for the same map lands in the
first pane rather than opening a second session, so two conversations never race
on the map's prose. Assist is reachable whatever the frontier looks like — an
empty or fully-claimed frontier is when it is most needed.

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
