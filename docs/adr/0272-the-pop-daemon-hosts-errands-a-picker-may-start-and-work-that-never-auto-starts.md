# The pop daemon hosts errands a picker may start and work that never auto-starts

Slow acts — **Checkout removal** first among them — blocked the **Worktree
picker** until the human killed it, which is how a checkout became a
**Half-removed checkout** in the first place. The fix is to queue them, and the
host is one process: `pop daemon run` subsumes `pop work daemon`, with one lock,
one journal and one store. A second daemon beside the supervisor would cost two
of each for one machine-global process.

But **Work daemon** was defined as never auto-started from a picker, because it
runs coding agents unattended across projects — and a queue that only drains
when the human has parked a daemon is not a queue. So the process **splits into
two halves behind one command**: **Errand**s, which a picker may start, and the
**Work daemon**, which never auto-starts. The dividing line is consent, not
cost: an errand is an act the human named at the moment they asked for it, while
the work half runs agents whose output the human watches by parking it in a
pane. Auto-starting the work half would defeat the rule even though consent is
already recorded per Task set and per Routine, because parking it *is* the
watching.

## Consequences

`pop work daemon` is kept as a backward-compatible alias for the work half,
deliberately unlike `pop queue`, which was deleted outright — splitting the
process is not a reason to break a command the operator already types.
`[work.daemon]` keeps its name and its three keys, which describe the work half only — the
errand half is woken by a queued row rather than polled, so it needs no timing
configuration of its own. Liveness is now two states, and any surface that says
"the daemon is running" has to say which half.
