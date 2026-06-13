---
fragment: 3c0baea3
branch: master
---

~ Queue
  A daemon that supervises per-project Task-set draining, fanning `pop tasks implement <set>`
  runs out concurrently across registered projects into tmux. It targets one specific
  not-currently-running Ready set per idle project (never no-arg, which would re-pick a
  running set). Each project drains serially by local Task set priority (enforced by the
  Runtime execution lock); projects run in parallel. Global cross-project priority ordering
  is a non-goal.
  was: (reserved, not implemented) Reserved for a future machine-global scheduler that picks the next Task set across all projects by priority and runs it. Do not use "queue" for today's per-repository scheduling; it has no current definition.
  under: Tasks

+ Picked-up Task set
  A Task set currently being drained, identified by a live Runtime execution lock that records
  its Set identifier. Derived from lock liveness (PID alive), never a persisted task status —
  the same reason an `in_progress` task status is rejected. The Queue reads this to choose the
  next not-running set; tmux panes are display only, not the source of truth.
  under: Tasks

+ Queue daemon
  The supervisor process behind `pop queue run`. Foreground and explicit (never auto-started
  from a picker, because it runs coding agents unattended across projects); the operator parks
  it in a pane and Ctrl-C (SIGINT) is graceful shutdown. Single-instance via a PID/lock file.
  Unlike the Monitor daemon it needs no control socket: it persists its state (agent cooldowns,
  parked sets, backoff timers) and a Queue journal to disk, so `pop queue status` and
  `pop queue log` are pure file readers. On `run` it reconciles in-flight drains from live
  Runtime execution locks, so a restart never disturbs them. Command surface: `run`, `status`,
  `log` (no `start`/`stop` — Ctrl-C is stop).
  under: Tasks

+ Queue scope
  The set of work the Queue daemon supervises: all registered projects' Ready Task sets.
  Starting the daemon (`pop queue start`) is the standing unattended-AFK consent — there is no
  per-project opt-in flag. The blast radius is self-limiting because the daemon only acts on
  Ready sets, and a Task set is a deliberately authored artifact; a project with no sets is
  skipped. The per-set opt-out is Archive (Archived sets are excluded from selection). When a
  project has no tmux session, the daemon creates one detached, ensures a `pop-queue` window,
  and spawns the drain pane there; finished panes are kept (not auto-closed) as a visible log.
  under: Tasks

+ Queue journal
  The durable append-only record in pop's data dir of every Queue drain event — started, done,
  failed, HITL-blocked, quota-paused-and-agent-switched, crashed, backing-off, parked. It is
  emitted by `implement` as a structured drain-outcome record (carrying set id, outcome, and
  the exhausted preset when relevant) that the daemon consumes to drive Queue agent fallback
  and backoff, and persists for observability. `pop queue status` reads live state (picked-up
  sets, cooling agents, parked sets, idle projects); `pop queue log` reads the journal history.
  under: Tasks

+ Queue backoff
  The daemon's response to an abnormal drain exit (crash, kill, interrupt) — which, unlike a
  clean failure or quota pause, leaves the set Ready with nothing cooled and would otherwise
  re-spawn immediately and thrash. The daemon applies an escalating per-set delay and, after N
  consecutive abnormal exits, parks the set: stops re-spawning it until a human clears it. A
  clean exit resets the counter. Distinguishing abnormal from clean exits requires the Queue
  journal's outcome record — storage status alone cannot tell a crash from a quota pause.
  under: Tasks

+ Queue agent fallback
  The Queue's policy for choosing an Agent preset when draining, owned by the daemon (not the
  executor). It rotates a configured ordered list of agents (`[queue].agents`) as a
  *non-overriding default* (ranks below a task's pinned Agent key — pins always win, unpinned
  tasks ride the rotating default). An agent whose binary is not on PATH is skipped with a
  Queue journal note, not a startup error. A global per-agent cooldown
  (`[queue].agent_quota_retry_after`) marks an agent exhausted-until after an Agent quota pause,
  because quota is per-subscription, not per-project; it is not per-agent overridable in v1.
  Recovery is probed by simply re-attempting after that fixed interval — there is no
  quota-remaining API to query. When a *pinned* task's agent is exhausted, the Queue backs that
  whole set off until that agent's cooldown expires rather than violating the pin. Set via a
  `[queue]` config section: `agents`, `poll_interval`, `agent_quota_retry_after`,
  `crash_retry_delays` (escalating per-set waits after an abnormal exit; list length is the
  park threshold).
  under: Tasks
