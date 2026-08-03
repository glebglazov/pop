---
status: accepted
---

# `work` is one `Kind` interface with data-shaped returns and kind-side adapters

## Context

pop has three kinds of work in flight and one model for exactly one of them. A
Task set has a registry row, a status derivation, a dashboard row, a sort order,
an action menu and a supervisor that advances it. A Map has a folder, four read
verbs and — since [ADR-0130](0130-the-queue-dashboard-becomes-the-work-dashboard.md)
— a row on the same dashboard, reached by an `IsMap` boolean on the Task-set row
model and roughly thirty branches hanging off it. Project routines have their own
dashboard, their own row type and no relationship to either.

The row model is the shape of the problem. `work.Row` embeds a `SetRef` of
task-set coordinates (`DefPath`, `RepoKey`, `Parked`, `Bound`, `Orphaned`,
`AutoDrain`, `VerifiedAtSHA`, `DestKind`, …) and a Map fills the three fields it
can (`IsMap`, `MapOpen`, `MapFrontier`) and leaves the rest blank. Every surface
then re-asks "is this a map?" — the status cell, the menu, the detail frame, the
summary, livepane, the spawner. Adding a third kind means a second boolean and
another thirty branches, which is not a thing anyone would choose to do twice.

`work`'s snapshot builder had also grown into the union of both kinds: it scanned
`tasks` for registered sets *and* `wayfinder` for Maps, so the package that was
supposed to be the shared data core imported every kind it served.

Three shapes were weighed for the unification. A **pure data shape** with no
interface cannot express per-kind actions or per-kind ordering, so every consumer
would go back to switching on a kind tag. **`Container` as an interface** —
method per cell — buys lazy cell computation nobody needs and makes a row
unprintable in a test without a mock. **Generics over the item type** leak the
kind into every consumer's type parameters, including the dashboard's.

The tempting middle road is a shared status taxonomy: a small set of facets
("needs you", "running", "blocked") every kind maps onto, so one comparator can
rank a Map against a task set. That is a shared vocabulary in disguise — the
facets would have to be defined by looking at both kinds and would be re-litigated
by the third — and it is exactly what the container+item model exists to avoid.

## Decision

- **One `Kind` interface; `work` knows no kind.** `work` defines `Kind` —
  `ID`, `Load`, `Less`, `Actions`, `ItemActions`, `Perform`, `Summary` — and the
  snapshot builder that walks a list of them. Every read of the filesystem and of
  pop.db happens inside a kind's `Load`.

- **The returns are plain data structs, not interfaces.** `Container`, `Item`,
  `Action`, `Outcome` and `Section` carry fields, because every consumer reads
  those fields to render them and a method per cell would only add indirection.
  They import neither bubbletea nor lipgloss, so [ADR-0143](0143-the-work-dashboard-data-core-lives-in-a-work-package.md)'s
  boundary and its guard test hold unchanged and styled wrappers stay TUI-side.

- **Import direction is one-way: kinds comply, `work` imports nothing.**
  Adapters live kind-side — `tasks/setkind` and `wayfinder`'s `MapKind` today —
  and the wiring list lives at the CLI edge, which hands `[]work.Kind` to the
  builder. `work` importing every kind would make it a hub that grows an import
  per future kind, and a future kind consuming `work` would then cycle;
  `init()`-time self-registration would hide the wiring where nobody looks for
  it. A second guard test asserts the direction, because the cost of finding out
  by cycle is a rewrite. The accepted cost is the explicit list in `cmd`.

  The Task-set adapter sits in `tasks/setkind` rather than in `tasks` because it
  needs `tasks/binding`, which imports `tasks`. The repository-group resolution
  both kinds scan through moved to `repogroup` for the same reason: it is
  per-repository infrastructure, not one kind's.

- **No shared status facets.** A container surfaces its kind and its own status
  label, nothing else. Ordering is fixed kind precedence — the closed enum's
  order, task sets then Maps then Routines — then that kind's own `Less`, which
  is only ever asked about containers it produced. `tasks` ports today's
  `sortTier`/`statusBand`/`statusOrder` wholesale, so the Task-set order is the
  same order, not a re-derivation. Header counts come from `Kind.Summary`, joined
  with `·` in kind order.

  The accepted cost is that an urgent Map can never outrank a `DONE` task set,
  and that a Map no longer interleaves between two projects' task sets the way
  the single comparator let it. Both follow from having no cross-kind ranking to
  derive, which is the point.

- **Capabilities are lazy.** `Actions`/`ItemActions` are called when a menu opens
  over one container, not per container at build time: menus open one at a time,
  and eligibility can consult state that moved since the snapshot. A container
  therefore cannot advertise its action count without a call — accepted.

- **`Perform` returns an outcome, not a side effect the caller must guess at**:
  a message, a refresh, a handoff (preserving [ADR-0158](0158-dashboard-verbs-split-by-whether-they-hand-off-and-say-so-in-the-key-case.md)'s
  tmux/exec split), or a hand-back for a caller-owned modal. The Task-set drain,
  bind and abandon pickers stay caller-side in this version; moving them behind
  `Perform` needs a modal-capable `Outcome` and is deferred deliberately.

- **One registry, membership only.** One pop.db registry keyed `(kind, id)`
  holds membership plus machine-local runtime — registered-at, consent bits,
  claims. It never caches derived status: derivation is cheap and a cache is a
  second source of truth. Work items get rows only when something must point at
  them.

- **The kind enum stays closed.** "Keep future kinds expressible" is a constraint
  on the model, not a plugin requirement: a goal-driven kind is a container of
  experiment items, a user-composed pipeline is a container of stage items — both
  fit and need no new seam. Every new kind is a deliberate edit to `work/ref`'s
  list plus an adapter.

## Consequences

The row model does not die here. `work.Row` stays, derived from `Container`, so
every consumer that has not moved yet keeps compiling and the tree stays green
while the surfaces migrate one at a time; the `IsMap` branches die with it. Two
transitional artefacts pay for that: `work.SetStatus`, of which
`tasks.TaskSetStatus` is an alias so the seam can hold the legacy row without
importing the kind that owns the vocabulary, and the composed-cell dispatch the
TUI keeps until it reads containers. Both are deleted with `Row`.

*Amended when the dashboard's rows moved onto `Container`.* Two details recorded
above landed differently. `work.Row` became an alias of `Container` rather than a
struct derived from it — the Task-set-only cells were absorbed into the container
instead of hung off it, so there is no second row model to derive, and the
`IsMap` boolean was deleted outright. And `StatusCell` is a method on `Kind`, not
a field on `Container`: the styled render needs the cell as tone-tagged segments
rather than one string, and a stored copy beside the composer that produces it
would be a second source of truth for the same text.

*Amended when the detail view became generic.* The detail frame reads the
container the table already built rather than loading its own copy: the periodic
rebuild is the one data path, so a per-detail loader that could disagree with the
row it was opened from no longer exists, and `Container` grew the two fields a
detail header and body need from a kind — `DetailSections` and a one-line
`Headline`. `Item` grew the same way, gaining the cells a reader sees (`Type`,
`StatusLabel` for a status that reads as more than its word, and an absolute
`File`) while its verbs stay off it, asked of the kind when a menu opens. Two
Map-only affordances were spent on that generality: the ticket table's
frontier/dim colouring and the flat Enter-to-grill shortcut, both now reached
through the item menu the kind fills.

A conformance test in `work` drives the real adapters through every method of the
interface, so a fourth kind earns its coverage by being named in one table rather
than by growing a suite. Because every kind imports `work`, that test lives in an
external test package — an in-package test in `work` can never import a kind.
