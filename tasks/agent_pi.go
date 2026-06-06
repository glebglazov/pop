package tasks

import (
	"encoding/json"
	"strings"
)

func normalizePiJSONL(raw string) AgentResult {
	var transcript string
	var diagnostics []string
	scanAgentJSONLines(raw, nil, func(line []byte) bool {
		var event struct {
			Type    string `json:"type"`
			Message struct {
				Role         string `json:"role"`
				ErrorMessage string `json:"errorMessage"`
				Content      []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			return false
		}
		if event.Type != "message_end" || event.Message.Role != "assistant" {
			return true
		}
		if event.Message.ErrorMessage != "" {
			appendAgentDiagnostic(&diagnostics, event.Message.ErrorMessage)
		}
		var message string
		for _, content := range event.Message.Content {
			if content.Type == "text" {
				message += content.Text
			}
		}
		if message != "" {
			transcript = message
		}
		return true
	})
	return normalizedTranscript(transcript, diagnostics)
}

// piLineRenderer renders pi-jsonl events live. Assistant prose streams
// incrementally: text_delta sub-events emit their raw delta with no trailing
// newline and a single newline is emitted on text_end. tool_execution_start
// emits a dim "→ toolName hint" tick. Assistant error messages are surfaced
// live; thinking_* and other lifecycle/framing events render nothing. Non-JSON
// lines are reported as unhandled so the writer passes them through raw.
func piLineRenderer(color bool) lineRenderer {
	dim := func(s string) string {
		if !color {
			return s
		}
		return ansiDim + s + ansiReset
	}
	return func(line []byte) (string, bool) {
		var event struct {
			Type                  string          `json:"type"`
			ToolName              string          `json:"toolName"`
			Args                  json.RawMessage `json:"args"`
			AssistantMessageEvent struct {
				Type  string `json:"type"`
				Delta string `json:"delta"`
			} `json:"assistantMessageEvent"`
			Message struct {
				Role         string `json:"role"`
				ErrorMessage string `json:"errorMessage"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			return "", false
		}
		switch event.Type {
		case "message_update":
			switch event.AssistantMessageEvent.Type {
			case "text_delta":
				return event.AssistantMessageEvent.Delta, true
			case "text_end":
				return "\n", true
			default:
				return "", true
			}
		case "tool_execution_start":
			return dim(piToolTick(event.ToolName, event.Args)) + "\n", true
		case "message_end":
			if event.Message.Role == "assistant" && event.Message.ErrorMessage != "" {
				return event.Message.ErrorMessage + "\n", true
			}
			return "", true
		default:
			return "", true
		}
	}
}

// piToolTick formats a compact "→ toolName hint" line, probing the args for
// the first recognized salient key. pi's read tool uses path (not file_path),
// so both are probed.
func piToolTick(name string, args json.RawMessage) string {
	tick := "→ " + name
	hint := piToolHint(args)
	if hint != "" {
		tick += " " + hint
	}
	return tick
}

func piToolHint(args json.RawMessage) string {
	if len(args) == 0 {
		return ""
	}
	var probe struct {
		Path     string `json:"path"`
		FilePath string `json:"file_path"`
		Command  string `json:"command"`
		Pattern  string `json:"pattern"`
		URL      string `json:"url"`
		Query    string `json:"query"`
	}
	if err := json.Unmarshal(args, &probe); err != nil {
		return ""
	}
	hint := firstNonEmpty(probe.Path, probe.FilePath, probe.Command, probe.Pattern, probe.URL, probe.Query)
	hint = strings.TrimSpace(strings.ReplaceAll(hint, "\n", " "))
	if len(hint) > 80 {
		hint = hint[:77] + "..."
	}
	return hint
}
