You are implementing the task at: /pop/tasks/2026-05-01-demo/04-afk.md

Read the task file in full. Follow any optional context references it
contains (for example a "## Parent" section) when present; the task may also
be self-contained. Implement the work described under "What to build" and
satisfy every box under "Acceptance criteria". As you complete each
criterion, check its box (`- [ ]` → `- [x]`) in /pop/tasks/2026-05-01-demo/04-afk.md.

Do NOT modify /pop/tasks/2026-05-01-demo/index.json. Do NOT modify other task files in /pop/tasks/2026-05-01-demo.
Do NOT make git commits — the runner handles assessment and committing.

Runtime checkout: /pop/checkouts/demo

Implementation edits belong only beneath the runtime checkout. The task file
above is the one file you also edit — its acceptance boxes are yours to tick.

## Before you hand-write mechanism

The trigger is your own output: when what you are about to write is plumbing — a
state flag, a `catch` that turns an error into a message, a success notice, a
confirmation, a retry around a call — stop and look for the composed form this
repository already has for it. Hand-written mechanism is the signal, not where
the change lands: being in the right file is not the same as transferring from
it.

Search cheapest first and stop at the first hit: the files the task's
Orientation names, then the directory the change lands in, then the repository.
Adopt what you find. Write the plumbing by hand only when the ladder ends empty.

## This repository's implementation convention

The rules below are this repository's standard for the code you write. Follow
them as you build: a Refine pass checks the work against this same text, and
what you leave short of it is fixed or reported there.

----- ANSWER: SHIPPED (pop's own) -----
conventions/shipped/implementation.md

Name things after what they are in this repository's language.
Keep a function's abstraction level uniform.

Rules: pop's own (shipped). No project or overlay document for this kind.

This attempt is a single non-interactive session. There is no human and no
later turn: once you end your response the attempt is over, and ending
without a completion sentinel (TASK_COMPLETE or TASK_FAILED) is recorded as a
failure. To wait on a long-running command, keep polling it across successive
bash calls until it finishes (or fails) — never background the work and end
your turn to "wait", which orphans it and yields no sentinel. A single bash
call may be killed at its own tool timeout (~10 min), but the whole attempt
has a far longer timeout (~1 hour), so poll across calls rather than waiting
within one.

Your context is billed on every turn and only grows within the attempt, so
the attempt's cost rises with the square of how many tool calls you make.
Probe wide once rather than laddering narrowing greps; read the ranges of a
file you need instead of whole large files; never re-run a command or re-read
a file whose output is already in this session; chain setup and command in one
shell call instead of repeating cd or env lines. Images are never evicted —
read one only when visual judgement is the question.

When you have completed the work, close out in this order:

1. Re-read the task file and tick every box under "Acceptance criteria" that
   you have satisfied (`- [ ]` → `- [x]`). An attempt that leaves a box
   unticked is recorded as failed even when the work itself landed.
2. Name in your summary the existing implementation you matched your new
   mechanism against, or say plainly that there was no prior art to match.
3. Print a summary block followed by the completion sentinel as the final
   lines of your output, exactly:

SUMMARY_START
<one or more lines describing what you did>
SUMMARY_END
TASK_COMPLETE

If you cannot complete the task (blocked, unclear, missing info, repeated
failure), instead print as the final line:

TASK_FAILED: <one-line reason>
