---
status: accepted
---

# Live agent rendering is a cosmetic side-channel

> Refined by [ADR 0016](0016-captured-stream-is-a-durable-telemetry-substrate.md). The invariant below still holds — the live-render parse is never the source of truth — but "cosmetic" undersells what is now the primary user-facing flow. 0016 reframes it: the *captured stream* is the authoritative substrate (for completion assessment and for telemetry), and the live render is one derived view among several.

When an **Agent output adapter** runs in adapter mode (`auto`), pop renders the agent's activity live as it streams — assistant prose plus a compact `→` tick per tool use — instead of capturing silently and showing nothing until the process exits. The rendering is a pure side-channel: the structured stream is teed to a capture buffer unchanged, and the captured raw output, not the rendered view, remains the single source of truth for completion-sentinel assessment and **Agent quota detection**. The stream is therefore parsed twice — once incrementally for the live view, once post-hoc over the full capture for assessment.

## Why

The silence was an accident of incomplete implementation, not a feature: in adapter mode the agent's output went to a capture buffer only, so a long structured run printed the `── Agent output ──` divider and then nothing for minutes. The fix is to render progress live. The open question was whether the live pass should *become* the normalizer — a single streaming parse that both renders and produces the assessment result — or stay a cosmetic overlay with the post-hoc normalizer untouched.

We kept it cosmetic. The post-hoc path already owns the completion contract and quota detection and is unit-tested as pure functions over a raw string; folding assessment into a stateful streaming parser would entangle the contract logic with terminal-rendering concerns and partial-line buffering, and make a wrong live render able to corrupt an assessment. A side-channel keeps the two concerns independent: live rendering can be wrong, absent, or unimplemented for a given adapter without any effect on whether an issue is judged complete. The cost is parsing the stream twice, which is negligible against agent runtime.

## Considered Options

- **Streaming pass becomes the normalizer (single parse).** Rejected: couples the completion/quota contract to rendering and partial-line state; a rendering bug could change assessment; harder to keep the pure-function test seam.
- **Keep silent capture (status quo).** Rejected: the silence is the problem being fixed.
- **Render live for claude only, hard-coded.** Rejected in favor of a per-adapter line-renderer seam mirroring the existing per-format `normalize*` split — claude is implemented first, other structured adapters fall back to silent capture (today's behavior) until wired, with no claude-specific branch to remove later.

## Consequences

A generic tee-writer splits the process stream into complete lines, appends raw bytes to the capture buffer unchanged, and hands each line to the active adapter's line-renderer for `liveOut`. Lines that fail to parse as a known event pass through raw — they are almost certainly interleaved stderr, and seeing them live is most valuable when an agent is stuck or dying. Adapters without a line-renderer render nothing, preserving current behavior. The live view is pop-owned rendering, not the agent's bytes, so it may be lightly styled (dim tool ticks); plain-text mode still passes raw agent output through untouched per the existing **Agent output mode** contract. A future contributor should not route assessment or quota detection through the live renderer "to avoid the second parse" — the independence is the point.
