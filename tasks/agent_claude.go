package tasks

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
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

// claudeToolTimings derives per-tool durations from one stored Captured
// attempt stream: each assistant tool_use block is paired with the user
// tool_result block carrying the same tool-use id, and the gap between their
// arrival times is that invocation's duration. Ids — not order — do the
// pairing, so parallel tool calls within one assistant turn resolve correctly.
// A tool_use with no result (e.g. a killed attempt) contributes nothing.
// Results aggregate per tool name, longest total first.
func claudeToolTimings(events []streamEventRecord) []ToolTiming {
	type pendingUse struct {
		name string
		atMS int64
	}
	pending := map[string]pendingUse{}
	totals := map[string]*ToolTiming{}
	for _, ev := range events {
		var msg struct {
			Type    string `json:"type"`
			Message struct {
				Content []struct {
					Type      string `json:"type"`
					ID        string `json:"id"`
					Name      string `json:"name"`
					ToolUseID string `json:"tool_use_id"`
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(ev.Raw), &msg); err != nil {
			continue
		}
		switch msg.Type {
		case "assistant":
			for _, c := range msg.Message.Content {
				if c.Type == "tool_use" && c.ID != "" {
					pending[c.ID] = pendingUse{name: c.Name, atMS: ev.AtMS}
				}
			}
		case "user":
			for _, c := range msg.Message.Content {
				if c.Type != "tool_result" {
					continue
				}
				use, ok := pending[c.ToolUseID]
				if !ok {
					continue
				}
				delete(pending, c.ToolUseID)
				agg := totals[use.name]
				if agg == nil {
					agg = &ToolTiming{Name: use.name}
					totals[use.name] = agg
				}
				agg.Count++
				agg.Total += time.Duration(ev.AtMS-use.atMS) * time.Millisecond
			}
		}
	}
	out := make([]ToolTiming, 0, len(totals))
	for _, t := range totals {
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Total != out[j].Total {
			return out[i].Total > out[j].Total
		}
		return out[i].Name < out[j].Name
	})
	return out
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
