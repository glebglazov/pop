# Queue is a parallel per-project supervisor, not a serial global scheduler

Status: accepted

ADR-0013 reserved the word "queue" — and CONTEXT.md restated it — for "a future machine-global
scheduler that picks the next Task set across *all* projects by priority and runs it": a single
serial scheduler imposing one global priority order. Building the feature, that shape turned out
to be the wrong one. `pop queue` is instead a **parallel per-project supervisor**: a foreground
daemon (`pop queue run`) that, every poll interval, fans `pop tasks implement <set>` out across
all registered projects — concurrent *across* projects, serial *within* each. Global
cross-project priority ordering is an explicit non-goal. This ADR spends the reserved word on
that meaning and supersedes the reservation's serial-global intent.

## Why parallel-per-project beats serial-global

The **Runtime execution lock** already draws the real concurrency boundary: it "prevents
concurrent task execution in one checkout while allowing unrelated projects ... to execute
concurrently." A serial global scheduler would *throw away* that latent parallelism — three
projects with ready work would drain one at a time for no reason, since they share no checkout
and no lock. Per-project fan-out simply uses the parallelism the lock already permits. The lock
also makes per-project serialization free and correct, so the supervisor never has to coordinate
within a project — it just ensures at most one drain per idle project.

A global priority order across projects is also not a thing the user actually wants: priority is
already meaningful *within* a repository (Task set priority chooses among a repo's ready sets),
but ranking project A's set above project B's is comparing unrelated work. Dropping global
ordering removes a coordination problem that bought nothing.

## "Picked-up" is derived from the runtime lock, never a persisted status

A supervisor must know which sets are already running so it doesn't re-dispatch them. We
deliberately do **not** persist a running/`in_progress` status — the same reason the executor
rejects `in_progress` (it goes stale after a crash, and a Task set carrying it is treated as
Malformed). Instead the runtime lock is enriched with the running set's identifier, and
"picked-up" is *derived* from a live lock (PID-alive, already self-healing for stale PIDs). The
lock — owned by the executor, not the daemon — is therefore the single source of truth: it
survives daemon restart, catches human-run drains, and makes a raced double-spawn harmless (the
loser exits "already in progress"). tmux panes are a display surface only, never the source of
truth, because pane-existence does not imply execution and session names are only approximate
(ADR-0005).

## Foreground, not a detached daemon

Unlike the Monitor daemon (ADR-0001/0021), which auto-starts detached because every picker needs
it, the Queue runs in the **foreground** and is started only by explicit `pop queue run`. It
launches coding agents that edit and commit code unattended across every registered project, so
it must never auto-start from a picker, and the operator wants to park it in a pane and Ctrl-C
it. Consequently it needs no control socket: it persists agent cooldowns, parked sets, and a
Queue journal to disk, and `pop queue status` / `pop queue log` are pure file readers.

## Considered options

- **Honor the reservation: build the serial global scheduler.** Rejected — it discards the
  cross-project parallelism the runtime lock already allows, and a global priority order across
  unrelated projects is not meaningful work the user wants.
- **Keep "queue" reserved-as-serial and name this `pop tasks daemon`/`watch`/`serve`.** Rejected
  — "queue" is the intuitive word for "the thing that works my backlog," and the parallel
  supervisor is the feature that word was always going to name in practice.
- **Track "picked-up" as a persisted Task set status.** Rejected — stale-on-crash, the exact
  failure that already bars `in_progress`.
- **Detached background daemon with a control socket (Monitor's pattern).** Rejected for v1 —
  the operator runs it foreground and Ctrl-Cs it; file-based state is less machinery than a
  socket and survives restart.

## Consequences

- The reserved-term note in CONTEXT.md is replaced by a full **Queue** glossary entry (parallel
  per-project supervisor); related terms (Picked-up Task set, Queue daemon, Queue agent
  fallback, Queue journal, Queue backoff, Queue scope) are added.
- `pop tasks implement` gains a machine-readable **drain-outcome record** (set, outcome,
  exhausted preset). Today a quota pause exits `0`, indistinguishable from success; the
  supervisor's agent-fallback and crash-backoff both depend on telling these apart, so this
  signal is required new plumbing in the executor.
- Worktree-level parallelism *within* a project is deliberately out of scope and parked in
  `docs/queue-worktree-parallelism.dormant.md`; the lock-per-checkout source of truth already
  accommodates it when revived.
