# claude dates a quota pause from the epoch its stream states

A drain lost an hour and eleven minutes to a **Agent quota pause** that had
already lifted. claude refused a task attempt at 19:58 local with

    You've hit your session limit · resets 9pm (Europe/Madrid)

and pop cooled the preset until 20:58 — a blind hour from the moment of the
refusal, because none of its patterns can read `resets 9pm`. The window actually
reopened at 21:00. The **Agent quota recovery wait** therefore woke two minutes
early, was refused on the edge, and wrote a *second* blind hour from that
moment: 21:58, now fifty-eight minutes past a window that was open. Three
restarts of `pop tasks implement` read that row and parked without asking
claude anything. The human cleared it with `sqlite3 … "delete from
agent_cooldowns"`, restarted, and the task ran first try.

## Context

[ADR-0034](0034-reset-aware-agent-cooldown.md) made the cooldown reset-aware:
each adapter reads the reset out of its own refusal, and a missing or
unparseable one falls back to a fixed interval. That contract holds, and its
fallback did what it promises. What it did not anticipate is that the fallback
is not neutral — it is an hour drawn from the wrong origin. A stated reset is
measured from the provider's window; the fallback hour is measured from the
refusal. When the two disagree by less than an hour the fallback expires *early*,
and an early retry does not merely waste an invocation: it is refused, and the
refusal writes another hour from a later origin. Each miss pushes the recorded
cooldown further past the truth. One unreadable sentence is enough to strand a
drain indefinitely, which is the shape the incident took.

The sentence was unreadable for two reasons at once, both visible in that one
line. `claudeBareResetAtPattern` requires minutes (`resets 9:00pm`), and the
message states the hour alone. And the message names the zone the hour is in —
`(Europe/Madrid)` — which pop's derivation has never read, so on a machine set
to any other zone a *readable* sentence would have produced a confidently wrong
instant instead.

## What the evidence said

The captured run carries the answer in a field beside the sentence. Every
`rate_limit_event` in it states the same epoch, twelve times across the run:

    {"type":"rate_limit_event","rate_limit_info":{"status":"allowed_warning",
     "resetsAt":1787598000,"rateLimitType":"five_hour","utilization":0.9,...}}
    …
    {"type":"rate_limit_event","rate_limit_info":{"status":"rejected",
     "resetsAt":1787598000,"rateLimitType":"five_hour",...}}

`1787598000` is 19:00:00Z — 21:00 local, the moment the window reopened and the
moment the prose was trying to say. pop parsed the sentence and ignored the
number. The capture is kept as `tasks/testdata/streams/claude-session-limit.events.jsonl.gz`.

## Decision

**claude dates a quota pause from its own rate-limit event when the capture
carries one, and from the prose clause only when it does not.**

1. **The wire figure wins.** `claudeRateLimitResetAt` reads the last
   `rate_limit_event` whose `status` is `rejected` and takes its `resetsAt`.
   Only a rejection dates a pause: the same event type reports `allowed` and
   `allowed_warning` throughout a healthy run, and those describe a window pop
   is still spending in, not one it is waiting on.
2. **The instant is resolved where the capture is.** The adapter stamps it on
   the verdict at detection, as cursor's spent-allowance refusal already does
   ([ADR-0168](0168-adapters-declare-a-proceed-verdict-and-effort-tiers-skip-to-the-next-model.md)),
   because `AgentQuotaResetCapability` is handed a reason string and cannot see
   the stream. `resolveProceedResetAt` therefore keeps an instant a verdict
   already carries rather than re-deriving one from the sentence — re-deriving
   is what threw this answer away.
3. **The prose clause stays, and reads more.** It is the fallback for a capture
   with no rate-limit event, and it now accepts the hour-only form (`resets
   9pm`) and honours the zone the message names, falling back to local time when
   it names none or names one this machine cannot load.
4. **The stated epoch is padded by the Quota assurance offset.** It is a
   second-granular edge, and the incident is precisely what happens when a retry
   lands on an edge: the refusal it earns is dated from the wrong origin.
5. **An epoch further out than the cooldown store would accept is garbage.**
   The prose clauses can only name an instant inside the week; a number can say
   anything, and the **Agent quota recovery wait** parks on this value directly.
   Beyond `maxAgentQuotaResetHorizon` the sentence answers instead, which is
   ADR-0034's own clamp applied at the new channel.

This is not a reopening of **Agent quota reporting**, on exactly the grounds
[ADR-0034](0034-reset-aware-agent-cooldown.md) already argued: the figure is
read from the capture pop already consumes, only after a refusal has been
detected, and only to date it. The `utilization` readings on the same events —
which would let pop predict exhaustion before it happens — are deliberately not
read.

## Considered options

- **Widen the regex and stop there.** Rejected as the whole fix, kept as part of
  it. It answers this wording and not the next one, and it cannot answer the
  timezone at all — the message states a zone precisely because the hour alone
  is ambiguous. A structured epoch has no wording.
- **Probe the agent before honouring a recorded cooldown.** Rejected. It buys a
  correct answer with an agent invocation per resume, on every preset, forever —
  and it is a workaround for a date pop can simply read correctly. The residual
  case it would cover (a cooldown that goes stale after it is written, e.g. an
  account topped up mid-wait) is real but rare, and is better served by a way to
  clear the row than by a probe on every wait.
- **Never fall back to a blind interval.** Rejected. An undated refusal has to
  cool for *something* or the walk re-invokes a dead preset immediately. The
  hour is a poor guess, not a wrong mechanism; the fix is to need it less often.
- **Read `resetsAt` from any rate-limit event, not just the rejection.** Rejected.
  A healthy run emits eleven of them before the refusal in this very capture. A
  reset instant is only a fact about a window pop is shut out of.

## Consequences

- A claude quota pause is dated by the provider whenever the capture carries a
  rate-limit event, so the recorded cooldown ends when the window does — and a
  premature retry, if one still happens, re-dates from the same truth instead of
  compounding an hour onto itself.
- Preset cooldowns become as accurate as the stream is honest. A stream that
  states nothing degrades to today's behaviour exactly, so the change is a strict
  superset and reversible by ignoring the field.
- The reading is claude-specific and lives in claude's adapter. Nothing here
  claims other CLIs emit the same event; each adapter still owns how its own
  refusal is dated.
- A cooldown can still outlive the limit it records when pop had to guess, and
  clearing one still means editing `pop.db` by hand. That is now the only
  remaining path to the incident's symptom, and it is left open deliberately —
  naming it here so the next report of a stale cooldown starts from a smaller
  question.
