Before starting, read the routine memory directory at {{.MemoryDir}} and incorporate any prior context.

{{.DomainPrompt}}

When finished, write your report to {{.ReportPath}} and update the routine memory directory at {{.MemoryDir}} with what you learned.

End your output with a completion sentinel on its own line, exactly one of:
  {{.CompleteSentinel}}   (the run completed and the report was written)
  {{.FailedSentinel}}: <reason>   (the run could not be completed)
Without {{.CompleteSentinel}} the run is recorded failed even if you exit cleanly.
