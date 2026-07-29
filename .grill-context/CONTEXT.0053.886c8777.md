---
fragment: 886c8777
generation: 0053
branch: master
---

+ Agent availability probe
  The active half of **Agent unavailability** detection: a short read-only
  command an **Agent adapter** may expose alongside its attended-assistance
  capability, asking its CLI whether it is authenticated. Run lazily the first
  time a preset is reached in an **Implement run** and memoised one-way for that
  run — a preset marked unavailable stays skipped until the process ends. Only an
  explicit positive counts (`cursor-agent status --format json` → `isAuthenticated`,
  `claude auth status` → `loggedIn`, `codex login status`); a non-zero exit,
  unparseable output, or a timeout reads as *unknown*, and unknown proceeds to
  invoke the agent, because a probe must never block real work on its own parse
  failure. `pi` and `opencode` expose no status readout and have no probe. A probe
  is not an agent invocation and writes no **Captured run**. Distinct from **Agent
  catalog** availability, which is a PATH lookup and never execs.
  avoid: agent health check, auth preflight, agent status command, doctor check
  under: Agent integrations

~ Agent unavailability
  A verdict meaning this **Agent preset** cannot do the work at all, as distinct
  from an attempt that ran and failed. **Agent quota pause** is one flavour; an
  **Agent authentication failure** is another, as is a binary missing from PATH.
  Every flavour skips the remaining **Task retry cap** for that preset and hands
  the turn to the next preset in the **Agent fallback** list; the flavours differ
  only in their **Agent unavailability recovery**. Detected on two channels — a
  passive read of the capture pop already consumes (like **Agent quota
  detection**), and an active **Agent availability probe**. The passive channel
  catches a session that lapsed mid-drain; the probe catches one already lapsed
  on arrival.
  was: "A condition detected from one agent invocation meaning this **Agent
  preset** cannot do the work at all… **Agent quota pause** is one flavour; an
  unauthenticated agent is another." (fragment 71c8a420, generation 0051) —
  detection was single-channel and read from an invocation only.
  avoid: agent error, attempt failure, agent down

~ Agent fallback
  The task executor's policy for choosing an **Agent preset**, owned by `pop tasks
  implement` rather than the **Queue**. Implement takes an ordered list of agents
  — one or more repeated `--agent` flags, else the `[tasks.implement].agents`
  config list, else the built-in `claude` — and runs each task on the first live
  agent, falling through to the next on any **Agent unavailability**: a quota
  pause, an **Agent authentication failure**, or a missing binary. It does *not*
  fall through on ordinary task failure. A machine-global cooldown store records
  quota pauses per preset; human-healing unavailability writes no cooldown. When
  every configured preset is human-healing unavailable, implement exits
  `ExitSetup` and the task stays Open. The Verifier walks a parallel list,
  `[tasks.verify].agents`, with the same class plus a fall-through once a preset's
  retry loop is exhausted — an asymmetry with implement that is deliberate and
  unresolved. When attended **Integration conflict** assistance needs an agent, it
  uses only the first entry of the implement list.
  was: "…runs each task on the first live agent, falling through to the next only
  on an **Agent quota pause**… The Verifier walks a parallel list… with the same
  quota fall-through (plus a missing-binary skip)."
  avoid: Queue agent fallback, executor agent policy, default-agent, agent pin, agent rotation, [queue].agents, [workload] default_agents

~ Agent catalog
  The readout of `pop tasks agents`: every recognized **Agent preset** with its
  binary, whether that binary is on PATH, which preset is the default, and notes
  such as attended-assistance availability. It reports what Pop owns by PATH
  lookup only and never execs agents. Authentication belongs to **Doctor**, which
  runs each preset's **Agent availability probe** — the promise this entry has
  always made, now kept. Its audience is a planner choosing an **Agent preset** as
  much as a human. Model details come from each preset's **Model source**,
  surfaced only on request.
  was: "…it never execs agents by default, and authentication or deeper health
  stays with **Doctor**." — Doctor carried no such check when that was written.
  avoid: Supported agents matrix, doctor, model catalog

~ Task retry cap
  The maximum started agent invocations per retry loop before giving up. A single
  default at `[tasks]` root (`max_tries`, default 3) applies to both implement and
  verify; `[tasks.implement]` and `[tasks.verify]` may each override their side
  independently. On implement, an explicit `--max-tries` flag wins over config.
  The cap is **per agent preset**: the executor retries the current preset up to
  the cap (with **Task attempt retry delay** between failures), then **Agent
  fallback** moves to the next preset. Any **Agent unavailability** abandons the
  remaining cap for that preset immediately and consumes no further attempts —
  the cap governs attempts that ran, not a preset that cannot run. Distinct from
  **Task attempt retry schedule** (how long to wait between tries).
  was: "…then **Agent fallback** moves to the next configured preset — on
  implement for quota, on verify for quota or after the current preset's retry
  loop is exhausted."
  avoid: max-tries flag alone, attempt count, DefaultMaxTries
