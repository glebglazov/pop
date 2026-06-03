package workload

import (
	"encoding/json"
	"strings"
)

func normalizeCursorStreamJSON(raw string) AgentResult {
	return normalizeResultStreamJSON(raw, nil)
}

// cursorLineRenderer renders cursor-agent stream-json events live. Assistant
// prose is INCREMENTAL: each "assistant" event carries only a delta chunk in
// message.content[].text, so deltas are emitted raw with NO newline framing
// (the divergence from claude, which frames terminal-per-message text). A
// tool_call with subtype "started" emits a dim "→ <tool.case> <hint>" tick;
// "completed" is skipped to avoid double ticks. system/user/result and unknown
// types render nothing; non-JSON lines are reported unhandled so the writer
// passes them through raw.
func cursorLineRenderer(color bool) lineRenderer {
	dim := func(s string) string {
		if !color {
			return s
		}
		return ansiDim + s + ansiReset
	}
	return func(line []byte) (string, bool) {
		var event struct {
			Type    string `json:"type"`
			Subtype string `json:"subtype"`
			Message struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
			ToolCall struct {
				Tool struct {
					Case  string `json:"case"`
					Value struct {
						Args json.RawMessage `json:"args"`
					} `json:"value"`
				} `json:"tool"`
			} `json:"tool_call"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			return "", false
		}
		switch event.Type {
		case "assistant":
			var b strings.Builder
			for _, c := range event.Message.Content {
				if c.Type == "text" {
					b.WriteString(c.Text)
				}
			}
			return b.String(), true
		case "tool_call":
			if event.Subtype != "started" {
				return "", true
			}
			return dim(cursorToolTick(event.ToolCall.Tool.Case, event.ToolCall.Tool.Value.Args)) + "\n", true
		default:
			return "", true
		}
	}
}

// cursorToolTick formats a compact "→ <case> hint" line. The tool name is the
// oneof case (e.g. readToolCall, shellToolCall) and the hint probes args.
func cursorToolTick(toolCase string, args json.RawMessage) string {
	tick := "→ " + toolCase
	hint := cursorToolHint(args)
	if hint != "" {
		tick += " " + hint
	}
	return tick
}

func cursorToolHint(args json.RawMessage) string {
	if len(args) == 0 {
		return ""
	}
	var probe struct {
		Command     string `json:"command"`
		Pattern     string `json:"pattern"`
		GlobPattern string `json:"globPattern"`
		Query       string `json:"query"`
		Path        string `json:"path"`
		URL         string `json:"url"`
	}
	if err := json.Unmarshal(args, &probe); err != nil {
		return ""
	}
	hint := firstNonEmpty(probe.Command, probe.Pattern, probe.GlobPattern, probe.Query, probe.Path, probe.URL)
	hint = strings.TrimSpace(strings.ReplaceAll(hint, "\n", " "))
	if len(hint) > 80 {
		hint = hint[:77] + "..."
	}
	return hint
}
