# A flash message takes the hint line for three seconds

Pop's TUIs had two ways to say "that worked": `Frame.Status`, a region of its
own sitting above `Footnote`/`Block`, and nothing at all. `Status` was
documented as "transient action feedback" and the Work dashboard's `statusMsg`
repeated the claim ("a transient one-line message shown above the hint bar"),
but neither had a lifetime — the Work dashboard cleared it by hand in some fifty
separate `m.statusMsg = ""` assignments, and any path that forgot left a stale
message on screen until the next keypress that happened to remember.

We replaced transient use of `Status` with a **Flash message**: a one-line
message that takes over the `Hints` region for three seconds and then yields it
back. One shared `ui.Flash` value type owns the expiry and the `tea.Cmd` that
fires it, and every `Frame`-based view embeds it — so "three seconds" is written
once, and a message's lifetime does not depend on whether the view happens to
have a poll tick.

## Considered options

**A reserved line of its own** — hold a line permanently so a message never
shifts the layout. Rejected: it costs a line of body height in every view,
forever, to display something that is absent almost all the time. Taking the
hint line is free, and the hints are the one region a user does not need while
reading a message about what just happened.

**Keep `Status` and add flash alongside it** — rejected outright. It would leave
pop with two mechanisms both called "transient one-line feedback", differing
only in lifetime and position, and the largest consumer of the semantics we
actually wanted was already using the wrong one.

## Consequences

Messages in the Work dashboard **move**: `Status` rendered above `Footnote` and
`Block`, and the flash renders below them, on the bottom line. That is a visible
relocation for the surface with the most messages, accepted so that every
dashboard says things in the same place.

Hints are hidden for three seconds after any action. On a surface whose hints
are the only advertisement of a binding, a user acting quickly will not see them
— the **Help overlay** (`C-h`) remains the complete listing.
