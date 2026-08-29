---
status: accepted
relates: "fires the revisit trigger of [ADR-0189](0189-a-work-read-surface-pays-only-for-what-it-renders.md) and takes the shape it pre-settled; distinguishes itself from the persistent cache deleted by [ADR-0242](0242-the-dashboard-reads-truth-through-one-guarded-reload.md) decision 7"
---

# The Manifest memo persists, and the verdict loop forks in parallel

## Context

ADR-0189 gave the Work read surface a budget and declined a persisted
**Manifest memo**, on a measured inventory of ~92 sets and a first paint under
the threshold where anyone could tell. It named the condition for revisiting:
**a first paint above 100ms on a real inventory**.

Measured now, on 250 registered sets, opening `pop work dashboard`:

| | cold first paint | warm 2s poll |
|---|---|---|
| `drain.DefaultDeps` + config load | 3ms | — |
| `launchPaneFacts` (tmux + git) | 35ms | — |
| **`BuildPageSnapshot`** | **170–340ms** | **~85ms** |
| — of which git forks in the verdict loop | ~55ms | ~53ms |
| — of which Manifest memo key validation | ~19ms | ~19ms |
| — of which Manifest memo *misses* | ~170–280ms | 0 |
| **total** | **~380ms** | **~85ms** |

Both numbers are past their budget, and they are two unrelated costs that happen
to share a call stack.

The cold cost is the memo miss: a fresh process reads every task markdown on the
machine (1893 `ReadFile` calls) to validate manifests that were already validated
by the last process. ADR-0189 predicted exactly this — cold and warm differ by
6× precisely because the content key is stat-based, so a hit never opens the
markdowns at all.

The warm cost is git. `ApplyVerifyVerdictsForRendered` resolves each rendered
terminal row's checkout, and each distinct checkout costs two forks —
`rev-parse --git-common-dir` for its **Repository identity** and `rev-parse HEAD`
for its work SHA. The per-checkout cache and the **Git fact memo** both already
work; four distinct checkouts really do need eight forks. They were simply paid
one after another, ~7ms each, in a sequential row loop.

## Decision

**1. The Manifest memo persists to a Cache database, and the content key is
unchanged.** `$XDG_CACHE_HOME/pop/cache.db`, opened through the existing
`store.Open` machinery (WAL, `busy_timeout(5000)`), separate from `pop.db` so
that read-path writes never contend with authoritative ones on the one
process-cached connection of ADR-0140 — and so that `rm` is a valid repair, which
it must never be for the **Execution state store**. The name is `cache.db`, not
`manifests.db`: this is the home for every derived answer pop may recompute
rather than lose, and the manifest table is merely its first.

**2. The persisted table is keyed by set directory path, with the content key as
a column.** A hit requires the row to exist *and* its stored content key to equal
the freshly computed one; a miss overwrites the row. Keying on the content hash
instead would mint a new row on every edit to every task markdown, making the
table grow with edit history and requiring a pruning policy. Path-keying bounds
it by inventory — 250 rows — structurally, with no policy to maintain. An older
content state is never asked for again: the question is always "is this directory
as it stands *right now* already validated?"

**3. The Work supervisor writes the cache too.** It ticks continuously and
already holds the in-memory tier for the life of the daemon, so letting it
persist means a dashboard or CLI usually opens onto work already done — a cache
warmed only by the surface already paying the cost would help the second open,
not the first. The supervisor's existing opt-out from the **Git fact memo** does
not apply here: manifest validation is a pure function of files, so a tick's own
writes move the content key honestly, while a git memo spanning a tick would
answer with the repository as it was before those writes.

**4. Every cache failure is a miss.** An unopenable, corrupt, or unwritable
`cache.db` degrades to the behaviour of no persisted tier at all. A read-path
write that loses a WAL race is dropped silently. Nothing on this path may turn a
cache problem into a user-visible error.

**5. A Verdict checkout pre-pass resolves distinct checkouts concurrently.**
Before the verdict loop walks the rows, one pass collects the distinct runtime
paths of the rows that will actually need resolving — rendered, terminal, placed
— and fills the per-checkout cache with a single `fanout.Map`. The loop itself is
untouched, and every `resolveCheckout` inside it becomes a hit. The store lookups
and the decoration stay serial: the forks are the whole cost, and pulling the
SQLite connection and the `changed` flag into concurrency buys nothing.

## Consequences

The expected shapes: cold first paint drops to roughly the warm figure, since the
~170–280ms of markdown reads is what the persisted tier removes; the warm poll's
~53ms of git becomes ~14ms. The ~19ms of key validation survives both, by design.

**This is not the cache ADR-0242 deleted, and the difference is the only thing
keeping it honest.** Decision 7 of that ADR removed `~/.cache/pop/glob_cache.json`
because it was persistence with nothing to validate against: it went stale
invisibly and needed a human to delete it. Here the content key — `index.json`'s
bytes plus every dirent's size and mtime plus the name set — is recomputed and
compared on *every* serve. A stale entry is not unlikely, it is unrepresentable.
The rule this ADR asserts for anything else that lands in `cache.db` later: an
entry that cannot be cheaply re-validated against its source does not belong
there.

**Forking is only parallelized, not eliminated.** Answering `rev-parse HEAD` by
reading `.git/HEAD` and the ref files directly would take the warm poll's git
cost to ~0.1ms rather than ~14ms, and it is deliberately declined here. It means
hand-rolling ref resolution — packed-refs, worktree `.git` files, detached HEAD —
directly on the verify path, the one place a wrong SHA silently mis-grades a set's
verdict. ~14ms is already below anything perceptible. If it is ever wanted, it is
its own decision with its own ADR.

**ADR-0189's amendment is reversed on its own terms, not overruled.** That
amendment declined persistence for three reasons. Two have since changed: the
inventory is 250 sets rather than four repo groups, and the first paint is ~380ms
rather than under the visible threshold. The third — that a cross-process memo
must still validate its key before serving — still holds and is accepted rather
than argued with; it is exactly the ~19ms this decision leaves on the table.

**The glossary changes, and ADR-0189 said so.** Its amendment recorded that
**Manifest memo** keeps `avoid: manifest cache (nothing persists across
processes)`, "which stays true precisely because persistence was declined." It no
longer is. The term now describes two tiers, and the avoid-line becomes
`manifest cache (it persists, but never authoritatively)`.
