---
fragment: 7C3E1A94
generation: 0031
branch: master
---

+ Agent spend cap
  The **Agent proceed verdict** flavour in which the provider is willing and the
  account is not: the agent's own service refuses because a spending limit
  somebody else controls has been reached. Preset-scoped, so **Agent fallback**
  walks past it to the next agent and the drain does not stop. Unlike an **Agent
  quota pause** the provider names no moment it will lift, and unlike an **Agent
  authentication failure** it is not left to a human either: pop supplies its own
  hour and treats the preset as merely cooling, so the cap enters the ordinary
  **Agent quota recovery wait** and the preset rejoins the walk when the hour is
  up. Hitting it again simply starts another hour. This is the one cooldown pop
  invents rather than reads — every other reset instant is one a provider stated
  — and it is a deliberate guess: an hour is short enough that a cap raised over
  lunch costs almost nothing, and a cap nobody ever raises costs one refused
  invocation an hour, which is cheaper than a human being needed to notice.
  avoid: quota pause, billing error, credit exhaustion, human-healing stop
  under: Agent proceed verdict

~ Agent fallback
  The unattended agent-choosing policy of a **Work group**: each kind takes an
  ordered list — repeated `--agent` flags, else its group's
  `[work.<group>].agents`, else the built-in `claude` — and runs on the first
  live entry, walking to the next on anything that stops the current one: a
  preset-scoped **Agent proceed verdict** (a quota pause, an **Agent spend
  cap**, an **Agent authentication failure**, a missing binary) *and* a preset
  that simply spent its whole **Task retry cap** without finishing. Every kind
  walks on both classes. The retry-cap class used to be verify's alone, an
  asymmetry with implement that is now resolved in verify's favour: a preset
  that could not finish a task is evidence about that preset as much as about
  the task, and the next agent is owed its turn before anybody concludes the
  work is impossible. Verify, review and routines reach the built-in through
  `[work.implement].agents`, and only while their own list is *absent*: an
  **Override config layer** entry of `agents = []` states the group has no
  agents of its own, and all three refuse the run with a setup error naming the
  key instead of walking on — the one empty state that disables the
  fallthrough, agreeing with what the **Config dashboard** promises for it
  (ADR-0202 decision 6). An empty list in a hand-authored file is not that
  statement and still falls through. Naming an agent per run — `--agent`, or a
  set's own `verifier` / `reviewer` directive, or a Routine's manifest list —
  outranks the emptiness and runs. A machine-global cooldown store records
  quota pauses per preset; a human-healing verdict writes no cooldown. Every
  kind reads that store before invoking a preset, and skips a cooling one as a
  quota pause carrying the recorded reset — implement, routines, verify and
  code review alike, so a preset the last hour already proved exhausted is
  never re-invoked to be told the same thing. Attended sessions have no such
  policy: they cannot switch mid-session and get an **Attended launch-time
  skip** instead.
  avoid: retry, agent rotation, [queue].agents, [tasks.implement].agents, [workload] default_agents
  was: The unattended agent-choosing policy of a **Work group**: each kind takes an ordered list — repeated `--agent` flags, else its group's `[work.<group>].agents`, else the built-in `claude` — and runs on the first live entry, falling through on any preset-scoped **Agent proceed verdict**: a quota pause, an **Agent authentication failure**, or a missing binary. Verify, review and routines reach that built-in through `[work.implement].agents`, and only while their own list is *absent*: an **Override config layer** entry of `agents = []` states the group has no agents of its own, and all three refuse the run with a setup error naming the key instead of walking on — the one empty state that disables the fallthrough, agreeing with what the **Config dashboard** promises for it (ADR-0202 decision 6). An empty list in a hand-authored file is not that statement and still falls through. Naming an agent per run — `--agent`, or a set's own `verifier` / `reviewer` directive, or a Routine's manifest list — outranks the emptiness and runs. Never on ordinary task failure. A machine-global cooldown store records quota pauses per preset; a human-healing verdict writes no cooldown. Every kind reads that store before invoking a preset, and skips a cooling one as a quota pause carrying the recorded reset — implement, routines, verify and code review alike, so a preset the last hour already proved exhausted is never re-invoked to be told the same thing. When every entry is human-healing unavailable, implement exits `ExitSetup` and the task stays Open. Verify's list falls through on one more class — a preset's exhausted retry loop — an asymmetry with implement that is deliberate and unresolved. Attended sessions have no such policy: they cannot switch mid-session and get an **Attended launch-time skip** instead.

+ Drain full stop
  The only three endings that stop a **Drain** rather than move it along: the
  work finished, the human interrupted it, or the **Agent fallback** walk ran
  out of agents. Nothing else is allowed to end a drain — not a failed
  **Task attempt**, not a spent **Task retry cap**, not any one agent refusing
  to run — because every one of those still leaves an untried agent, and
  stopping in front of an untried agent is the outcome the whole policy exists
  to prevent: a human returning to find that nothing drained overnight for a
  reason the next agent in the list would have shrugged off. It is scoped to
  the whole drain, not to one task: a walk exhausted on one task stops the
  drain rather than moving to the next task, because a set that cannot get an
  agent for one task will not get one for the next either, and grinding every
  remaining task through the same dead list wastes the same discovery many
  times. It is a statement about exhaustion, not about blame: reaching a full
  stop says every avenue was tried, never that the task was impossible. An
  unattended full stop parks at its gate like an attended one — a **Checkout
  gate hold** is set-scoped unless the tree is dirty, so parking a drain whose
  agents were merely unavailable blocks no other set on the checkout.
  avoid: drain failure, hard stop, terminal disposition, give up
  under: Agent fallback

+ Unstartable walk
  An **Agent fallback** walk in which no agent could be invoked at all — every
  preset cooling, spend-capped, unauthenticated, or absent from PATH. It ends
  the drain like a **Drain full stop** and is deliberately not the same event:
  nothing was attempted, so nothing was learned about the work, and neither the
  task nor the **Task set** has failed. The drain is a no-op — the tasks stay
  as they were, ready for the next drain to find an agent that will run. The
  distinction matters because the two look identical from the outside and mean
  opposite things: one says every agent tried and could not finish, the other
  says the machine had no agent to try.
  avoid: exhausted walk, failed drain, empty agent list, setup error
  under: Drain full stop

~ Task attempt timeout
  The maximum duration for one task attempt, defaulting to 45 minutes and
  configurable per command. When exceeded, the task executor terminates the
  agent process group and preserves partial changes. It is its own outcome, not
  the non-zero-exit path: it consumes one slot of the **Task retry cap** and
  carries the ADR-0040 "continue" digest forward — on implement retrying
  INSTANTLY, with no **Task attempt retry delay**, unlike an incomplete-
  assessment failure. A timeout almost always means execution simply ran too
  long (typically an oversized context window), and a fresh attempt restarts
  from the compact prior-attempt digest rather than the bloated transcript, so a
  wait would add nothing. A spent cap hands the turn to the next agent like any
  other exhaustion; only a timeout that survives every agent in the walk marks
  the task Failed, because nine attempts' evidence that the work does not fit in
  one attempt is not something a later drain fixes — what it needs is the task
  split. On verify a timeout stays a delayed retry (the instant-retry rationale
  is implement-specific — a Verifier timeout is more likely a real hang).
  Distinct from **Agent quota pause** (clean stop, recovery wait) and from
  **Interrupted task** (SIGINT, no progress record).
  avoid: Task set timeout, interruption
  was: The maximum duration for one task attempt, defaulting to 45 minutes and configurable per command. When exceeded, the task executor terminates the agent process group and preserves partial changes. The outcome is retry-eligible: it consumes one slot of the Task retry cap and carries the ADR-0040 "continue" digest forward — but on implement it retries INSTANTLY, with no Task attempt retry delay, unlike an incomplete-assessment failure. A timeout almost always means execution simply ran too long (typically an oversized context window), and a fresh attempt restarts from the compact prior-attempt digest rather than the bloated transcript, so a wait would add nothing; genuine transient API failures surface instead as an Agent quota pause with its own recovery wait. On verify a timeout stays a delayed retry (the instant-retry rationale is implement-specific — a Verifier timeout is more likely a real hang). Only after the cap is spent does the executor mark an Exhausted task, append a Failed progress record, and stop at the Failed gate prompt. Distinct from Agent quota pause (clean stop, recovery wait) and from Interrupted task (SIGINT, no progress record).
