## Framework contract

When the routine fires, pop wraps {{.PromptNoun}} — it does NOT run it verbatim. Below
is the exact prompt a run receives, produced by the same wrapper the daemon uses,
with placeholders for your body and for the run's timestamped report:

```
{{.WrappedExample}}
```

So {{.PromptNoun}} should assume the memory has already been read and a report will be
written for it; write it as the routine's task, not as setup/teardown. Do not have
{{.PromptNoun}} fight the framework — leave the sentinel to it, and don't tell the run
to emit a conflicting end marker.
