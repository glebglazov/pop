package tasks

import (
	"bufio"
	"encoding/json"
	"sort"
	"strings"
)

const cursorAuthenticationRequiredPrefix = "Error: Authentication required."

func normalizeCursorStreamJSON(raw string) AgentResult {
	if u := cursorAuthFailureReason(raw); u != nil {
		return AgentResult{Unavailability: u}
	}
	return normalizeResultStreamJSON(raw, nil)
}

// cursorAuthFailureReason scans the raw capture for the logged-out cursor-agent
// diagnostic (ADR-0153). Confirmed shape: one plain line on stderr, empty stdout.
func cursorAuthFailureReason(raw string) *AgentUnavailability {
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

// cursorTerminalTokenUsage reads the whole-run total from the terminal result
// event for the over-count guard. Independent of the Usage extraction rule so
// a mistaken sum across events is caught.
func cursorTerminalTokenUsage(events []streamEventRecord) (TokenUsage, bool) {
	u := cursorTokenUsage(events)
	return u, u.HasUsage()
}
