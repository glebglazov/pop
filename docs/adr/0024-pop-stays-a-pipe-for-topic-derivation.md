# Pop stays a pipe for Topic derivation

Pop derives a pane's **Topic** without ever linking a model SDK or managing API keys. By default it truncates the user's prompt (~8 words) to a built-in label. For better quality, the user sets `topic_command` under `[pane_monitoring]` to *any* shell command — a local model, a cloud CLI, a script. Pop normalizes whatever the agent's hook handed it into a stable JSON object on stdin and reads the resulting topic off stdout. Pop owns the contract; the user owns the model, keys, and runtime. This keeps Pop's first foray into summarization config-free (consistent with ADR-0001) and lets users run fully local models, at the cost of a turnkey experience.

The JSON contract pop passes to `topic_command`:

```json
{ "prev_topic": "...", "prompt": "...", "transcript_path": "/…/abc.jsonl", "pane_id": "%5", "session": "..." }
```

`prompt` is always present; `transcript_path` is passed through only when the agent's hook exposes one, and parsing it (e.g. to read the last assistant turn) is the command's responsibility, not pop's — this is what keeps pop agent-agnostic. The command's stdout is trimmed, first line, capped, and becomes the new Topic.

## Considered Options

- **Bundle an Anthropic/Haiku call into pop** (`topic_model = "haiku"` + `ANTHROPIC_API_KEY`). Rejected: turnkey, but couples pop to a provider, forces key management into a tool that currently makes zero model calls, and excludes users who want a local model.
- **Have the Pane skill make the agent self-report its own topic.** Rejected earlier in design: pollutes the working agent's context with unrelated instructions and tool calls — the opposite of low-key.
- **Have pop parse each agent's transcript itself.** Rejected: drags per-agent transcript-format knowledge into pop's core; passing `transcript_path` through to the user command keeps that variability out of pop.

## Consequences

- Pop makes no outbound network calls of its own; all model cost, latency, and credentials live in the user's `topic_command`.
- On command failure, timeout (~5s), or empty output, pop keeps the previous Topic and never blocks the agent; built-in truncation is the last resort when there is no previous Topic.
- The stdin JSON shape is a published contract others build commands against, so adding/renaming fields is a compatibility concern — additive changes only.
