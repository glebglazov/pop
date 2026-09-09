---
fragment: C78021CD
generation: 0060
branch: master
---

+ Half-removed checkout
  A directory left where a checkout's removal stopped part way: its `.git` file
  is gone, so git refuses every later `git worktree remove`, while the directory
  and some of its contents remain.
  avoid: orphan worktree, stale worktree, zombie worktree, broken worktree
  under: Language

+ Checkout removal
  Pop's own act of deleting a checkout: a recursive delete that does not stop at
  the first entry it cannot remove, then `git worktree prune`, then the checkout's
  **History** entry. It finishes a **Half-removed checkout** instead of refusing
  one, and it names what still holds the directory rather than refusing on its
  account.
  avoid: worktree delete, git worktree remove
  under: Language

+ Errand
  A single act pop performs for the human in the background, running no coding
  agent — a **Checkout removal** is one. It records its subject and not a plan,
  so the **Pop daemon** works out what to do when it runs rather than when it
  was queued.
  avoid: chore, job, background task, operation
  under: Language

+ Errand failure
  The record left when an **Errand** does not finish: one per subject,
  overwritten by a retry, carrying a pointer to the document holding the
  errand's output and the verbs to retry or dismiss it. An **Errand** still
  marked running when the **Pop daemon** starts is one of these, with the
  interruption as its output. An **Errand** that finishes leaves no record but
  its **Work journal** line.
  avoid: failed job, error row, dead letter
  under: Language

+ Pop daemon
  The single process behind `pop daemon run`, holding one lock, one journal and
  one store. It has two halves: **Errand**s, which a picker may start because
  the human asked for each one by name, and the **Work daemon**, which never
  auto-starts.
  avoid: chore daemon, background daemon, queue daemon
  under: Language

~ Work daemon
  The agent-running half of the **Pop daemon**, and no longer a process of its
  own. `pop work daemon` stays as a backward-compatible alias for this half,
  deliberately unlike `pop queue`, which was deleted outright. It keeps
  everything that made it explicit — it is the half a picker may never
  start, because parking it in a pane is how the operator watches it run coding
  agents unattended across projects — along with its `[work.daemon]` timing
  (`poll_interval`, `agent_quota_retry_after`, `crash_retry_delays`), its
  reconciliation of in-flight drains from live **Runtime execution lock**s, and
  its `pop work status` / `pop work log` / `pop work cooldowns` readers.
  was: The supervisor process behind `pop work daemon` — foreground despite the name: explicit, never auto-started from a picker, because it runs coding agents unattended across projects; the operator parks it in a pane and Ctrl-C (SIGINT) is graceful shutdown. It is single-instance via a PID/lock file at <data>/pop/work/supervisor.lock, beside its narration log; because a pre-cut daemon holds the old queue-named path invisibly to a post-cut binary, startup reads both paths for liveness and refuses naming whichever file is held. Its stdout lines are prefixed `work:`, and its timing is configured under [work.daemon] (poll_interval, agent_quota_retry_after, crash_retry_delays) — a leftover [queue] table is an unknown section, not an alias. Unlike the Monitor daemon, it needs no control socket: it persists agent cooldowns and drain lifecycle to the SQLite store, from which parked sets, backoff, and the Work journal are derived, so `pop work status` and `pop work log` are pure store readers. On start it reconciles in-flight drains from live Runtime execution locks, so a restart never disturbs them. Its command surface is `pop work daemon`, `pop work status`, `pop work log`, and `pop work cooldowns` — `pop queue` is deleted with no alias; Ctrl-C is stop, and there are no service-management verbs because foreground-and-explicit is the point.
