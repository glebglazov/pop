---
status: accepted
---

# Mute is a timed, human-set "not now" on a Work container

A human can **Mute** one Work container for a while: it leaves the default Work view and
comes back on its own when its instant passes. Mute is the first suppression in pop that
is both authored by a human and expiring, it reaches supervision by destroying the
container's Auto-drain bit rather than by gating the daemon, its default duration
resurfaces at a random instant three to seven days out that no read surface ever
discloses, and only Task sets and Maps are mutable.

## Context

The Work dashboard shows every container that matters, which is the point — and the
reason the list goes unusable during a busy week. A human sitting in front of six Ready
Task sets they have no time for this sprint has three existing gestures, none of which
say what they mean:

- **Archive** says the work is over. It is indefinite and it is a filing decision, so
  using it for "not this week" corrupts the one bit that answers "is this set still a
  thing".
- **Park** is human-set and indefinite, but it belongs to the Drain story — it is what a
  crashed drain becomes until a human clears it. It is not a triage gesture and it has
  no expiry.
- **Drain backoff** expires, but it is machine-derived from crash history. Nothing human
  authors it.

So the gap is precise: pop has an indefinite human suppression (Park, Archive) and an
expiring machine suppression (backoff), and nothing that is both human and expiring. The
glossary already shows the vocabulary crowding — **Spawn deferral** carries
`_Avoid_: spawn hold, pause, suppression, block`, four words spent on the read-side
"why isn't this spawning" answer. A fifth word for a human's "not now" would be one
more overloaded term unless it is unmistakably its own concept.

Two facts made the mechanism cheap enough to be worth doing at all:

- `work_containers.archived` already lives on the Work container registration precisely
  because it is "the only genuinely cross-kind registration bit". A `muted_until` beside
  it is the same shape, one appended migration, and one accessor file.
- `matchesViewFilter` already receives `now`, so a preset input whose truth changes with
  the clock needs no new seam.

Against that, the daemon interaction had no cheap answer. A muted Auto-drain set with
`pop work daemon` running keeps draining: it spends tokens, produces terminals, and
generates exactly the noise the human muted it to stop. A display-only mute would be a
lie the moment the daemon is up.

## Decision

**1. Mute is a durable, expiring, per-machine bit on the Work container registration.**
`muted_until` joins `archived` on `work_containers`, as one appended forward-only
migration. Expiry is a read-time comparison against `now` — no sweeper, no cleanup job,
no state to reconcile. A mute therefore does not travel with the repository, the same way
an archive does not.

**2. Muting a Task set clears its Auto-drain bit, and that is the whole of mute's reach
into supervision.** Not a gate the daemon consults — a write, once, at mute time.
`selectReadySets` already admits only `Ready && AutoDrain`, so the daemon needs no
change at all. Consequences accepted deliberately: unmuting does **not** restore the bit
and does not report that it was cleared; the human may turn Auto-drain back on while the
container is still muted, and that combination is honoured, because Auto-drain is
standing consent to act and a view gesture has no business vetoing an explicit
instruction.

**3. Mute never reaches a running process, and muting a busy container succeeds.** Like
Archive, it can neither cancel a Drain, retire a Checkout gate hold, nor answer an open
gate prompt. Unlike Archive it does **not** refuse on live occupancy — it reports what is
still live ("muted — drain still running, pid 4123") and proceeds. Archive refuses
because filing implies the work is finished; "no time for this right now" is a coherent
thing to say about a set that is mid-drain, and refusing would disable mute in the exact
moment a noisy set is what the human wants out of their view.

**4. A Mute window is a date the human picks, not a duration.** The menu offers **six
entries, always**: the random default at digit 1, then five dates at digits 2–6. Every dated
entry lands at **09:00 UTC** on its day, so no arithmetic and no normalization rule is
needed — the list enumerates only instants that are already mornings. It is
surface-owned: a date is not kind knowledge, so three kinds returning the identical six
actions would be the copy-paste the Work seam exists to prevent, and a config roster would
buy tunability for a list that is already specified, at the cost of a validation surface and
ADR-0198's reach question.

Which five days appear is derived from today. **The first is always tomorrow, whatever day
tomorrow is** — including a weekend. Every other entry is a weekday morning: this week's
remaining weekdays take absolute precedence over next week's, and within each week a
preference ladder decides who makes the cut — **this week `Fri > Wed > Mon > Tue > Thu`,
next week `Mon > Wed > Fri > Tue > Thu`**. At least one entry always comes from next week, so
"not this week at all" is available every day; since this week can contribute at most four
days, that guarantee never displaces a this-week entry. The chosen five are then displayed
**chronologically**, which keeps the digits monotonic in time — `3` is always sooner than
`5`. The random default is the one exception to chronological order: it is pinned to digit 1.

Tomorrow is the exception to weekday-only because a one-day postponement is the most common
thing a human wants and it must not vanish two days a week — from Friday you can say
Saturday, and from Saturday you can say Sunday. Since tomorrow is by definition the earliest
date, this also fixes it at **digit 2 every day of the week**, so the most-used entry is the
one entry with a stable key. It is deliberately the *only* weekend-capable entry: making
Saturday and Sunday ordinary offerable days would have them compete for slots with next
week's weekdays, and then nothing about the list is a working morning any more.

Note that the this-week ladder does **not** bind at six entries: this week never offers more
future weekdays than the slots available, so every remaining weekday of this week is always
taken and the ordering among them is inert. It is written down because it becomes
load-bearing the moment the cap changes — at five entries, Monday would drop Tuesday. Only
the next-week ladder actually selects today.

The ladders are not arbitrary. Friday leads this week because "deal with it before the week
ends" is the common intent, and Thursday trails because it is nearly the same answer as
Friday; next week inverts to Monday-first because inside a week you are not pushing past
anything, so the near days are the useful ones. Worked out, every day of the week:

| Today | Dated entries, digits 2–6 |
| --- | --- |
| Mon | Tomorrow (Tue), Wed, Thu, Fri, Mon next |
| Tue | Tomorrow (Wed), Thu, Fri, Mon, Wed next |
| Wed | Tomorrow (Thu), Fri, Mon, Wed, Fri next |
| Thu | Tomorrow (Fri), Mon, Tue, Wed, Fri next |
| Fri | Tomorrow (Sat), Mon, Tue, Wed, Fri next |
| Sat | Tomorrow (Sun), Mon, Tue, Wed, Fri |
| Sun | Tomorrow (Mon), Tue, Wed, Thu, Fri |

Opening the menu on a weekend needs no special case: Saturday and Sunday fall out of the same
rules, with the upcoming Monday-to-Friday as the next-week pool.

Entries are labelled by day and date — `Fri 14 Aug`, `Fri 21 Aug` — except the first, which
reads `Tomorrow`. The invariant hour is stated once in the submenu's footer rather than
repeated on five entries, and in full on the row afterwards.

**Unmute** sits outside all of this: it is offered only on an already-muted row, keyed `u`
rather than a digit, and does not count against the six. It is not a window, so it should
neither compete for a window's digit nor make the date list change length with state.

**5. The submenu is the `VerbStatus` pattern, reused.** A kind cannot ask the surface to
open a modal — `work.Outcome` has no modal-capable kind, deliberately — so nesting
happens only when the surface recognises one shared verb id. `work.VerbMute` joins
`VerbStatus` as a shared opener; every mutable kind offers `{VerbMute, "m", "mute ▸"}` in
its `Actions`; the surface owns the numbered submenu. `a` is already the action menu and
`m` is unused as a menu-item key on every kind, so the gesture is `a` then `m` with no new
prefix machinery — the single `pendingG` bool stays the only prefix in the dashboard.

**6. The random window's resurfacing instant is secret.** The default rolls a uniform
instant in `[3d, 7d)` at full precision, injected through the deps bag so tests fix a
seed rather than tolerate a range. It takes **no** working-instant normalization — landing
every secret mute at 09:00 UTC would hand back most of what the secret hides, and the
guarantee that matters is the floor (never sooner than three days), not the hour.

A random mute renders `unmuted on [?]`, which discloses that a secret exists rather than
concealing that there is one; a dated mute renders `unmuted on Fri 14 Aug, 09:00 UTC`,
because a day the human picked off a list was never secret. The secret binds every read
surface equally — the status cell, the detail view one `l` away, and `pop work status` —
and it binds **sort order** too: no preset may order muted rows by resurfacing instant,
because position in a list of six muted rows discloses the roll as surely as a date. The
shipped `muted` preset therefore sorts by creation date from the id. The instant stays
legible in `pop.db`; the secret protects against the human's own glance, not their
database.

**7. Only Task sets and Maps are mutable, enforced at the SQL boundary.** Routines are
not: a Routine already carries an indefinite pause bit, human-authored and read by its
advancer, so a second human-set suppression beside it would be two vocabularies for one
intent. Declaring this by omission — the Routine kind simply never offering `VerbMute` —
is how eligibility works everywhere else in the seam, but omission alone leaves the
durable invariant unenforced: a future CLI or test could write `muted_until` onto a
Routine ref, producing a row that is muted with no verb able to clear it. So the mutable
kinds are expressed on `ref.Kind` — the leaf package `store` may import — and `store`
refuses the write, the way `validContainerRef` already refuses an invalid kind. No
`Kind.Mutable() bool` member is added: it would make every kind answer a question
two-thirds of them answer by silence.

**8. `active` hides muted rows and a shipped `muted` preset shows only them.** Without the
first, mute changes nothing in the view the human actually sits in. The second is the only
route to unmute, since a muted row is by construction invisible elsewhere.

## Considered Options

**Gate the daemon on `muted_until` instead of clearing Auto-drain.** More reversible — the
human's Auto-drain preference survives the mute. Rejected as a bigger mechanism for a
worse boundary: it puts a display fact into the supervision path, so every future reader
of `selectReadySets` has to know that a view concept can veto a drain. Clearing the bit
keeps mute's reach visible at the point the human performs it, at the accepted cost of a
destroyed preference (decision 2).

**Extend Park with an expiry.** Rejected: Park is the Drain story's vocabulary, produced
by crashes and consumed by `SpawnDeferral`. Overloading it would mean a human's triage
gesture and a crash aftermath render as the same word.

**Mute Routines too, wiring preset evaluation into page B.** This was the largest single
piece of plumbing in the feature; decision 7 deletes rather than defers it. The pause bit
already covers the intent for Routines.

**Show the random mute's instant.** Simplest, and it makes the row self-explanatory.
Rejected because it defeats the only reason the default is random (decision 6): a batch
muted together must not all come back on the same morning, and that only holds while the
human cannot read the roll and pre-empt it.

**Normalizing the random window to a working instant as well.** Consistent, and it would
make every mute end at a predictable hour. Rejected in decision 6: it collapses the search
space from "some instant in a four-day span" to one of four or five mornings, which is
close enough to disclosure to defeat the point. The asymmetry is the decision, not an
oversight — named windows are normalized precisely *because* they are not secret.

**Duration-labelled windows — three days, one week, two weeks, one month.** The first
design, and the one this ADR moved away from. Two problems. It makes the human do the
arithmetic pop can do for them ("if I mute for three days on Wednesday evening, when does
it come back?"), and a duration lands wherever it lands — including Saturday night, which is
no use to anyone. Fixing the second needed a normalize-forward-to-a-weekday-morning rule
that the dated menu makes unnecessary, since it can only offer working mornings in the first
place. What was lost is reach: durations went out a month, the dated list stops at next
week (see Consequences).

**Rendering a dated mute as a relative countdown (`muted 4d`).** Considered while windows
were durations and an absolute date was ambiguous at the edges. Rejected once entries became
dates: `unmuted on Fri 14 Aug` answers "will I be at my desk when this returns", which a
countdown does not, and the row should read back the same words the menu offered.

**Fixed digit slots so a day always keeps its number.** Rejected because the six-entry cap
already stabilises the menu's *shape*, and gaps would make the list look broken. The list is
one you read rather than one you fire from memory, since the dates change daily.

## Consequences

- A muted row still holds its checkout, its binding and its panes. Mute is scheduling and
  display only, so occupancy is never filtered by it — the same rule Archive states.
- **The furthest a mute now reaches is next Friday.** The dated menu tops out inside next
  week, so "not this month" is no longer expressible — Archive is the only thing beyond it,
  and Archive says the work is over. This is the price of decision 4 and the most likely
  thing to want changing later; the fix is a seventh entry (a dated Monday two or four weeks
  out), not a wider random default, which must stay the anti-clustering option rather than
  become the long one.
- Unmute is reachable only through the `muted` preset, on `u`. That is acceptable because the
  preset ships numbered, but it does mean "what did I mute?" is a preset switch, not a
  glance.
- The menu's content depends on the day it is opened, so any test of it must fix the clock —
  the same injected clock the random roll already needs, and it wants a case per weekday
  including Saturday and Sunday.
- **A mute can end on a weekend.** "Muted work resurfaces on a working morning" is therefore
  true of every entry except the one the human explicitly picked as tomorrow. That is the
  accepted price of a one-day postponement that works on a Friday and a Saturday.
- A mute expiring while the dashboard is open resurfaces the row on the next rebuild, with
  no event and no notification. The human is not told a mute ended; the row simply comes
  back.
- `pop work mute` does not exist. Mute is a triage gesture made while looking at the list,
  and the unattended half — a daemon that mutes things — is not wanted. The store accessor
  is the same either way, so adding the CLI later costs nothing.
- The secrecy rule in decision 6 is the fragile part of this ADR. It looks like an
  omission, so a future `Sort: muted_soonest` reads as an obvious usability win. The
  shipped preset carries a comment saying why it must not exist.
