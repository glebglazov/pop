package tasks

import (
	"bufio"
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const cursorAuthenticationRequiredPrefix = "Error: Authentication required."

// cursorSpentAllowancePrefix opens the diagnostic cursor-agent prints when the
// login's allowance for the requested model is spent; the usage-limit phrase
// distinguishes it from every other ActionRequiredError.
const (
	cursorSpentAllowancePrefix = "ActionRequiredError:"
	cursorSpentAllowancePhrase = "usage limit"
)

// cursorResetDatePattern matches the bare calendar date cursor's spent-allowance
// diagnostic names as the reset ("… ends on 9/4/2026."). It carries no time of
// day and no timezone, so it can only be read as a local date.
var cursorResetDatePattern = regexp.MustCompile(`\b(\d{1,2})/(\d{1,2})/(\d{4})\b`)

func normalizeCursorStreamJSON(raw string) AgentResult {
	if v := cursorAuthFailureReason(raw); v != nil {
		return AgentResult{ProceedVerdict: v}
	}
	if v := cursorSpentAllowanceReason(raw, time.Now()); v != nil {
		return AgentResult{ProceedVerdict: v}
	}
	return normalizeResultStreamJSON(raw, nil)
}

// cursorSpentAllowanceReason scans the raw capture for cursor-agent's
// spent-allowance refusal (ADR-0168). Confirmed shape, captured 2026-08-09: one
// bare non-JSON line after system/init and the prompt echo, exit 1, the model
// never engaged — the same raw-line shape as the logged-out diagnostic.
//
// The refusal condemns the model, not the login: the same cursor-agent on the
// same account runs the tier's next entry, so the verdict is model-scoped and
// the Effort ladder walks down rather than surrendering the preset. It heals
// with time, carrying the message's own reset when it named one; the caller caps
// how long the resulting skip actually holds.
func cursorSpentAllowanceReason(raw string, now time.Time) *AgentProceedVerdict {
	scanner := bufio.NewScanner(strings.NewReader(raw))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, cursorSpentAllowancePrefix) || !strings.Contains(line, cursorSpentAllowancePhrase) {
			continue
		}
		// The model is left for the caller to stamp: the message names the family
		// ("Opus"), and what must be skipped is the ladder entry that was pinned.
		v := NewModelRefusedVerdict("", "", line, ProceedRecoveryTime).WithResetAt(cursorQuotaResetAt(line, now))
		return &v
	}
	return nil
}

// cursorQuotaResetAt reads the reset instant out of a cursor diagnostic. The
// message states a bare M/D/YYYY billing-cycle date, so the instant is that
// local date's start; a date already behind now, or no date at all, is no
// instant.
func cursorQuotaResetAt(reason string, now time.Time) time.Time {
	m := cursorResetDatePattern.FindStringSubmatch(reason)
	if m == nil {
		return time.Time{}
	}
	month, err1 := strconv.Atoi(m[1])
	day, err2 := strconv.Atoi(m[2])
	year, err3 := strconv.Atoi(m[3])
	if err1 != nil || err2 != nil || err3 != nil || month < 1 || month > 12 || day < 1 || day > 31 {
		return time.Time{}
	}
	reset := time.Date(year, time.Month(month), day, 0, 0, 0, 0, now.Location())
	if !reset.After(now) {
		return time.Time{}
	}
	return reset
}

// cursorAuthFailureReason scans the raw capture for the logged-out cursor-agent
// diagnostic (ADR-0153). Confirmed shape: one plain line on stderr, empty stdout.
func cursorAuthFailureReason(raw string) *AgentProceedVerdict {
	scanner := bufio.NewScanner(strings.NewReader(raw))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, cursorAuthenticationRequiredPrefix) {
			return DetectedAuthFailure(line)
		}
	}
	return nil
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
	return toolTick(toolName, cursorToolHint(args))
}

type cursorToolHintProbe struct {
	Command     string `json:"command"`
	Pattern     string `json:"pattern"`
	GlobPattern string `json:"globPattern"`
	Query       string `json:"query"`
	Path        string `json:"path"`
	URL         string `json:"url"`
}

func cursorToolHint(args json.RawMessage) string {
	return toolHint(args, func(p cursorToolHintProbe) string {
		return firstNonEmpty(p.Command, p.Pattern, p.GlobPattern, p.Query, p.Path, p.URL)
	})
}

// cursorTokenUsage is cursor's Usage extraction rule (ADR-0160).
//
// Authoritative event: the terminal `result` event's top-level `usage`
// object, under camelCase keys (inputTokens, outputTokens, cacheReadTokens,
// cacheWriteTokens). That block is the whole-run total — cursor emits it
// exactly once.
//
// Semantics: replace — read that one event; sum nothing. A present zero is
// a reported value (Has* true), not a Token-blind absence.
func cursorTokenUsage(events []streamEventRecord) TokenUsage {
	var u TokenUsage
	found := false
	for _, ev := range events {
		var event struct {
			Type  string `json:"type"`
			Usage *struct {
				InputTokens      *int64 `json:"inputTokens"`
				OutputTokens     *int64 `json:"outputTokens"`
				CacheReadTokens  *int64 `json:"cacheReadTokens"`
				CacheWriteTokens *int64 `json:"cacheWriteTokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(ev.Raw), &event); err != nil {
			continue
		}
		if event.Type != "result" || event.Usage == nil {
			continue
		}
		// Replace: the last matching result wins (cursor emits one; fixtures
		// that emit more still must not accumulate).
		var next TokenUsage
		if v := event.Usage.InputTokens; v != nil {
			next.Input = *v
			next.HasInput = true
		}
		if v := event.Usage.OutputTokens; v != nil {
			next.Output = *v
			next.HasOutput = true
		}
		if v := event.Usage.CacheReadTokens; v != nil {
			next.CacheRead = *v
			next.HasCacheRead = true
		}
		if v := event.Usage.CacheWriteTokens; v != nil {
			next.CacheWrite = *v
			next.HasCacheWrite = true
		}
		u = next
		found = true
	}
	if !found {
		return TokenUsage{}
	}
	return u
}

// cursorActualModel is cursor's actual-model extraction rule (ADR-0165).
//
// Authoritative event: system/init's top-level model field — the model cursor
// actually ran, which may differ from the requested preset model.
func cursorActualModel(events []streamEventRecord) string {
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

// renderCursorEvent parses one cursor stream-json event into readable entries.
func renderCursorEvent(ev streamEventRecord) []StreamEvent {
	var out []StreamEvent

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
	if err := json.Unmarshal([]byte(ev.Raw), &event); err != nil {
		return []StreamEvent{{
			AtMS: ev.AtMS,
			Type: "raw",
			Text: ev.Raw,
		}}
	}

	switch event.Type {
	case "assistant":
		var text strings.Builder
		for _, c := range event.Message.Content {
			if c.Type == "text" {
				text.WriteString(c.Text)
			}
		}
		if s := strings.TrimRight(text.String(), "\n"); s != "" {
			out = append(out, StreamEvent{
				AtMS: ev.AtMS,
				Type: "assistant",
				Text: s,
			})
		}
	case "tool_call":
		if event.Subtype != "started" {
			break
		}
		toolName, args := cursorToolCall(event.ToolCall)
		if toolName == "" {
			break
		}
		argsStr := ""
		if len(args) > 0 {
			argsStr = string(args)
		}
		out = append(out, StreamEvent{
			AtMS:     ev.AtMS,
			Type:     "tool_use",
			ToolName: toolName,
			ToolArgs: argsStr,
		})
	}

	return out
}

// cursorToolTimings derives per-tool durations from cursor stream-json events:
// each tool_call started is paired with the completed event sharing call_id
// (or toolCallId), and the gap between their arrival times is that invocation's
// duration. Tool names come from the started event's tool_call payload.
func cursorToolTimings(events []streamEventRecord) ([]ToolTiming, []toolWindow) {
	return accumulateToolTimings(events, func(ev streamEventRecord) ([]toolOpen, []toolClose) {
		var event struct {
			Type     string          `json:"type"`
			Subtype  string          `json:"subtype"`
			CallID   string          `json:"call_id"`
			ToolCall json.RawMessage `json:"tool_call"`
		}
		if err := json.Unmarshal([]byte(ev.Raw), &event); err != nil {
			return nil, nil
		}
		if event.Type != "tool_call" {
			return nil, nil
		}
		id := event.CallID
		if id == "" && len(event.ToolCall) > 0 {
			var meta struct {
				ToolCallId string `json:"toolCallId"`
			}
			if err := json.Unmarshal(event.ToolCall, &meta); err == nil {
				id = meta.ToolCallId
			}
		}
		if id == "" {
			return nil, nil
		}
		switch event.Subtype {
		case "started":
			name, _ := cursorToolCall(event.ToolCall)
			if name == "" {
				name = "tool_call"
			}
			return []toolOpen{{ID: id, Name: name}}, nil
		case "completed":
			return nil, []toolClose{{ID: id}}
		default:
			return nil, nil
		}
	})
}

// cursorTurnCount is cursor's Turn extraction rule (ADR-0165).
//
// Authoritative boundary: distinct model_call_id values on assistant and
// tool_call events. One model call may fan out into many tool_call events.
func cursorTurnCount(events []streamEventRecord) TurnCount {
	seen := make(map[string]struct{})
	for _, ev := range events {
		var event struct {
			Type        string `json:"type"`
			ModelCallID string `json:"model_call_id"`
		}
		if err := json.Unmarshal([]byte(ev.Raw), &event); err != nil {
			continue
		}
		if event.Type != "assistant" && event.Type != "tool_call" {
			continue
		}
		if event.ModelCallID == "" {
			continue
		}
		seen[event.ModelCallID] = struct{}{}
	}
	return TurnCount{Count: len(seen), HasTurn: true}
}
