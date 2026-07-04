package tasks

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Some reasoning models (e.g. qwen via opencode-go) wrap chain-of-thought in
// literal <think>...</think> tags. pi routes the reasoning body to the thinking
// channel (suppressed), but the closing </think> tag plus trailing whitespace
// leaks into the text channel as its own content block, surfacing as a stray
// "</think>" entry live. thinkSpanRe drops complete spans (if a whole block ever
// leaks as text); thinkTagRe drops orphan opening/closing tags left behind when
// only one side leaks.
var (
	thinkSpanRe = regexp.MustCompile(`(?s)<think>.*?</think>`)
	thinkTagRe  = regexp.MustCompile(`</?think>`)
)

// stripThinkTags removes leaked reasoning tags from pi prose. The remainder is
// returned untrimmed so callers decide how to handle whitespace-only results.
func stripThinkTags(s string) string {
	s = thinkSpanRe.ReplaceAllString(s, "")
	return thinkTagRe.ReplaceAllString(s, "")
}

func normalizePiJSONL(raw string) AgentResult {
	if pause := opencodeGoQuotaPauseReason(raw); pause != nil {
		return AgentResult{QuotaPause: pause}
	}
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
			transcript = strings.TrimSpace(stripThinkTags(message))
		}
		return true
	})
	return normalizedTranscript(transcript, diagnostics)
}

// piLineRenderer renders pi-jsonl events live. pi streams assistant prose as
// many token-level text_delta sub-events, each on its own JSONL line; the
// line-based live writer would prefix and newline-terminate every one, scattering
// a single sentence across dozens of "+0.0s" entries. So deltas are buffered and
// the message is emitted as one entry when text_end closes it (a tool tick or
// message_end also drains any open prose, in case the close is missing).
// tool_execution_start emits a dim "→ toolName hint" tick. Assistant error
// messages are surfaced live; thinking_* and other lifecycle/framing events
// render nothing. Non-JSON lines are reported as unhandled so the writer passes
// them through raw.
func piLineRenderer(color bool) lineRenderer {
	dim := func(s string) string {
		if !color {
			return s
		}
		return ansiDim + s + ansiReset
	}
	var prose strings.Builder
	flushProse := func() string {
		if prose.Len() == 0 {
			return ""
		}
		text := strings.TrimSpace(stripThinkTags(prose.String()))
		prose.Reset()
		if text == "" {
			return ""
		}
		return text + "\n"
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
				prose.WriteString(event.AssistantMessageEvent.Delta)
				return "", true
			case "text_end":
				return flushProse(), true
			default:
				return "", true
			}
		case "tool_execution_start":
			return flushProse() + dim(piToolTick(event.ToolName, event.Args)) + "\n", true
		case "message_end":
			out := flushProse()
			if event.Message.Role == "assistant" && event.Message.ErrorMessage != "" {
				return out + event.Message.ErrorMessage + "\n", true
			}
			return out, true
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
