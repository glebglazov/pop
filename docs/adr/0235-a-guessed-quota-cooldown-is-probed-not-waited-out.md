# A guessed quota cooldown is probed, not waited out

[ADR-0233](0233-claude-dates-a-quota-pause-from-the-epoch-its-stream-states.md)
closed by naming what it had not fixed: *"A cooldown can still outlive the limit
it records when pop had to guess, and clearing one still means editing `pop.db`
by hand."* This is that residual. A cooldown pop invented now says so, expires
against a ceiling instead of a fresh guess, and ends when the agent says it may
rather than when a clock pop set for itself runs out.

## Context

`agentQuotaCooldownUntil` answers an undated refusal with `now.Add(fallback)` —
one hour measured from the moment of the refusal. That origin is the defect, not
the hour. A provider's reset is an instant on the provider's window; the fallback
is a duration from pop's disappointment, and the two coincide only by luck.

Worse, the error only ever grows. An early retry earns a refusal, and that
refusal writes another full hour *from the later moment*. The incident behind
ADR-0233 ran 19:58 → 20:58 → 21:58 against a window that reopened at 21:00: an
undershoot of two minutes bought an overshoot of fifty-eight. Nothing ratchets
the deadline back, and once the row exists nothing asks the agent again —
`tasks/attempts.go` reads the store before invoking and synthesises a quota-pause
verdict straight from the recorded instant, so three restarts parked without
sending a single request.

The row cannot express any of this. `agent_cooldowns` is `preset` and
`exhausted_until`, and four very different things write that column: a provider's
epoch, a provider's countdown, a per-signal backoff pop invented, and the blind
hour. The sibling `agent_model_cooldowns` already keeps `stated_until` beside the
expiry it enforces, and its migration comment already reasons the case: *"a
spent-allowance probe costs seconds and a month-long park would sit blind through
a top-up or a plan change."* The preset row never got the same treatment, because
until ADR-0231 a preset park was assumed too expensive to re-test.

## Decision

**A cooldown records whether pop read it or guessed it, and a guess is resolved
by asking rather than by waiting.**

1. **The row says which it is.** `stated_until` NULL means pop guessed. A guess
   never overwrites a live stated instant, which alone would have prevented the
   21:58 write.
2. **A guess expires against a ceiling, computed once.** `exhausted_until`
   becomes the latest the refusal's **Quota window class** can still run —
   five hours from a session limit, a week from a weekly one — derived at the
   *first* refusal and never re-derived from a later one. The ratchet is not
   forbidden by a rule that can be forgotten; after this there is no expression
   anywhere that dates a deadline from a subsequent refusal.
3. **The class sets the probe interval**: `clamp(span / 30, 10m, 2h)` — ten
   minutes for a five-hour window, two hours for a weekly or monthly one. Around
   thirty probes bound the overshoot to roughly three percent of the window
   either way. Escalation survives only for a refusal naming no class: assume the
   shortest, and walk toward the ceiling once that assumption is disproved.
4. **A probe is cheap or it is not worth having.** It runs on the store-pure
   attempt path `RunRoutineAgentInvocation` already uses — no `BeginDrain` and so
   no `drains` row (which is also the Queue journal entry), no **Captured run**,
   no attempt against the **Task retry cap**. Refused, it advances the next
   probe; allowed, it deletes the row and every parked waiter resumes through its
   ordinary **Recovery turn**.
5. **One probe per preset per interval, claimed by a lease.** The cooldown row is
   machine-global while parks are per-checkout, and **Recovery turn** only
   serialises waiters within one checkout — so parallel worktrees would each
   probe. A ~60s `probe_lease_until`, won by compare-and-swap, settles it. The
   lease follows `spawnIntentTTL` rather than the `recovery_waiters` pid pattern:
   a probe claim blocks nothing and lasts seconds, so a killed prober costs one
   lease and needs no sweep to detect.
6. **A statement that proves wrong becomes a guess.** Refused at the instant a
   provider named, pop clears `stated_until` and falls into the schedule above
   rather than inventing a fresh interval — so the two policies compose instead
   of needing a third.
7. **The intervals are constants, not config.** The house split puts
   operator-facing policy in `[work.*]` and mechanism cadence in a constant with
   a comment justifying its number; the recovery poll cadence, the closest
   analogue, is firmly the latter. `[work.daemon].agent_quota_retry_after`
   survives as the *unclassed* ceiling, defaulting to the shortest class span
   rather than an hour — as a one-hour blind wait it is a live path back to the
   compounding.

A cooldown is also clearable without `sqlite3`: listing them shows which are
guesses, and dropping one is a command.

## Considered and rejected

**Leaving the blind hour alone, as ADR-0233 chose.** That ADR rejected probing
because it "buys a correct answer with an agent invocation per resume, on every
preset, forever" — a fair objection to probing as a *replacement for reading the
wire*. This probes only rows pop has already admitted are guesses, through a path
that writes nothing, on a cadence the window class sets. ADR-0233's decision is
untouched and load-bearing here; only the scope of its rejection narrows.

**Generic capped backoff — 5m, 10m, 20m, to a ceiling.** It assumes we do not
know the window's span. We usually do: the class is on the wire and in the prose
both. A class-blind ceiling also keeps the failure it was meant to fix — parked
at an hour, a window opening a minute after a probe still costs fifty-nine.

**A flat short interval for everything.** Ten minutes across kimi's monthly
signal is roughly a thousand refused invocations. The class is exactly what
makes a short interval affordable where it helps and unnecessary where it does
not.

## Consequences

`exhausted_until` changes meaning: for a guessed row it is a backstop rather than
a retry time, and no longer readable at face value by anything that does not also
read `next_probe_at`. The surfaces that print it today — the work dashboard
snapshot and the attended launch-time skip — must say *guessed* rather than state
a confident instant, which is the reporting failure that sent the incident's
human to a raw SQL delete in the first place.

This is not the deferred **Agent quota reporting**. A probe asks "will you run?"
about a preset already known to be exhausted, after a refusal, and displays
nothing; reporting is proactive and quantitative. The `utilization` field on the
same events stays deliberately unread.
