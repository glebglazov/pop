package workload

import (
	"encoding/json"
	"strings"
)

func normalizeClaudeStreamJSON(raw string) AgentResult {
	return normalizeResultStreamJSON(raw, claudeQuotaPauseReason)
}

// claudeLineRenderer renders claude stream-json events live: assistant prose
// plain, and a dim "→ Tool hint" tick per tool use. Other event types render
// nothing; non-JSON lines are reported as unhandled so the writer passes them
// through raw.
func claudeLineRenderer(color bool) lineRenderer {
	dim := func(s string) string {
		if !color {
			return s
		}
		return ansiDim + s + ansiReset
	}
	return func(line []byte) (string, bool) {
		var event struct {
			Type    string `json:"type"`
			Message struct {
				Content []struct {
					Type  string          `json:"type"`
					Text  string          `json:"text"`
					Name  string          `json:"name"`
					Input json.RawMessage `json:"input"`
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			return "", false
		}
		if event.Type != "assistant" {
			return "", true
		}
		var b strings.Builder
		for _, c := range event.Message.Content {
			switch c.Type {
			case "text":
				if text := strings.TrimRight(c.Text, "\n"); text != "" {
					b.WriteString(text)
					b.WriteByte('\n')
				}
			case "tool_use":
				b.WriteString(dim(claudeToolTick(c.Name, c.Input)))
				b.WriteByte('\n')
			}
		}
		return b.String(), true
	}
}

// claudeToolTick formats a compact "→ Name hint" line, probing the tool input
// for the first recognized salient key without knowing per-tool schemas.
func claudeToolTick(name string, input json.RawMessage) string {
	tick := "→ " + name
	hint := claudeToolHint(input)
	if hint != "" {
		tick += " " + hint
	}
	return tick
}

func claudeToolHint(input json.RawMessage) string {
	if len(input) == 0 {
		return ""
	}
	var probe struct {
		FilePath string `json:"file_path"`
		Path     string `json:"path"`
		Command  string `json:"command"`
		Pattern  string `json:"pattern"`
		URL      string `json:"url"`
		Query    string `json:"query"`
	}
	if err := json.Unmarshal(input, &probe); err != nil {
		return ""
	}
	hint := firstNonEmpty(probe.FilePath, probe.Path, probe.Command, probe.Pattern, probe.URL, probe.Query)
	hint = strings.TrimSpace(strings.ReplaceAll(hint, "\n", " "))
	if len(hint) > 80 {
		hint = hint[:77] + "..."
	}
	return hint
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func claudeQuotaPauseReason(result string) *AgentQuotaPause {
	for _, marker := range []string{
		"You've hit your session limit",
		"You've hit your weekly limit",
		"You've hit your Opus limit",
	} {
		if strings.Contains(result, marker) {
			return &AgentQuotaPause{Reason: result}
		}
	}
	return nil
}
