package tasks

import (
	"encoding/json"
	"sort"
	"strings"
)

func normalizeCursorStreamJSON(raw string) AgentResult {
	return normalizeResultStreamJSON(raw, nil)
}

// cursorLineRenderer renders cursor-agent stream-json events live. Assistant
// prose is INCREMENTAL: each "assistant" event carries only a delta chunk in
// message.content[].text, so deltas are emitted raw with NO newline framing
// (the divergence from claude, which frames terminal-per-message text). A
// tool_call with subtype "started" emits a dim "→ <toolName> <hint>" tick;
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
			ToolCall json.RawMessage `json:"tool_call"`
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
			toolName, args := cursorToolCall(event.ToolCall)
			if toolName == "" {
				return "", true
			}
			return dim(cursorToolTick(toolName, args)) + "\n", true
		default:
			return "", true
		}
	}
}

func cursorToolCall(raw json.RawMessage) (string, json.RawMessage) {
	if len(raw) == 0 {
		return "", nil
	}

	var legacy struct {
		Tool struct {
			Case  string `json:"case"`
			Value struct {
				Args json.RawMessage `json:"args"`
			} `json:"value"`
		} `json:"tool"`
	}
	if err := json.Unmarshal(raw, &legacy); err == nil && legacy.Tool.Case != "" {
		return legacy.Tool.Case, legacy.Tool.Value.Args
	}

	var keyed map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keyed); err != nil {
		return "", nil
	}
	names := make([]string, 0, len(keyed))
	for name := range keyed {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if name == "" {
			continue
		}
		// A live keyed tool_call carries the single <name>ToolCall entry — an
		// object — alongside sibling metadata: toolCallId (a string) and
		// hookAdditionalContexts (an array). Only the tool entry is a JSON
		// object, so decode each value on its own and skip any that is not an
		// object. This both ignores the metadata keys (which sort before
		// readToolCall and would otherwise be returned as the tool name) and
		// survives them — decoding the whole map into a struct-valued map fails
		// outright the moment one sibling value is an array.
		var entry struct {
			Args json.RawMessage `json:"args"`
		}
		if err := json.Unmarshal(keyed[name], &entry); err != nil {
			continue
		}
		return name, entry.Args
	}
	return "", nil
}

// cursorToolTick formats a compact "→ <toolName> hint" line. The tool name is
// the oneof case (e.g. readToolCall, shellToolCall) and the hint probes args.
func cursorToolTick(toolName string, args json.RawMessage) string {
	tick := "→ " + toolName
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
