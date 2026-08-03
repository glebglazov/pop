---
status: accepted
---

# Routines are a Work kind: both seams on one adapter, a page of their own, and discovered membership

## Context

Ticket 06 of the *generalize-work* map asked how far Routines fold into the Work
model. The earlier steer was that they keep their own view and implement only the
advance seam
([ADR-0176](0176-the-work-supervisor-drives-a-second-seam-advancer.md)), on the
reasoning that a Routine's columns and verbs look nothing like a Task set's.

Looking at what was actually there overturned it. `routine/dashboard.go` already
held the exact shape the generic Work detail view had just generalized — rows that
drill into a per-row history list — wrapped in a hand-rolled second table, second
list, second menu and second frame. Leaving Routines out would have preserved the
duplication this whole effort exists to remove, and would have left two answers to
"what is a row" in one binary.

Three facts about Routines resisted the fold and had to be decided rather than
smoothed over: their storage is the global data dir, not a per-repository Work
store; their columns share no cell with the Task-set columns; and their relevance
depends on where the reader is standing, which no comparator can see.

## Decision

**The Routine adapter implements the read seam as well, on the same object that
already wears the advance seam.** One adapter, both relationships with the Work
model — what a reader sees and what the daemon may fire. A caller wires the kind
once; the **Work supervisor** takes the advance half by type assertion exactly as
it does for a Task set.

**`Kind` gains `Columns() []string`, and a dashboard page takes its header from
its primary kind.** The Work dashboard becomes two pages behind the `v` toggle
that already exists on both sides: page A holds Task sets and Maps, page B holds
Routines. A non-primary kind on a page fills the primary kind's columns, which is
what Map rows already do. Rejected: declaring headers dashboard-side per page,
which separates the header from the kind that authors the cells and makes a page
for a future kind cost custom dashboard code. Also rejected: one flat list with a
kind filter — the paged split matches the existing toggle and keeps two very
different column sets legible.

**No registry rows for Routines. Membership is derived from `BoundDirectory`, the
canonical cwd stamped at creation.** Storage stays the global data dir and
discovery stays the folder walk. A membership row would be a second source of a
fact that already exists; moving Routine storage into the per-repository Work
store would cost a migration of every Routine, would kill Routines bound to
directories no repository owns, and would buy no capability. The model rule this
establishes for every future kind: **membership is registered *or* discovered, per
kind** — the **Work container registry** is where a kind records membership it
cannot derive, not a census every kind owes.

**Relevance is a tier stamped at `Load`, never computed in `Less`.** `Less` is
pure over two containers and cannot consult a cwd, so the caller wires the adapter
with the reader's resolved checkout and project label, `Load` stamps each
container's tier, and the comparator reads only what is stamped — which keeps
`work` free of any notion of a checkout. Tiers: the checkout the reader stands in,
another checkout of the same project, everything else; alphabetical by id within
each. Page B applies no filter at all, so the "outside a project" special case
disappears: everything is listed, ordered by how likely it is to matter.

**Project routines are the same kind**, carrying a `project:<name>` id and a badge
cell, always tier 1 and never an **Advance candidate** (no schedule ⇒ no consent).
Rejected: a fourth Work kind, which would open the closed enum for a display
distinction.

**A Routine that cannot be read is a container, not a warning.** `Load` has no
warnings channel: an unloadable Routine comes back with a kind-local `BROKEN`
label and its parse error in its **Detail sections**. Erroring instead would blank
a whole page over one bad file, and a warnings side channel puts the thing a reader
must fix somewhere other than where they are looking. `Load`'s error return covers
directory-level failures only, as it always did.

## Consequences

- **The warnings field and its rendering are gone.** `DashboardSnapshot` carries
  rows and nothing beside them. The discovery walk still reports per-file failures
  to its callers — the advance seam turns each into a refusal verdict, and the
  `pop routine list` CLI table still prints its warning lines — but no surface
  renders a channel *beside* a list any more.
- **One derivation, two projections.** The Routine table and the Work container
  are both projected from a single per-read derivation, so status, last run and
  bound directory can never disagree between the two. The container carries
  render-ready cells (a labelled schedule, `(missing)`) while the table keeps the
  raw values its modals prefill from — which is why neither projects from the
  other.
- **A Routine outlives its directory.** A bound directory that is gone shows
  `(missing)`, lands in the outermost tier — nobody is standing in a directory that
  does not exist — and resolves no checkout, so a shell or a fire refuses by name
  rather than landing somewhere arbitrary. It is never pruned: deleting authored
  work because a checkout moved is not pop's call.
- **Project routines move to the top of page B.** Today they are appended last;
  under tier ordering they lead. A deliberate change, and the reason ordering is
  worth stating: the routines you can act on where you are should be the ones you
  see first.
- **A Routine's items are its runs.** They come off the store on the same read the
  container does, which is what lets the generic detail view replace the hand-rolled
  run-history frame. A run's file is the report it wrote, falling back to the path
  that report would have taken, so an item always points somewhere its reader can
  open without knowing this package's layout.
- **The supervisor's special case is one slice from dying.** The Routine adapter
  now wears both seams, so the appended advancer entry becomes an ordinary member
  of the read wiring list the moment page B is wired — at which point the append
  goes away.
- **Relevance is display, never scheduling.** A tier decides where a row sits and
  nothing else: the daemon's candidate read stays global and cwd-independent, so
  the machine fires the same Routines whatever directory anyone is standing in.

## As landed (page B)

- **Each page has its own wiring list.** Page B is wired from `Deps.RoutinePageKinds`
  rather than by adding the Routine kind to `WorkKinds`, because `WorkKinds` is also
  what the supervisor derives its advancers from and what `pop queue status` prints:
  folding the Routine in there would have doubled its advance entry (the appended
  one is still there) and put Routine rows in the static Task-set table. The
  supervisor's appended advancer therefore survives this slice and dies with the
  status surface's own split.
- **Relevance tiers are wired at the page, not at the tick.** The Routine page
  resolves the reader's checkout once per rebuild and hands it to the adapter; the
  supervisor leaves it unwired, so a tick still pays nothing for a fact it never
  reads.
- **The status label carries a tone, not a colour table.** The kind tags its own
  status word with one of the seam's attention levels — a live or finished run good,
  a pause warning, a failure or an unreadable definition bad — so the render layer
  paints page B's STATUS cell through the same segment walk as page A's, and the
  retired TUI's private colour switch is gone.
