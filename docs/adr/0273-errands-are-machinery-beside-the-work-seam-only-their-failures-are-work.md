# Errands are machinery beside the Work seam; only their failures are Work

An **Errand** could have been a `work.Kind`, which would have handed it
dashboard rows, `Actions`/`Perform` and the daemon's dispatch loop for free. It
is not one. That seam models containers a human tracks over days — `Columns`,
`StatusCell` and detail `Sections` all have to mean something — and a delete
that lives 400ms would stretch every one of them to say nothing.

A human never tracks a successful errand. They track a **stuck** one. So the
queue is machinery beside the seam, with its own table and its own tick in the
supervisor loop, and the **Errand failure** is the object that carries a kind, a
status and verbs onto the Work dashboard.

The record follows from that. It is keyed on the errand's **subject**, not on
the errand, so a retry overwrites instead of accumulating — the same shape as
`verify_verdicts`. Its states are queued → running → cleared or failed, and the
`running` state is the one that matters: an errand still marked running when the
daemon starts is an **Errand failure** with the interruption as its output.
Without it the design rebuilds the very bug it exists to fix, one level up — an
operation that looks queued and a checkout that is half gone. A finished errand
clears its row and leaves a **Work journal** line, so "forget on success" never
means "no trace". The output lives in a document and the row holds a pointer to
it, per ADR-0245: a failed removal may need to name several thousand surviving
paths, which does not belong in SQLite. And the row carries retry and dismiss,
because a read-only failure list is what those eight orphan directories already
were.

Destructive errands are attempted once and never retried automatically. Backoff
is right for a quota cooldown and wrong for a recursive delete: an errand that
keeps failing keeps racing whatever is writing into the directory, so the retry
is the human's verb.

## Consequences

The daemon has two dispatch mechanisms — `work.Advancer` for Work, and the
errand tick beside it. That duplication is deliberate, and cheaper than making
the Work seam mean two different things.
