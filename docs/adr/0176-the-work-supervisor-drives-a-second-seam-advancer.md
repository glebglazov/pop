---
status: accepted
---

# The Work supervisor drives a second seam, `Advancer`, separate from the read one

## Context

Ticket 05 of the *generalize-work* map asked what the Queue daemon becomes once
Maps, Task sets and Routines are all Work behind one **Work kind** seam
([ADR-0173](0173-work-is-one-kind-interface-with-data-shaped-returns-and-kind-side-adapters.md)):
which units may be advanced without a human, what the runner interface is, and
whether a Map grows an auto-advance bit mirroring **Auto-drain**.

As found, `tick` was two unrelated pipelines sharing a timer. The Task-set half
scanned, produced typed decisions, then dispatched and spawned; the Routine half
was one inline pass that checked schedule, pause, fingerprint and liveness and
fired, with two database writes mid-scan and no decision data in between.
Consent was per-kind and unrelated: a Task set's `auto_drain` bit, a Routine's
`IsScheduled() && !Paused`, and a Map with no daemon presence at all.

The tempting move was to grow `Kind` a few more methods. `Kind` is consumed by
the Work dashboard and the status surface — surfaces that read and render and
never advance — so every read surface would then carry methods it must never
call, and every kind would have to answer them.

## Decision

**Advancement is its own interface, obtained by type assertion on the same
kind-side adapter.**

```go
type Advancer interface {
    Reconcile() error
    Candidates() ([]Candidate, error)
    Advance(Candidate) (Outcome, error)
}
```

The Map adapter implements `Kind` and **not** this — the concrete proof the split
is load-bearing rather than speculative. The method is `Advance` rather than the
`Perform` the wayfinding wrote, because a Go type cannot carry two methods of one
name and `Kind.Perform(Container, *Item, Verb)` is the verb seam every kind
already implements. The vocabulary difference is the useful half of the accident:
a verb is performed, a candidate is advanced.

**Maps never auto-advance; the advanceable kinds are {Task set, Routine}.**
Rejected from the ticket's own framing: resolving a **Decision ticket** is a
human-opened session, and a daemon spawning grilling sessions unattended is not
wanted. So there is no map auto-advance bit, and the AFK/HITL ticket-type
distinction stays a within-session concern.

**Consent is not in the seam at all.** Both advanceable kinds already have a
working consent model and the two mean different things, so consent is an *input*
to `Candidates()`: a kind simply does not surface a non-consented item, and the
supervisor never learns it exists. There is no generic consent bit on the
`(kind, id)` **Work container registry**; `auto_drain` stays a Task-set column. A
supervisor-level consent concept could only be a lowest common denominator, and
pausing a Routine already expresses withdrawal in a way an `auto_drain`-shaped bit
does not.

**`Candidates()` is pure, and reconciliation becomes an explicit phase.** The
crash-detection pass leaves every read path and becomes `Reconcile()`, which the
supervisor calls before asking for candidates. Worktree routing — which
provisions — moves the other way, out of the candidate read and into dispatch.

**Refusals cross the seam as verdicts**, because the supervisor's output is
mostly *why nothing ran*. One `Advance` handles both verdicts: starting the work
and recording a refusal ride the same call, which is what a Routine's overlap
skip and fingerprint-drift pause need — they are dispatch-phase writes that
happen to say no. Rejected: advances-only with each kind reporting its own
refusals internally, a smaller seam that costs the daemon its reporting.

**Checkout occupancy is the one invariant the supervisor enforces centrally**,
over `Candidate.Checkout`, opt-in per adapter: an adapter whose advance mutates a
tree fills it, and one that does not leaves it empty and is never blocked. The
mechanism is the existing **Checkout claim** ([ADR-0135](0135-admission-to-a-checkout-is-gated-on-the-checkout-claim-union.md)),
not a new per-tick concept, and enforcement is on both sides — the Task-set
adapter keeps computing it for its own deferral display, the supervisor enforces
it as the cross-kind backstop so a new occupying kind cannot forget. Not two
sources of truth: the store's liveness-backed union is the truth and both are
pure reads of it.

## Consequences

- **No read surface heals state any more.** `pop queue status` and the Work
  dashboard perform no writes, which amends
  [ADR-0055](0055-drain-execution-lifecycle-is-a-durable-store.md)'s
  "every layer-2 reader runs a cheap bounded reconcile before reading". The daemon
  is now the only healer. Nothing reads worse for it: every store read that
  matters is already liveness-filtered — a dead owner's drain row, waiter or gate
  hold does not block admission and does not render as live — so reconciliation
  is garbage collection, and a machine that never runs the daemon only keeps
  crash-shaped rows around longer. The one visible lag is Drain-history-derived
  backoff and parking, which are the daemon's own inputs.
- **`Start` stays kind-local.** Pane titles, session resolution, worktree routing
  and routine firing are irreducibly kind-specific. The supervisor sequences,
  isolates errors, reports, and enforces the one invariant above.
- **The Task-set advance implementation stays in `queue` for now**, composed onto
  the read adapter rather than living beside it, because the drain pipeline — the
  scan, the deferral selector, routing, the tmux spawn — is there. Both wiring
  lists build the Task-set kind through `queue` so the supervisor can never be
  handed a task-set kind that cannot advance. The package split moves the pipeline
  once, not twice.
- **Reporting generalizes; the view diff does not.** The Task-set run-output diff
  is thoroughly Task-set-shaped — it diffs a snapshot to avoid re-announcing state
  each tick — and stays kind-local, driven by the Task-set adapter. Generalizing
  it would make every kind grow a snapshot type. Until the supervisor emits
  structured per-advance events, a Task-set refusal is reported only by that diff,
  which is why advancing one prints nothing: the alternative is printing every
  deferral twice, every tick.
- **A candidate is not durable.** It describes the tick it was read in, and the
  kind that produced it is what resolves it back to its own coordinates at
  dispatch — a candidate replayed into a later pass is refused as not that pass's.
