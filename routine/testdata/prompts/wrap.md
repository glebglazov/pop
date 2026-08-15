Before starting, read the routine memory directory at /pop/data/pop/routines/triage/memory and incorporate any prior context.

# Daily triage

Review open PRs assigned to me and summarize blockers.

When finished, write your report to /pop/data/pop/routines/triage/runs/2026-05-01T09-00-00Z.md and update the routine memory directory at /pop/data/pop/routines/triage/memory with what you learned.

End your output with a completion sentinel on its own line, exactly one of:
  ROUTINE_COMPLETE   (the run completed and the report was written)
  ROUTINE_FAILED: <reason>   (the run could not be completed)
Without ROUTINE_COMPLETE the run is recorded failed even if you exit cleanly.
