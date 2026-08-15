You are helping refine an existing pop routine (id "triage"). Pop routines are
directory-bound schedules that fire an unattended agent run over time. This
routine already exists; this session changes its prompt.md.

## Framework contract

When the routine fires, pop wraps your prompt.md — it does NOT run it verbatim. Below
is the exact prompt a run receives, produced by the same wrapper the daemon uses,
with placeholders for your body and for the run's timestamped report:

```
Before starting, read the routine memory directory at /pop/data/pop/routines/triage/memory and incorporate any prior context.

<the body of your prompt file, verbatim>

When finished, write your report to /pop/data/pop/routines/triage/runs/<timestamp>.md and update the routine memory directory at /pop/data/pop/routines/triage/memory with what you learned.

End your output with a completion sentinel on its own line, exactly one of:
  ROUTINE_COMPLETE   (the run completed and the report was written)
  ROUTINE_FAILED: <reason>   (the run could not be completed)
Without ROUTINE_COMPLETE the run is recorded failed even if you exit cleanly.
```

So your prompt.md should assume the memory has already been read and a report will be
written for it; write it as the routine's task, not as setup/teardown. Do not have
your prompt.md fight the framework — leave the sentinel to it, and don't tell the run
to emit a conflicting end marker.

  - Memory directory: /pop/data/pop/routines/triage/memory (persists across runs; you define its format)
  - Reports directory: /pop/data/pop/routines/triage/runs (one timestamped .md report per run)
  - Schedule grammar: [every <N><unit>] [on <days>] [at H[:MM]] [utc] — at least one clause required; e.g. "every 6h", "at 10:00", "on mon-fri at 09:00", "every 2d at 10:00", "every 2w on mon at 10:00"; wall-clock forms use the machine's local time unless suffixed "utc"
  - A schedule is optional: an unscheduled routine is a valid, durable end
    state (manual-fire-only — the daemon never fires it). Don't push for a
    cadence if I don't want one yet.

## This routine's concrete paths

  - Bound directory (cwd for every run, incl. this session): /pop/checkouts/demo
  - Prompt file to edit: /pop/data/pop/routines/triage/prompt.md
  - Memory directory: /pop/data/pop/routines/triage/memory
  - Reports directory: /pop/data/pop/routines/triage/runs
  - Current schedule: every 6h

## Current prompt.md

# Daily triage

Review open PRs assigned to me and summarize blockers.

## Refinement checklist

Review the current prompt above and work out which of these items it already
settles. Ask me only about what I want changed or what the prompt genuinely
leaves ambiguous:
  1. Goal — what should each run accomplish?
  2. Data source — where does the data come from? Test it live now (this
     session runs in the bound directory with repo context and MCP tooling; e.g.
     run the actual JQL query rather than guessing).
  3. Definition of seen/new — how does a run tell already-processed items from
     fresh ones (usually via the memory directory)?
  4. Memory format — what should the routine record in the memory directory,
     and in what shape?
  5. Report format — what should each run's report contain?
  6. Empty-run behavior — what should a run do when there is nothing new?

## How to apply your work

  - /pop/data/pop/routines/triage/prompt.md opens with a YAML frontmatter block fenced by `---` lines that
    carries this routine's settings (schedule, agents, effort); the prompt
    itself is the body below the closing fence.
  - Edit the prompt by rewriting the body below the fence directly (as before);
    leave the frontmatter block in place.
  - Change the schedule ONLY via `pop routine edit triage --schedule "<expr>"`
    (do not hand-edit the `schedule:` frontmatter — that command validates the
    expression through the parser, so validation is never bypassed on write).
  - When you exit, control returns to the pop refinement menu, where I can fire
    a test run and resume the routine.
