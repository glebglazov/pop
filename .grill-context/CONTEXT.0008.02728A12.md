---
fragment: 02728A12
generation: 0008
branch: master
---

+ Pane work attribution
  The answer to "which Work container does this tmux pane belong to?", derived from
  what the pane itself can show. A first-hit ladder: the pane's own tag naming a Task
  set or a Decision ticket, then the Work session stamp naming a Map, then the live
  Drain running at the pane's directory, then the checkout the pane sits in and the
  Task sets bound to it. Each rung is kind knowledge, so the ladder is answered
  kind-side behind the Work seam rather than by a switch in the surface, and it is
  re-derived on every read like every other Work fact.
  avoid: pane ownership, pane binding, reverse lookup
  under: Language

+ Bound-checkout attribution tiebreak
  Which Task set a bare shell in a checkout is attributed to when several sets are
  bound to that checkout. The Checkout claim answers it outright when something is
  live there; otherwise the set drained most recently; otherwise the topmost row bound
  to that checkout under the current sort. The chosen set is always named where the
  attribution is used, because the weakest rung of Pane work attribution is also the
  one that fires most often — for the ordinary editor shell the human opened
  themselves.
  under: Language

+ Pane-seeded cursor
  The Work dashboard opening with its cursor already on the row the launching pane is
  attributed to. One shot, at first render: it never chases the human's own
  navigation, and it never revisits its choice later in the session. Silent when the
  pane is attributed to nothing, which is the ordinary case for an unrelated shell;
  loud in the status line when a container was attributed but its row is not
  renderable, because a cursor sitting at row one with no explanation is
  indistinguishable from a broken feature. It moves a cursor and nothing else, so it
  carries no opt-out — the first keypress is the opt-out.
  avoid: follow mode, cursor sync, reveal, jump-to-row
  under: Language

+ Mute
  A human-set, expiring "not now" on one Work container: hidden from the default Work
  view until its instant passes, at which point it resurfaces on its own with nothing
  to clean up. Muting a Task set also clears its Auto-drain bit, and that is the whole
  of mute's reach into supervision — a destroyed bit, not a gate, so unmuting never
  gives it back and the human may turn it on again while still muted. It never touches
  a running process, so like Archive it can neither cancel a Drain, retire a Checkout
  gate hold, nor answer an open gate prompt; muting a busy container succeeds and
  reports what is still live there, because the hazard is hiding the row that would
  have reminded the human to answer a parked prompt. It is the first suppression in pop
  that is both authored by a human and timed: Park is human-cleared but indefinite,
  Drain backoff is timed but machine-derived, Archive is indefinite. Lives beside
  Archive on the Work container registration, so a mute is per-machine and does not
  travel with the repository.
  avoid: snooze, pause, suppress, hide, defer
  under: Language

+ Mute window
  When a Mute ends, picked as a date rather than a duration: the human chooses a morning off
  a list, so pop does the arithmetic instead of them. Always six entries — the random default
  first, then tomorrow, then four weekday mornings, every one at 09:00 UTC and labelled by
  day and date except tomorrow. Tomorrow is offered whatever day it falls on, weekend
  included, because a one-day postponement must not vanish two days a week; it is the only
  entry that may land on a weekend, and being the earliest date it always holds the same
  position. The other four come from today by the Weekday preference ladder. Unmute stands outside the six, offered only on a row that is already
  muted, since it is not a window. The list is the same for every mutable kind — a date is
  not kind knowledge — so the surface owns it rather than the kinds, and it never reaches
  past next week: anything further away is what Archive is for.
  avoid: mute duration, snooze period
  under: Language

+ Weekday preference ladder
  How the dated Mute windows after tomorrow are chosen. This week's remaining
  weekdays take absolute precedence over next week's; within this week the order is
  Fri, Wed, Mon, Tue, Thu, and within next week it inverts to Mon, Wed, Fri, Tue, Thu. At
  least one entry always comes from next week, so "not this week at all" can be said on any
  day — a guarantee that never costs a this-week entry, because this week can offer four
  days at most. Friday leads this week because finishing before the week ends is the common
  intent and Thursday is nearly the same answer; next week leads with Monday because inside
  a week nothing is being pushed past, so the near days are the useful ones. The chosen dates
  are displayed chronologically, which keeps the numbering monotonic in time; the random
  default is the sole exception, pinned first. The this-week half of the ladder does not bind
  while the list holds six entries — this week never offers more weekdays than there are
  slots, so their order is inert — and it is stated because it starts selecting the moment
  the list gets shorter.
  under: Language

+ Secret resurfacing
  The rule that a Mute taken with the random Mute window never displays when it ends.
  Such a row reads `unmuted on [?]` — it discloses that a secret exists, which is the
  honest form, rather than hiding that there is one. A dated window shows its instant
  plainly, since a day picked off a list was never a secret. The random default is also the
  one window that lands at an unrounded instant, because rounding every secret mute to the
  same morning would hand back most of what the secret hides. It exists so a batch muted in
  one triage pass does not all return together, which only works while the human cannot read
  the roll — so no read surface prints it and no Work view preset may order rows by it, since
  position in a list of muted rows would disclose it as surely as a date. The instant is
  legible in pop.db, which is not the glance the secret protects against.
  avoid: hidden mute, blind snooze
  under: Language

+ Mutable Work kind
  A Work kind whose containers can be Muted: Task sets and Maps. Routines are not
  mutable — a Routine already carries its own indefinite pause bit, authored the same
  way and read by the daemon, so a second human-set suppression beside it would be two
  vocabularies for one intent.
  under: Language
