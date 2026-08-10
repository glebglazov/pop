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

**4. The Mute window is a fixed, surface-owned, numbered list, and a named window lands on
a working instant.** Six entries: the default random window, three days, one week, two
weeks, one month, unmute. A duration is not kind knowledge, so three kinds returning the
identical six actions would be the copy-paste the Work seam exists to prevent; and a config
roster would buy tunability for a list that is already specified, at the cost of a
validation surface and ADR-0198's reach question.

A named window does not resurface at "now plus the duration". It normalizes to **09:00 UTC
of the first weekday at or after the raw arithmetic** — a set muted at 23:40 on a Wednesday
for three days comes back at the start of a working day, not late on a Saturday night.
Normalization only ever moves **forward**: a Saturday or Sunday landing goes to Monday, not
back to Friday. Every window is therefore a floor, never returning work sooner than the
human asked, which is the property the random default already has explicitly. The cost is
that a Friday mute and a Saturday mute of the same length resurface together on Monday;
that is preferable to the alternative, where the shortest window in the list is also the one
most likely to end early. UTC rather than local time is deliberate: the instant is stored
and compared machine-side, and a fixed meridian keeps a mute meaning the same thing on two
machines in two timezones.

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
concealing that there is one; a named mute renders its working instant plainly, because a
duration the human chose from a list was never secret. The secret binds every read
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

**Closest weekday in either direction.** The literal reading of "closest": Saturday back to
Friday, Sunday forward to Monday. Rejected — it makes a three-day mute the window most
likely to end early, and a mute that returns work sooner than asked is the surprise that
stops the human trusting the feature.

**Rendering a named mute as a relative countdown (`muted 4d`) rather than a date.** The
original choice, made when a named window could land at any second and an absolute date was
therefore ambiguous at the edges. Superseded by the working-instant rule: once a named
window lands at 09:00 UTC on a named weekday, the date is unambiguous and answers "will I
be at my desk when this returns", which a countdown does not.

## Consequences

- A muted row still holds its checkout, its binding and its panes. Mute is scheduling and
  display only, so occupancy is never filtered by it — the same rule Archive states.
- Unmute is reachable only through the `muted` preset. That is acceptable because the
  preset ships numbered, but it does mean "what did I mute?" is a preset switch, not a
  glance.
- A mute expiring while the dashboard is open resurfaces the row on the next rebuild, with
  no event and no notification. The human is not told a mute ended; the row simply comes
  back.
- `pop work mute` does not exist. Mute is a triage gesture made while looking at the list,
  and the unattended half — a daemon that mutes things — is not wanted. The store accessor
  is the same either way, so adding the CLI later costs nothing.
- The secrecy rule in decision 6 is the fragile part of this ADR. It looks like an
  omission, so a future `Sort: muted_soonest` reads as an obvious usability win. The
  shipped preset carries a comment saying why it must not exist.
