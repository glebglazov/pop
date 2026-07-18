---
status: accepted
---

# Execution-state store handle is process-cached and liveness is injected at open

## Context

ADR-0055 made the **Execution-state store** the durable home of layer-2 facts,
but the seam around it stayed thin. Every helper opened a fresh SQLite handle —
connection, WAL setup, and a forward migration run per operation, 43 open sites
in `tasks/` alone — so "when is the store open" was re-decided everywhere, and
`store.OpenCount()` existed purely so a test could assert the number stayed
bounded. Store methods that touch owner PIDs (`StartDrain`, `ReconcileCrashed`,
`ReconcileGateHolds`, `ReconcileSpawnIntents`, `MutateIfCheckoutQuiescent`)
each took an `isAlive` closure, re-wired identically at ~10 callsites in two
signatures (`func(Drain) bool` and `func(pid int, procStart string) bool`) for
one concept — the PID-reuse defense leaked across the seam to every caller.

The **Worktree binding** rows crossed three identical four-field types
(`store.Binding` → `tasks.BindingEntry` → `binding.Binding`) with two
hand-written converter pairs adding nothing but renames. Worse, the
`binding.Store` in-memory map façade did Load → mutate → Save, where Save is a
whole-table `ReplaceAllBindings` — reintroducing, one layer above the
transactional store, exactly the read-modify-write race ADR-0055 existed to
remove: a concurrent **Bind worktree** and Queue provision could clobber each
other's rows. Finally, `queue` carried its own copy of the
bound-checkout-else-representative rule (`verifyRuntimeForSet`) kept in lockstep
with `pop tasks status` by comment convention.

## Decision

- **One process-cached handle.** `tasks.Deps` holds a lazily-opened cached
  `*store.Store` behind an exported accessor with two modes — create-if-needed
  and if-exists — preserving the invariant that pure readers never materialise
  an empty database. `guardTestStorePath` moves inside the accessor (one
  chokepoint). Migrations run once per process. `DrainHandle` borrows the
  shared handle; `Finish`/`Cancel` stop closing it. A `CloseStore` exists for
  the queue daemon and test cleanup; one-shot CLI runs rely on process exit
  (WAL-safe).
- **Liveness is a construction-time policy.** `store.Open(path string, alive
  store.Liveness)` with `type Liveness func(pid int, procStart string) bool` —
  a required parameter, wired once from `Deps.ProcessAlive` +
  `ProcessStartToken`. Store methods drop their `isAlive` parameters; the
  Drain-shaped variant becomes a private adapter inside `store`.
- **`store.Binding` is the one binding type.** `tasks.BindingEntry`,
  `binding.Binding`, and both converter pairs are deleted. `tasks/binding`
  keeps the behaviour (key shape, adopt, provision, teardown, project
  detection) and talks to the store through the Deps accessor.
  `queue.WorktreeBinding` stays as an alias of `store.Binding` to avoid test
  churn (removal tracked in CLEANUP.md §D2).
- **The `binding.Store` map façade is retired.** Callers use keyed store ops;
  `ReplaceAllBindings` dies with its only caller. The adopt path gets a
  transactional `store.PutBindingIfAbsent` so "never clobber an existing
  binding" holds under concurrency — closing the reintroduced race.
- **`verifyRuntimeForSet` moves to `binding.RuntimeForSet`.** Queue's wrapper
  around `tasks.ApplyVerifyVerdictsWith` dies; the resolution rule has one home.
- The legacy `bindings.json` migration moves into `tasks/binding`; its sunset
  is a CLEANUP.md §D row (beta-tester sign-off), not part of this change.

## Considered options

- **Functional option with a nil-alive default** for liveness — rejected: a
  forgetful caller would silently disable crash healing; a missing predicate is
  a programming error and should fail at construction.
- **Keep per-call opens behind one funnel accessor** — rejected: the
  migration-and-connection cost per operation remains, and the open lifecycle
  stays smeared across callsites.
- **Keep `binding.Binding` as the domain type** (one converter pair) —
  rejected: the `ScopedKey` field riding along on `store.Binding` is harmless,
  and zero converters beats one.

## Consequences

- The cached handle plus `SetMaxOpenConns(1)` means a leaked `rows` cursor now
  blocks every later query in the process, where per-call opens bounded the
  damage. Accepted: the store's row helpers already close eagerly; the
  discipline is enforced at the store seam, not at callers.
- `store.OpenCount()` and its bounded-opens test lose their purpose (opens drop
  to at most one per process) and are updated or removed.
- ADR-0055's open-per-operation reading and its "binding store … wraps it in
  its own façade" wording are superseded by this ADR; its layer-1/layer-2 split
  and single-writer transactional design are untouched.
- ADR-0048 (deferred) keeps its module boundary — `tasks/binding` owns binding
  behaviour — but its binding rows are now store-typed; the mirror-type
  ceremony it inherited is gone.
