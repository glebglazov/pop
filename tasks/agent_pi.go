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
		return AgentResult{Unavailability: pause}
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
	return toolTick(name, piToolHint(args))
}

type piToolHintProbe struct {
	Path     string `json:"path"`
	FilePath string `json:"file_path"`
	Command  string `json:"command"`
	Pattern  string `json:"pattern"`
	URL      string `json:"url"`
	Query    string `json:"query"`
}

func piToolHint(args json.RawMessage) string {
	return toolHint(args, func(p piToolHintProbe) string {
		return firstNonEmpty(p.Path, p.FilePath, p.Command, p.Pattern, p.URL, p.Query)
	})
}

// piTokenUsage is pi's Usage extraction rule (ADR-0160).
//
// Authoritative events: `message_end` events whose message carries a usage
// block under keys input / output / cacheRead / cacheWrite. That block is
// the settled per-message total. `message_update` deltas also carry a
// *cumulative* usage block on every partial — summing those over-counts by
// roughly the number of deltas per message (measured ~4× on real runs).
//
// Semantics: accumulate — sum every message_end usage block; ignore all
// message_update deltas. A present usage object reports its fields even
// when zero (Has* true), distinct from a Token-blind absence.
func piTokenUsage(events []streamEventRecord) TokenUsage {
	var u TokenUsage
	for _, ev := range events {
		var event struct {
			Type    string `json:"type"`
			Message struct {
				Usage *struct {
					Input      *int64 `json:"input"`
					Output     *int64 `json:"output"`
					CacheRead  *int64 `json:"cacheRead"`
					CacheWrite *int64 `json:"cacheWrite"`
				} `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(ev.Raw), &event); err != nil {
			continue
		}
		if event.Type != "message_end" || event.Message.Usage == nil {
			continue
		}
		usage := event.Message.Usage
		if v := usage.Input; v != nil {
			u.Input += *v
			u.HasInput = true
		}
		if v := usage.Output; v != nil {
			u.Output += *v
			u.HasOutput = true
		}
		if v := usage.CacheRead; v != nil {
			u.CacheRead += *v
			u.HasCacheRead = true
		}
		if v := usage.CacheWrite; v != nil {
			u.CacheWrite += *v
			u.HasCacheWrite = true
		}
	}
	return u
}

// piPartialCost is pi's cost extraction rule (ADR-0160).
//
// Authoritative events: `message_end` events whose message carries a settled
// cost total. On real pi streams the dollars live under usage.cost.total;
// message.cost.total is accepted when present. Component keys
// (input/output/cacheRead/cacheWrite) must not be summed alongside total.
// message_update deltas are ignored, matching pi's token rule.
func piPartialCost(events []streamEventRecord) PartialCost {
	var c PartialCost
	for _, ev := range events {
		var event struct {
			Type    string `json:"type"`
			Message struct {
				Cost *struct {
					Total *float64 `json:"total"`
				} `json:"cost"`
				Usage *struct {
					Cost *struct {
						Total *float64 `json:"total"`
					} `json:"cost"`
				} `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(ev.Raw), &event); err != nil {
			continue
		}
		if event.Type != "message_end" {
			continue
		}
		var total *float64
		if event.Message.Cost != nil {
			total = event.Message.Cost.Total
		} else if event.Message.Usage != nil && event.Message.Usage.Cost != nil {
			total = event.Message.Usage.Cost.Total
		}
		if total != nil {
			c.Dollars += *total
			c.HasCost = true
		}
	}
	return c
}

// piActualModel is pi's actual-model extraction rule (ADR-0165).
//
// Authoritative field: message.model on assistant-role events. pi repeats the
// resolved model on every message lifecycle event; the last assistant message
// wins.
func piActualModel(events []streamEventRecord) string {
	var model string
	for _, ev := range events {
		var event struct {
			Message struct {
				Role  string `json:"role"`
				Model string `json:"model"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(ev.Raw), &event); err != nil {
			continue
		}
		if event.Message.Role == "assistant" {
			if m := strings.TrimSpace(event.Message.Model); m != "" {
				model = m
			}
		}
	}
	return model
}

// renderPiEvent parses one pi-jsonl event into readable stream entries.
func renderPiEvent(ev streamEventRecord) []StreamEvent {
	var out []StreamEvent

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
			Content      []struct {
				Type string `json:"type"`
				Name string `json:"name"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(ev.Raw), &event); err != nil {
		return []StreamEvent{{
			AtMS: ev.AtMS,
			Type: "raw",
			Text: ev.Raw,
		}}
	}

	switch event.Type {
	case "message_update":
		if event.AssistantMessageEvent.Type == "text_delta" {
			if text := strings.TrimRight(event.AssistantMessageEvent.Delta, "\n"); text != "" {
				out = append(out, StreamEvent{
					AtMS: ev.AtMS,
					Type: "assistant",
					Text: text,
				})
			}
		}
	case "tool_execution_start":
		name := event.ToolName
		if name == "" {
			name = "tool"
		}
		args := ""
		if len(event.Args) > 0 {
			args = string(event.Args)
		}
		out = append(out, StreamEvent{
			AtMS:     ev.AtMS,
			Type:     "tool_use",
			ToolName: name,
			ToolArgs: args,
		})
	case "message_end":
		if event.Message.Role == "assistant" && event.Message.ErrorMessage != "" {
			out = append(out, StreamEvent{
				AtMS: ev.AtMS,
				Type: "assistant",
				Text: event.Message.ErrorMessage,
			})
		}
		for _, c := range event.Message.Content {
			if c.Type == "toolCall" && c.Name != "" {
				out = append(out, StreamEvent{
					AtMS:     ev.AtMS,
					Type:     "tool_use",
					ToolName: c.Name,
				})
			}
		}
	}

	return out
}

// tool_execution_start is paired with tool_execution_end sharing toolCallId,
// and the gap between their arrival times is that invocation's duration.
func piToolTimings(events []streamEventRecord) ([]ToolTiming, []toolWindow) {
	return accumulateToolTimings(events, func(ev streamEventRecord) ([]toolOpen, []toolClose) {
		var event struct {
			Type       string `json:"type"`
			ToolCallId string `json:"toolCallId"`
			ToolName   string `json:"toolName"`
		}
		if err := json.Unmarshal([]byte(ev.Raw), &event); err != nil {
			return nil, nil
		}
		switch event.Type {
		case "tool_execution_start":
			if event.ToolCallId == "" {
				return nil, nil
			}
			name := event.ToolName
			if name == "" {
				name = "tool"
			}
			return []toolOpen{{ID: event.ToolCallId, Name: name}}, nil
		case "tool_execution_end":
			if event.ToolCallId == "" {
				return nil, nil
			}
			return nil, []toolClose{{ID: event.ToolCallId}}
		default:
			return nil, nil
		}
	})
}

// piTurnCount is pi's Turn extraction rule (ADR-0165).
//
// Authoritative events: type=="turn_end" with message.role=="assistant".
// message_end mixes assistant, toolResult and user roles and must not be used.
func piTurnCount(events []streamEventRecord) TurnCount {
	count := 0
	for _, ev := range events {
		var event struct {
			Type    string `json:"type"`
			Message struct {
				Role string `json:"role"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(ev.Raw), &event); err != nil {
			continue
		}
		if event.Type == "turn_end" && event.Message.Role == "assistant" {
			count++
		}
	}
	return TurnCount{Count: count, HasTurn: true}
}

// piPeakInput is pi's peak-input extraction rule (ADR-0165).
//
// For each assistant turn_end, read message.usage and take the maximum of
// input + cacheRead + cacheWrite.
func piPeakInput(events []streamEventRecord) PeakInput {
	var peak int64
	found := false
	for _, ev := range events {
		var event struct {
			Type    string `json:"type"`
			Message struct {
				Role  string `json:"role"`
				Usage *struct {
					Input      *int64 `json:"input"`
					CacheRead  *int64 `json:"cacheRead"`
					CacheWrite *int64 `json:"cacheWrite"`
				} `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(ev.Raw), &event); err != nil {
			continue
		}
		if event.Type != "turn_end" || event.Message.Role != "assistant" || event.Message.Usage == nil {
			continue
		}
		usage := event.Message.Usage
		var sum int64
		if v := usage.Input; v != nil {
			sum += *v
		}
		if v := usage.CacheRead; v != nil {
			sum += *v
		}
		if v := usage.CacheWrite; v != nil {
			sum += *v
		}
		if !found || sum > peak {
			peak = sum
			found = true
		}
	}
	if !found {
		return PeakInput{}
	}
	return PeakInput{Tokens: peak, HasPeak: true}
}
