package tasks

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	claudeWeekdayResetAtPattern = regexp.MustCompile(`(?i)\bresets\s+(Sun|Mon|Tue|Wed|Thu|Fri|Sat)\s+([0-9]{1,2}):([0-9]{2})\s*([AP]M)\b`)
	claudeBareResetAtPattern    = regexp.MustCompile(`(?i)\bresets\s+([0-9]{1,2}):([0-9]{2})\s*([AP]M)\b`)
)

func normalizeClaudeStreamJSON(raw string) AgentResult {
	return normalizeResultStreamJSON(raw, claudeQuotaPauseReason)
}

// claudeLineRenderer renders claude stream-json events live: assistant prose
// plain, a dim "model <id>" line when the init event reports the actual model,
// and a dim "→ Tool hint" tick per tool use. Other event types render nothing;
// non-JSON lines are reported as unhandled so the writer passes them through
// raw.
func claudeLineRenderer(color bool) lineRenderer {
	dim := func(s string) string {
		if !color {
			return s
		}
		return ansiDim + s + ansiReset
	}
	printedModel := false
	return func(line []byte) (string, bool) {
		var event struct {
			Type    string `json:"type"`
			Subtype string `json:"subtype"`
			Model   string `json:"model"`
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
		if event.Type == "system" && event.Subtype == "init" {
			model := strings.TrimSpace(event.Model)
			if model == "" || printedModel {
				return "", true
			}
			printedModel = true
			return dim("model "+model) + "\n", true
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

func claudeActualModel(events []streamEventRecord) string {
	for _, ev := range events {
		var event struct {
			Type    string `json:"type"`
			Subtype string `json:"subtype"`
			Model   string `json:"model"`
		}
		if err := json.Unmarshal([]byte(ev.Raw), &event); err != nil {
			continue
		}
		if event.Type == "system" && event.Subtype == "init" {
			return strings.TrimSpace(event.Model)
		}
	}
	return ""
}

// claudeTokenUsage is claude's Usage extraction rule (ADR-0160).
//
// Authoritative events: each per-API-call `usage` block Claude emits for an
// assistant turn. In the stream-json wire format those blocks are aggregated
// onto events with a top-level `usage` object under Anthropic API field names
// (input_tokens, output_tokens, cache_read_input_tokens,
// cache_creation_input_tokens) — typically the terminal `result` event.
// Nested message.usage on streamed assistant partials is not authoritative
// (duplicated incomplete figures); other shapes (e.g. task_progress) lack the
// API fields and are ignored.
//
// Semantics: accumulate — sum every matching block. A present zero is a
// reported value (Has* true), not a Token-blind absence.
func claudeTokenUsage(events []streamEventRecord) TokenUsage {
	var u TokenUsage
	for _, ev := range events {
		var usage struct {
			Usage struct {
				InputTokens              *int64 `json:"input_tokens"`
				OutputTokens             *int64 `json:"output_tokens"`
				CacheReadInputTokens     *int64 `json:"cache_read_input_tokens"`
				CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(ev.Raw), &usage); err != nil {
			continue
		}
		if v := usage.Usage.InputTokens; v != nil {
			u.Input += *v
			u.HasInput = true
		}
		if v := usage.Usage.OutputTokens; v != nil {
			u.Output += *v
			u.HasOutput = true
		}
		if v := usage.Usage.CacheReadInputTokens; v != nil {
			u.CacheRead += *v
			u.HasCacheRead = true
		}
		if v := usage.Usage.CacheCreationInputTokens; v != nil {
			u.CacheWrite += *v
			u.HasCacheWrite = true
		}
	}
	return u
}

// claudeTurnCount is claude's Turn extraction rule (ADR-0165).
//
// Authoritative events: type=="assistant", deduped by message.id. Consecutive
// assistant events repeat identical usage for the same turn; counting without
// dedup inflates the figure.
func claudeTurnCount(events []streamEventRecord) TurnCount {
	seen := make(map[string]struct{})
	for _, ev := range events {
		var event struct {
			Type    string `json:"type"`
			Message struct {
				ID string `json:"id"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(ev.Raw), &event); err != nil {
			continue
		}
		if event.Type != "assistant" || event.Message.ID == "" {
			continue
		}
		seen[event.Message.ID] = struct{}{}
	}
	return TurnCount{Count: len(seen), HasTurn: true}
}

// claudePeakInput is claude's peak-input extraction rule (ADR-0165).
//
// For each deduped assistant turn, read message.usage and take the maximum of
// input_tokens + cache_read_input_tokens + cache_creation_input_tokens.
func claudePeakInput(events []streamEventRecord) PeakInput {
	perTurn := make(map[string]int64)
	for _, ev := range events {
		var event struct {
			Type    string `json:"type"`
			Message struct {
				ID    string `json:"id"`
				Usage *struct {
					InputTokens              *int64 `json:"input_tokens"`
					CacheReadInputTokens     *int64 `json:"cache_read_input_tokens"`
					CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(ev.Raw), &event); err != nil {
			continue
		}
		if event.Type != "assistant" || event.Message.ID == "" || event.Message.Usage == nil {
			continue
		}
		usage := event.Message.Usage
		var sum int64
		if v := usage.InputTokens; v != nil {
			sum += *v
		}
		if v := usage.CacheReadInputTokens; v != nil {
			sum += *v
		}
		if v := usage.CacheCreationInputTokens; v != nil {
			sum += *v
		}
		perTurn[event.Message.ID] = sum
	}
	if len(perTurn) == 0 {
		return PeakInput{}
	}
	var peak int64
	for _, v := range perTurn {
		if v > peak {
			peak = v
		}
	}
	return PeakInput{Tokens: peak, HasPeak: true}
}

// claudeToolTick formats a compact "→ Name hint" line, probing the tool input
// for the first recognized salient key without knowing per-tool schemas.
func claudeToolTick(name string, input json.RawMessage) string {
	return toolTick(name, claudeToolHint(input))
}

type claudeToolHintProbe struct {
	FilePath string `json:"file_path"`
	Path     string `json:"path"`
	Command  string `json:"command"`
	Pattern  string `json:"pattern"`
	URL      string `json:"url"`
	Query    string `json:"query"`
}

func claudeToolHint(input json.RawMessage) string {
	return toolHint(input, func(p claudeToolHintProbe) string {
		return firstNonEmpty(p.FilePath, p.Path, p.Command, p.Pattern, p.URL, p.Query)
	})
}

// claudeToolTimings derives per-tool durations from one stored Captured
// attempt stream: each assistant tool_use block is paired with the user
// tool_result block carrying the same tool-use id, and the gap between their
// arrival times is that invocation's duration. Ids — not order — do the
// pairing, so parallel tool calls within one assistant turn resolve correctly.
// A tool_use with no result (e.g. a killed attempt) contributes nothing to the
// per-tool rows, but its still-open interval is reported as a tool window so
// Model time never absorbs the wait on a tool that was running when the
// attempt ended. Results aggregate per tool name, longest total first.
func claudeToolTimings(events []streamEventRecord) ([]ToolTiming, []toolWindow) {
	return accumulateToolTimings(events, func(ev streamEventRecord) ([]toolOpen, []toolClose) {
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
			return nil, nil
		}
		switch msg.Type {
		case "assistant":
			var opens []toolOpen
			for _, c := range msg.Message.Content {
				if c.Type == "tool_use" && c.ID != "" {
					opens = append(opens, toolOpen{ID: c.ID, Name: c.Name})
				}
			}
			return opens, nil
		case "user":
			var closes []toolClose
			for _, c := range msg.Message.Content {
				if c.Type == "tool_result" {
					closes = append(closes, toolClose{ID: c.ToolUseID})
				}
			}
			return nil, closes
		default:
			return nil, nil
		}
	})
}

func claudeQuotaPauseReason(result string) *AgentUnavailability {
	for _, marker := range []string{
		"You've hit your session limit",
		"You've hit your weekly limit",
		"You've hit your Opus limit",
	} {
		if strings.Contains(result, marker) {
			return DetectedQuotaPause(result)
		}
	}
	return nil
}

func claudeQuotaResetAt(reason string, now time.Time) time.Time {
	if m := claudeWeekdayResetAtPattern.FindStringSubmatch(reason); m != nil {
		hour, minute, ok := parseQuotaClock(m[2], m[3], m[4])
		if !ok {
			return time.Time{}
		}
		weekday, ok := parseQuotaWeekday(m[1])
		if !ok {
			return time.Time{}
		}
		return nextQuotaWeekdayTime(now, weekday, hour, minute)
	}
	if m := claudeBareResetAtPattern.FindStringSubmatch(reason); m != nil {
		hour, minute, ok := parseQuotaClock(m[1], m[2], m[3])
		if !ok {
			return time.Time{}
		}
		return nextQuotaLocalTime(now, hour, minute)
	}
	return time.Time{}
}

func parseQuotaClock(hourText, minuteText, meridiem string) (int, int, bool) {
	hour, err := strconv.Atoi(hourText)
	if err != nil || hour < 1 || hour > 12 {
		return 0, 0, false
	}
	minute, err := strconv.Atoi(minuteText)
	if err != nil || minute < 0 || minute > 59 {
		return 0, 0, false
	}
	switch strings.ToUpper(meridiem) {
	case "AM":
		if hour == 12 {
			hour = 0
		}
	case "PM":
		if hour != 12 {
			hour += 12
		}
	default:
		return 0, 0, false
	}
	return hour, minute, true
}

func parseQuotaWeekday(text string) (time.Weekday, bool) {
	switch strings.ToLower(text) {
	case "sun":
		return time.Sunday, true
	case "mon":
		return time.Monday, true
	case "tue":
		return time.Tuesday, true
	case "wed":
		return time.Wednesday, true
	case "thu":
		return time.Thursday, true
	case "fri":
		return time.Friday, true
	case "sat":
		return time.Saturday, true
	default:
		return time.Sunday, false
	}
}

func nextQuotaLocalTime(now time.Time, hour, minute int) time.Time {
	loc := now.Location()
	localNow := now.In(loc)
	reset := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), hour, minute, 0, 0, loc)
	if !reset.After(localNow) {
		reset = reset.Add(24 * time.Hour)
	}
	if reset.Sub(localNow) > 24*time.Hour {
		return time.Time{}
	}
	return reset
}

func nextQuotaWeekdayTime(now time.Time, weekday time.Weekday, hour, minute int) time.Time {
	loc := now.Location()
	localNow := now.In(loc)
	reset := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), hour, minute, 0, 0, loc)
	days := (int(weekday) - int(localNow.Weekday()) + 7) % 7
	reset = reset.AddDate(0, 0, days)
	if !reset.After(localNow) {
		reset = reset.AddDate(0, 0, 7)
	}
	return reset
}
