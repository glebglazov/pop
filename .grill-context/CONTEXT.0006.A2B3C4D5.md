---
fragment: a2b3c4d5
generation: 0006
branch: master
---

+ Task attempt retry delay
  The wall-clock wait inserted after a failed agent invocation and before the next try, shared by **Task attempt** retries and **Verifier** retries alike. Applies to every retry-eligible failure — for implement, the same incomplete-outcome trigger as today; for the **Verifier**, only invocation failures (timeout, agent error, unparseable output) — not a cleanly parsed NEEDS-HUMAN or FIXABLE verdict, which is the Verifier succeeding at its job. **Agent quota pause** on implement remains a clean stop with no retry loop; on verify it still falls through to the next configured agent without a delay. Timeout on either path still fails immediately with no further tries — unchanged. The default schedule is one minute after the first failure, five minutes after the second, fifteen minutes after the third and every subsequent failure when the attempt cap exceeds three; when the configured delay list is shorter than the retry count, the last entry repeats. Attempt one still starts immediately; delays apply only between retries. The wait is a blocking in-process sleep: the **Drain** and runtime lock stay held, the pane shows a countdown, and Ctrl-C during the wait exits as **Interrupted** with no further attempt. Configurable via **Task attempt retry schedule** at `[tasks]` root; distinct from **Queue backoff** (abnormal drain exits).
  avoid: retry backoff, attempt cooldown, API backoff, persisted retry schedule
  under: Task execution

+ Task attempt retry schedule
  The ordered list of duration strings at `[tasks]` root (`attempt_retry_delays`) governing **Task attempt retry delay** for both implement task retries and **Verifier** retries. Omitted ⇒ `["1m", "5m", "15m"]`. An empty list ⇒ zero delay (instant retries, restoring pre-backoff behavior). Parsed like `[queue] crash_retry_delays`: each entry is one inter-attempt wait, and once the list is exhausted the last entry repeats for every subsequent retry. Distinct from **Task retry cap** and from **Queue backoff**.
  avoid: max-tries, crash_retry_delays, retry_after
  under: Task execution

+ Task retry cap
  The maximum started agent invocations per retry loop before giving up. A single default at `[tasks]` root (`max_tries`, default 3) applies to both implement and verify; `[tasks.implement]` and `[tasks.verify]` may each override their side independently. On implement, an explicit `--max-tries` flag wins over config. The cap is **per agent preset**: the executor retries the current preset up to the cap (with **Task attempt retry delay** between failures), then **Agent fallback** moves to the next configured preset — on implement for quota, on verify for quota or after the current preset's retry loop is exhausted. Distinct from **Task attempt retry schedule** (how long to wait between tries).
  avoid: max-tries flag alone, attempt count, DefaultMaxTries
  under: Task execution

~ Task attempt
  One agent invocation for a task. The task executor retries an unsuccessful task up to the implement **Task retry cap**, waiting a **Task attempt retry delay** between consecutive failures. Exhaustion marks the task Failed, records the attempt count and reason locally, and stops draining.
  was: One agent invocation for a task. The task executor retries an unsuccessful task up to the configured maximum, defaulting to three attempts, waiting a **Task attempt retry delay** between consecutive failures. Exhaustion marks the task Failed, records the attempt count and reason locally, and stops draining.
