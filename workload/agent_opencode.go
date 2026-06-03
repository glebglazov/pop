package workload

import (
	"encoding/json"
	"strings"
)

func normalizeOpenCodeJSON(raw string) AgentResult {
	var transcript strings.Builder
	var diagnostics []string
	scanAgentJSONLines(raw, nil, func(line []byte) bool {
		var event struct {
			Type  string          `json:"type"`
			Error json.RawMessage `json:"error"`
			Part  struct {
				Text string `json:"text"`
			} `json:"part"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			return false
		}
		switch event.Type {
		case "text":
			transcript.WriteString(event.Part.Text)
		case "error":
			appendAgentDiagnostic(&diagnostics, agentJSONDiagnostic(event.Error))
		}
		return true
	})
	return normalizedTranscript(transcript.String(), diagnostics)
}

// openCodeLineRenderer renders opencode JSONL events live: assistant prose
// plain, and a dim "→ tool hint" tick per tool use. step_start, step_finish,
// and error render nothing; non-JSON lines are reported as unhandled so the
// writer passes them through raw. Each text event carries a whole-message
// chunk, so per-line newline framing works without fragment handling.
func openCodeLineRenderer(color bool) lineRenderer {
	dim := func(s string) string {
		if !color {
			return s
		}
		return ansiDim + s + ansiReset
	}
	return func(line []byte) (string, bool) {
		var event struct {
			Type string `json:"type"`
			Part struct {
				Text  string `json:"text"`
				Tool  string `json:"tool"`
				Title string `json:"title"`
				State struct {
					Input json.RawMessage `json:"input"`
				} `json:"state"`
			} `json:"part"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			return "", false
		}
		switch event.Type {
		case "text":
			if text := strings.TrimRight(event.Part.Text, "\n"); text != "" {
				return text + "\n", true
			}
			return "", true
		case "tool_use":
			return dim(openCodeToolTick(event.Part.Tool, event.Part.State.Input, event.Part.Title)) + "\n", true
		default:
			return "", true
		}
	}
}

// openCodeToolTick formats a compact "→ tool hint" line, probing the tool
// input for the first recognized salient key, then falling back to the
// pre-rendered part.title.
func openCodeToolTick(tool string, input json.RawMessage, title string) string {
	tick := "→ " + tool
	hint := openCodeToolHint(input)
	if hint == "" {
		hint = strings.TrimSpace(strings.ReplaceAll(title, "\n", " "))
		if len(hint) > 80 {
			hint = hint[:77] + "..."
		}
	}
	if hint != "" {
		tick += " " + hint
	}
	return tick
}

func openCodeToolHint(input json.RawMessage) string {
	if len(input) == 0 {
		return ""
	}
	var probe struct {
		FilePath string `json:"filePath"`
		Path     string `json:"path"`
		Command  string `json:"command"`
		Pattern  string `json:"pattern"`
		Query    string `json:"query"`
		URL      string `json:"url"`
	}
	if err := json.Unmarshal(input, &probe); err != nil {
		return ""
	}
	hint := firstNonEmpty(probe.FilePath, probe.Path, probe.Command, probe.Pattern, probe.Query, probe.URL)
	hint = strings.TrimSpace(strings.ReplaceAll(hint, "\n", " "))
	if len(hint) > 80 {
		hint = hint[:77] + "..."
	}
	return hint
}
