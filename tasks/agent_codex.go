package tasks

import (
	"encoding/json"
	"strings"
)

func normalizeCodexJSONL(raw string) AgentResult {
	var transcript string
	var diagnostics []string
	scanAgentJSONLines(raw, nil, func(line []byte) bool {
		var event struct {
			Type    string          `json:"type"`
			Message string          `json:"message"`
			Error   json.RawMessage `json:"error"`
			Item    struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"item"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			return false
		}
		switch event.Type {
		case "item.completed":
			if event.Item.Type == "agent_message" && event.Item.Text != "" {
				transcript = event.Item.Text
			}
		case "error", "turn.failed":
			if detail := agentJSONDiagnostic(event.Error); detail != "" {
				appendAgentDiagnostic(&diagnostics, detail)
			} else {
				appendAgentDiagnostic(&diagnostics, event.Message)
			}
		}
		return true
	})
	return normalizedTranscript(transcript, diagnostics)
}

// codexLineRenderer renders codex-jsonl Thread Events live: assistant prose is
// emitted whole on item.completed for an agent_message item, and a dim
// "→ kind hint" tick is emitted on item.started for each tool/command item.
// Reasoning, todo_list, lifecycle events, and errors render nothing (the
// normalizer surfaces errors); non-JSON lines are reported as unhandled so the
// writer passes them through raw.
//
// Prose is emitted only on item.completed (the cumulative final text), never on
// item.updated, so the renderer is correct regardless of whether item.updated.text
// is a cumulative snapshot or a delta — one of the open items that could not be
// confirmed against a live authenticated run (no codex auth / installed v0.7.0
// predates --json). mcp_tool_call.arguments is probed as both an object and a
// JSON string, so it is also robust to that open item.
func codexLineRenderer(color bool) lineRenderer {
	dim := func(s string) string {
		if !color {
			return s
		}
		return ansiDim + s + ansiReset
	}
	return func(line []byte) (string, bool) {
		var event struct {
			Type string `json:"type"`
			Item struct {
				Type      string          `json:"type"`
				Text      string          `json:"text"`
				Command   string          `json:"command"`
				Tool      string          `json:"tool"`
				Server    string          `json:"server"`
				Arguments json.RawMessage `json:"arguments"`
				Query     string          `json:"query"`
				Changes   []struct {
					Path string `json:"path"`
					Kind string `json:"kind"`
				} `json:"changes"`
			} `json:"item"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			return "", false
		}
		switch event.Type {
		case "item.completed":
			if event.Item.Type == "agent_message" {
				if text := strings.TrimRight(event.Item.Text, "\n"); text != "" {
					return text + "\n", true
				}
			}
			return "", true
		case "item.started":
			switch event.Item.Type {
			case "command_execution", "mcp_tool_call", "file_change", "web_search":
				var changePath string
				if len(event.Item.Changes) > 0 {
					changePath = event.Item.Changes[0].Path
				}
				hint := codexItemHint(
					event.Item.Command,
					event.Item.Tool,
					event.Item.Server,
					codexArgumentsHint(event.Item.Arguments),
					changePath,
					event.Item.Query,
				)
				return dim(codexItemTick(event.Item.Type, hint)) + "\n", true
			}
			return "", true
		default:
			return "", true
		}
	}
}

// codexItemTick formats a compact "→ kind hint" line, where kind is the item
// type discriminator and hint is the first salient field found.
func codexItemTick(kind, hint string) string {
	tick := "→ " + kind
	if hint != "" {
		tick += " " + hint
	}
	return tick
}

// codexItemHint returns the first non-empty probe value, collapsed to a single
// line and truncated to ~80 chars, matching claudeToolHint.
func codexItemHint(values ...string) string {
	hint := firstNonEmpty(values...)
	hint = strings.TrimSpace(strings.ReplaceAll(hint, "\n", " "))
	if len(hint) > 80 {
		hint = hint[:77] + "..."
	}
	return hint
}

// codexArgumentsHint extracts a hint from mcp_tool_call.arguments, whose JSON
// type (object vs string) is unconfirmed against a live run. A JSON string is
// used verbatim; an object yields no hint (the tool/server name already names
// the call).
func codexArgumentsHint(args json.RawMessage) string {
	if len(args) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(args, &s); err == nil {
		return s
	}
	return ""
}
