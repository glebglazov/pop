# Task attempt retry backoff is blocking in-process

AFK implement and verify retries no longer fire back-to-back. After a retry-eligible failure, pop sleeps a configured **Task attempt retry delay** before the next invocation — default `1m → 5m → 15m`, with the last entry repeating when the attempt cap exceeds the list length. The wait is a blocking in-process sleep inside the running drain (countdown on stdout; Ctrl-C during the wait exits as **Interrupted**), not a persisted schedule the queue picks up later. That keeps retry state out of the daemon and matches today's immediate-retry loop shape, at the cost of holding the runtime lock for up to fifteen minutes between tries.

Schedule and caps live at `[tasks]` root (`attempt_retry_delays`, `max_tries`); `[tasks.implement]` and `[tasks.verify]` may override their side's cap. An empty delay list restores instant retries for local dev and tests. Verify retries only on invocation failures (timeout, agent error, unparseable output) — a cleanly parsed NEEDS-HUMAN or FIXABLE verdict is not retried. Both paths retry per agent preset up to the cap, then **Agent fallback** to the next configured preset. **Agent quota pause**, attempt timeout, and **Queue backoff** (abnormal drain exits) are unchanged.

**Considered Options**

- **Persist retry-at timestamps and release the drain** — frees the runtime lock during the wait but needs durable state, reconciliation, and queue integration; rejected for a first slice.
- **API/transient-only classifier** — retry delays only when output matches rate-limit signals; rejected as fragile across agent adapters when quota detection already handles hard stops.
- **Separate verify delay schedule** — rejected; one `[tasks] attempt_retry_delays` shared by implement and verify.
