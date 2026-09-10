# The worktree picker animates a removal in flight and polls while one is

**Checkout removal** became an **Errand** so the **Worktree picker** would stop
blocking on it (ADR-0272), and the picker went quiet in the same move: the human
presses delete, the row is still listed by `git worktree list`, and nothing on it
says pop is working. That is the failure mode ADR-0271 was written about, one
level up — an act that looks like it did not happen.

So the picker marks it. An **Errand** whose subject is a row of the picker
renders in the marker column: the shared working spinner (`ui.SpinnerFrames`,
sized on the fixed-width stand-in of ADR-0111) while it is queued or running, and
a static `✗` once it has failed. Queued and running are one marker, not two —
`deleteWorktree` wakes the errand half at queue time, so queued is a state the
human has no chance to observe as distinct. The in-flight marker is checked
before `Prunable`, so it beats the **Half-removed checkout** `!`: a removal in
flight is what repairs that state, and naming the damage while pop is fixing it
is the wrong headline.

This does not contradict ADR-0273. Queued and running stay off the **Work
seam** — they get no container, no `Columns`, no `StatusCell`, no verbs. The
picker is the surface where the human asked for this act thirty seconds ago, and
answering "did my delete happen" there costs one `ListErrands()` per build, not a
`work.Kind`. Only the **Errand failure** is still Work, and the picker's `✗`
carries no retry: pressing delete again on a failed row re-queues it through the
store's existing conflict rule, so the marker needs no verb of its own.

The cost is a timer in `ui.Picker`, which has never had one — it built its items
once and held nothing live. A static marker would have been free and would have
lied: it is painted for a state that ends in seconds, on a row for a directory
that is by then gone. So the picker takes an items provider and rebuilds at 1s,
with the spinner advancing at the shared 100ms cadence, and **starts neither tick
unless an item is animated**. A picker with no removal in flight is exactly as
idle as before.

## Consequences

`ui.Picker` is now a surface that can re-read the world, which the project picker
may want next. What it re-reads is still scoped: it holds no live config, so the
**Config dashboard host** rule that only the Work dashboard hot-reloads config is
untouched.
