# Runtime execution lock is held only during active execution

The **Runtime execution lock** (the running Drain row, ADR-0055) is now held only while a drain is *actively executing* in its checkout. Every wait for human input releases it: the pre-run confirmation, the [HITL gate prompt](0012-hitl-gates-offer-attended-agent-assistance.md) menu, and the Failed gate prompt menu. Concretely, `BeginDrain` wraps only the contiguous run of AFK attempts; reaching any gate `Finish`es the drain with its park outcome (`unverified`/`blocked`/`failed` — the same disposition the `--yes`/daemon path already records), the menu runs lock-free, and resuming after the human clears the gate `BeginDrain`s afresh. Assist sessions and the runtime shell launched from a gate also run lock-free. This applies uniformly to whole-set drains and single-task `implement`.

## Why

A drain parked at a gate is doing nothing — it is waiting on a human — yet it held the checkout exclusively for the entire wait. A human who hit a HITL prompt in one set could not `implement` another task in the same checkout until they exited the prompt (`runtime execution already in progress (PID …)`). The lock should track *active execution*, not *process liveness*. Modelling it as "running Drain row = live lock, terminal row = outcome history" makes the release free: finishing the drain at the gate both records the park outcome the daemon needs and drops the live lock.

## Considered Options

- **Keep the lock through the menu (status quo).** Rejected: it conflates "a human is reading a prompt" with "an agent is mutating the checkout," blocking unrelated manual work for no execution-safety reason.
- **Release only at terminal (UNVERIFIED) gates.** Rejected in favour of one rule for every human-wait — two code paths, and the BLOCKED setup gate camps the lock just as needlessly.
- **Re-acquire real mutual exclusion around assist/shell.** Rejected: assist and shell never took the lock themselves; they were only ever shielded incidentally by the parent drain. The human launching them owns the checkout, so they run lock-free.
- **Preserve the daemon's "checkout busy" signal** (keep the lock as the marker, or derive occupancy from the parked outcome). Rejected: see Consequences — we accept the daemon footgun rather than carry a second occupancy mechanism.

## Consequences

- **The daemon's anti-double-spawn signal is dropped.** Holding the lock through the gate was deliberate — it made the [Queue daemon](0027-queue-is-a-parallel-per-project-supervisor.md) treat a parked pane as busy and never spawn a second drain into that checkout. With the lock released, an inline-trunk repo running multiple auto-drain sets can have the daemon spawn a *second* set into the same trunk while the first is parked at a gate — concurrent mutation of one working tree. The mitigation is **worktree isolation** ([ADR-0046](0046-implement-defaults-to-worktree-execution.md)): each set owns its checkout, so interleaving is harmless. Inline-trunk + multiple auto-drain sets is an accepted footgun, not a supported configuration.
- **Resume can be refused.** When the human clears a gate and AFK work becomes eligible, the re-`BeginDrain` may collide with a drain that grabbed the checkout meanwhile; it refuses cleanly. No work is lost — the gate decision was already persisted to the manifest.
- **One attended session may emit several Drain rows** (e.g. an `unverified` park, then a `done` resume). This is already the daemon's normal pattern across spawns, so outcome readers and the supervisor are unaffected; a parked set simply reads as blocked/unverified rather than running, and is no longer a [Picked-up Task set](0055-drain-execution-lifecycle-is-a-durable-store.md).
- Refines ADR-0055: a Drain row no longer spans one `implement` invocation; it spans one contiguous active-execution segment.
