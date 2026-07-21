# Routine schedules are one clause production over a step+mask slot

The schedule parser grew two unrelated forms — `every <Go duration>` (rolling elapsed time since the last fire) and `daily at H[:MM][ utc]` (a wall-clock slot) — each with its own regex, its own branch in `NextAfter`, and its own hand-copied grammar blurb in five places. Adding weekday anchoring, day intervals, and bare `at` as three further forms would have meant three more regexes and three more branches, all re-deriving the same zone/DST/slot arithmetic that [ADR-0126](0126-daily-routine-schedules-are-machine-local-with-an-explicit-utc-suffix.md) settled once.

Instead the whole grammar is **one production with fixed clause order**, every clause optional and at least one required:

```
[every <N><unit>] [on <days>] [at H[:MM]] [utc]
```

which parses into exactly two shapes. `every` with an `h`/`m` unit and no other clause stays the **rolling** schedule: fire at `lastFired + interval`, unchanged. Everything else is one **slot** schedule carrying a day step, a 7-bit weekday mask, and a time of day:

| expression | step | mask | time |
|---|---|---|---|
| `every 6h` | — (rolling) | — | — |
| `at 10:00` | 1d | all | 10:00 |
| `on mon` | 1d | mon | 00:00 |
| `on mon-fri at 09:00` | 1d | mon–fri | 09:00 |
| `every 2d at 10:00` | 2d | all | 10:00 |
| `every 2w on mon at 10:00` | 14d | mon | 10:00 |

Step and mask are held together rather than as an either/or day-selector, because fortnightly-on-a-weekday genuinely needs both at once. One rule computes the next fire for every slot schedule:

> candidate = `date(lastFired)` at the slot time; if that instant is not strictly after `lastFired`, add `step` days; then advance forward to the next day the mask allows.

That rule is what "predictable" means here. A manual anchor fire at 08:00 under `every 2d at 10:00` makes **today 10:00** the first scheduled fire, then every second day after — the human's first fire sets the phase, exactly as [ADR-0124](0124-routines-are-created-paused-and-anchored-by-a-manual-first-fire.md) already requires. A fire that lands late (daemon polls at 10:07) still schedules the next at 10:00, so slot schedules never accumulate drift the way rolling ones do. A biweekly routine anchored on a Wednesday snaps forward to the next Monday — a short first cycle, stable 2w cadence after.

Rejections are deliberate and each gets a targeted error naming the form to use instead: `h`/`m` intervals refuse `on`/`at` clauses (a time of day means nothing when the step is not whole days), ranges do not wrap (`on fri-mon` — write the comma list), sugar does not mix into lists (`on weekdays,sun` — write `on mon-fri,sun`), and clauses out of order are refused rather than reordered. `on weekdays`/`on weekends` sugar coexists with ranges because both are cheap over a mask; expressions are stored exactly as typed, so the dashboard shows the spelling the human chose.

Consequences: `daily at H[:MM][ utc]` becomes a **permanent parse-only alias**, not a deprecation — manifests store the raw expression and `routine/fingerprint.go` hashes it, so rewriting stored expressions would churn fingerprints and pause working routines under [ADR-0128](0128-any-failure-or-run-affecting-change-pauses-a-routine.md) for no gain. Nothing is migrated and no existing schedule changes meaning. The grammar blurb becomes one exported constant consumed by the parser error, both `--schedule` flag helps, the empty-list hint, and the authoring agent's framework contract, which were already drifting apart. ADR-0126's zone rule now covers every slot form: machine-local wall clock unless suffixed `utc`, with DST inherited from the local zone.

Known gaps, named rather than half-supported: no day-of-month form (`on the 1st`), and no fixed calendar epoch — a `every 2w` phase always derives from the anchor fire, never from an absolute week number.
