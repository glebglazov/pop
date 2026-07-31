package tasks

import "fmt"

// renderStreamEvents converts raw stream events into a readable sequence for
// the tracer. Supported adapters render each event; render-blind adapters
// refuse once with a named reason instead of dumping raw JSON (ADR-0165).
func renderStreamEvents(agent string, events []streamEventRecord) []StreamEvent {
	if len(events) == 0 {
		return nil
	}
	adapter, ok := agentAdapters[agent]
	if !ok {
		return []StreamEvent{{
			Type: "render_refused",
			Text: streamRenderRefusal(agent, "unknown agent adapter"),
		}}
	}
	cap := adapter.StreamRenderCapability()
	if cap.Kind != CapabilitySupported || cap.Render == nil {
		return []StreamEvent{{
			Type: "render_refused",
			Text: streamRenderRefusal(agent, cap.Reason),
		}}
	}
	var out []StreamEvent
	for _, ev := range events {
		out = append(out, cap.Render(ev)...)
	}
	return out
}

func streamRenderRefusal(agent, reason string) string {
	return fmt.Sprintf("%s: stream cannot be normalized — %s", agent, reason)
}
