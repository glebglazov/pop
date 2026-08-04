---
status: accepted
relates: "moves the last standalone machine-local state file into the store of [ADR-0055](0055-drain-execution-lifecycle-is-a-durable-store.md)/[ADR-0118](0118-execution-state-store-handle-is-process-cached-and-liveness-injected-at-open.md) through the shared handle of [ADR-0140](0140-sibling-packages-borrow-the-process-cached-store-handle-through-tasks-deps.md), and gives the handoff verbs of [ADR-0158](0158-dashboard-verbs-split-by-whether-they-hand-off-and-say-so-in-the-key-case.md) a side effect they were missing"
---

# History records every human landing, and moves into the execution-state store

## Context

**History** is a flat `history.json` in pop's data dir — `{path, last_access}`
entries — and every write site lives in `cmd/`: the project picker, `pop project
switch`, the worktree picker, the monitor dashboard. No package outside `cmd/`
imports it at all.

Meanwhile most of the sessions pop opens are opened from somewhere else. Map
sessions (`pop map open`/`assist`/`next`/`fan-out`), every task-set pane (drain,
verify, assist, fold, runtime shell), the routine refinement spawn and
`pop workbench apply` all create real tmux sessions rooted at real checkouts, and
none of them record. The sharpest symptom is two dashboards with one gesture: the
monitor dashboard records when it moves you into a pane, the **Work dashboard**
does not. The second is a trunk checkout ageing out of the **Project picker**
while you have been living in that trunk's Map session all week.

The obvious fix — record on pane creation — has a trap. The **Work daemon**
spawns unattended drains around the clock, so "pop created a pane here" would
make the picker's recency ordering a report on what the daemon did overnight
instead of where its user has been. Both readers of History (`SortByRecency`,
`session_last_visit`) exist to answer the latter.

Storage is the other half. `SaveWith` is a non-atomic whole-file rewrite with no
lock and every caller is best-effort. That survives today only because the writers
are all foreground `cmd/` paths that run one at a time; adding dashboard and
spawn-site recording — `pop map fan-out` loops over a whole frontier — makes lost
writes routine.

## Decision

1. **History records human landings.** The predicate is: did this act put the
   human into a session or pane? A **Switch**, a window open, a cd-to-pane
   landing, every **Handoff verb** on the Work dashboard, `pop map open`/`assist`/
   `next`, and the routine refinement spawn all record. What the Work daemon
   spawns unattended never does.
2. **A manually launched drain, verify or fold records too.** The line is
   manual-versus-daemon, not human-work-versus-machine-work: you pressed a key and
   tmux moved you into that checkout, which is exactly the fact History exists to
   hold. A task-set pane records the set's **Runtime path** — the checkout you are
   now sitting in — not its trunk.
3. **The write goes at the handoff chokepoint**, `handoffAfterLaunch`, plus the
   `cmd/` entry points that hand off without passing through it (`pop map open`/
   `assist`, the routine refinement spawn). "Handed the human off" is precisely
   what that function means, so one site covers every dashboard verb instead of
   eight sites covering each spawn. History stays out of `wayfinder`, `tasks` and
   `routine`; no history seam is added to their deps.
4. **A Map session records its Trunk worktree.** A `pop-map-<id>` session has no
   checkout of its own, and the trunk is where the code under study lives.
5. **History moves into the execution-state store.** `Record` becomes a
   single-row upsert in a transaction on the handle the process already caches.
   `history.json` is folded in once on first read and then ignored — not deleted —
   with its removal tracked in `CLEANUP.md`.

## Consequences

- **Atomicity stops being a discipline and becomes a property.** There is no lock
  to remember to take and no whole-file rewrite to lose, and a one-row upsert is
  cheaper than the rewrite it replaces, which is what makes it safe to record from
  a loop like `fan-out`.
- The picker's startup path now opens `pop.db` where it may not have before. The
  handle is process-cached and lazily opened, so the cost is one open per process;
  if that ever shows up in picker latency it is a real regression to measure, not
  a surprise to explain.
- **Recording a Map session against its trunk is a small lie** — you were in the
  Map session, and History will say you were in the trunk. Accepted deliberately:
  the alternative pseudo-path (`tmux:<session>`, which the monitor dashboard
  already synthesizes) keeps the truth but is useless to both readers, which key
  on checkout paths. ADR-0185 already nests Map sessions under their project, so
  the attribution is consistent with what the picker shows.
- Daemon activity remains invisible to recency ordering. If that turns out to be
  the wrong call, widening the predicate is a one-line change at a known
  chokepoint — but it cannot be narrowed again once the file is polluted, which is
  why this starts at human interaction only.
- `--no-history` keeps working where it exists. The Work dashboard gets no
  equivalent flag.
