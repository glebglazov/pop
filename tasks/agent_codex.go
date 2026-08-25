package tasks

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"
)

var codexResetAtPattern = regexp.MustCompile(`(?i)\btry again at\s+([0-9]{1,2}):([0-9]{2})\s*([AP]M)\b`)

// codexToolItemTypes is the set of Thread Event item types that count as a tool
// invocation — the same set codexLineRenderer ticks live. Sharing one set keeps
// the timing lens and the live render in agreement on what a tool is, so
// reasoning, todo_list, and agent_message items can never leak into per-tool
// rows even if they grow a started event we have not observed (ADR 0016).
var codexToolItemTypes = map[string]bool{
	"command_execution": true,
	"mcp_tool_call":     true,
	"file_change":       true,
	"web_search":        true,
}

func normalizeCodexJSONL(raw string) AgentResult {
	var transcript string
	var diagnostics []string
	var pause *AgentProceedVerdict
	scanAgentJSONLines(raw, nil, func(line []byte) bool {
		var event struct {
			Type    string          `json:"type"`
			Message string          `json:"message"`
			Error   json.RawMessage `json:"error"`
			Item    struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"item"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			return false
		}
		switch event.Type {
		case "item.completed":
			if event.Item.Type == "agent_message" && event.Item.Text != "" {
				transcript = event.Item.Text
			}
		case "error", "turn.failed":
			detail := agentJSONDiagnostic(event.Error)
			if detail == "" {
				detail = event.Message
			}
			if pause == nil {
				pause = codexProceedStop(raw, detail)
			}
			appendAgentDiagnostic(&diagnostics, detail)
		}
		return true
	})
	if pause != nil {
		return AgentResult{ProceedVerdict: pause}
	}
	return normalizedTranscript(transcript, diagnostics)
}

// codexSpendCapRateLimitType is the rate_limit_reached_type value codex's
// token_count event states, on its rate_limits block, when the account has hit
// its workspace spend cap — observed 2026-08-21 on the token_count event
// preceding the refusal.
const codexSpendCapRateLimitType = "workspace_member_usage_limit_reached"

// codexRefusalSignature is how codex recognises its spend-cap refusal: the
// typed rate-limit-type field first, its "spend cap" substring beneath it for
// a capture that changed its event schema rather than its wording (ADR-0234).
var codexRefusalSignature = AgentRefusalSignatureCapability{
	Kind:       CapabilitySupported,
	Structured: codexStructuredSpendCap,
}

// codexStructuredSpendCap reads codex's typed rate-limit-type field: the
// token_count event ahead of the refusal carries rate_limit_reached_type on
// its rate_limits block, unread by the substring match the refusal itself used
// to be classified by alone (ADR-0234). A spend cap names no allowance window,
// so the class is always Unknown; the bool is the whole reading.
func codexStructuredSpendCap(raw string) (AgentQuotaWindowClass, bool) {
	found := false
	scanAgentJSONLines(raw, nil, func(line []byte) bool {
		var event struct {
			Type       string `json:"type"`
			RateLimits struct {
				RateLimitReachedType string `json:"rate_limit_reached_type"`
			} `json:"rate_limits"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			return false
		}
		if event.Type == "token_count" && event.RateLimits.RateLimitReachedType == codexSpendCapRateLimitType {
			found = true
		}
		return true
	})
	return QuotaWindowUnknown, found
}

// codexProceedStop reads one codex error or turn.failed diagnostic, and the
// whole capture beside it, for the stops that are the account's rather than
// the task's. The two are told apart by what they promise: a usage limit
// refills at a stated time, a spend cap waits on a person, so they cool on
// different clocks and are separate verdict flavours (ADR-0231).
func codexProceedStop(raw, message string) *AgentProceedVerdict {
	if v := codexSpendCapReason(raw, message); v != nil {
		return v
	}
	return codexQuotaPauseReason(message)
}

// codexSpendCapReason detects codex's spend-cap refusal from the most
// structured channel this capture carries: the typed rate-limit-type field a
// preceding token_count event states, with the "spend cap" substring retained
// as the reading for a capture whose event schema changed instead of its
// wording (ADR-0234). Either way the provider's own sentence stays the reason
// a human reads.
func codexSpendCapReason(raw, message string) *AgentProceedVerdict {
	if _, ok := codexRefusalSignature.structuredRefusal(raw); ok {
		return DetectedSpendCap(strings.TrimSpace(message))
	}
	return spendCapReason(message)
}

// codexQuotaPauseReason detects codex's usage-limit message in an error or
// turn.failed diagnostic, the codex analog of claudeQuotaPauseReason. Confirmed
// against a live limit-hit: codex exec --json aborts the turn (exit 1) and emits
// both {"type":"error","message":...} and {"type":"turn.failed","error":
// {"message":...}}, each carrying "You've hit your usage limit. ... try again at
// <time>." The reset time and upsell URLs vary, so the stable anchor is the
// leading sentence; the full message is kept as the pause reason so the reset
// time reaches the user.
func codexQuotaPauseReason(message string) *AgentProceedVerdict {
	if strings.Contains(message, "You've hit your usage limit") {
		return DetectedQuotaPause(message)
	}
	return nil
}

func codexQuotaResetAt(reason string, now time.Time) time.Time {
	m := codexResetAtPattern.FindStringSubmatch(reason)
	if m == nil {
		return time.Time{}
	}
	hour, minute, ok := parseQuotaClock(m[1], m[2], m[3])
	if !ok {
		return time.Time{}
	}
	return withQuotaAssuranceOffset(nextQuotaLocalTime(now, hour, minute))
}

func agentQuotaResetAt(preset, reason string, now time.Time) time.Time {
	adapter, err := ResolveAgentAdapter(preset)
	if err != nil {
		return time.Time{}
	}
	return adapter.QuotaResetCapability().resetAt(reason, now)
}

// codexLineRenderer renders codex-jsonl Thread Events live: assistant prose is
// emitted whole on item.completed for an agent_message item, and a dim
// "→ kind hint" tick is emitted on item.started for each tool/command item.
// Reasoning, todo_list, lifecycle events, and errors render nothing (the
// normalizer surfaces errors); non-JSON lines are reported as unhandled so the
// writer passes them through raw.
//
// Prose is emitted only on item.completed (the cumulative final text), never on
// item.updated, so the renderer is correct regardless of whether item.updated.text
// is a cumulative snapshot or a delta — one of the open items that could not be
// confirmed against a live authenticated run (no codex auth / installed v0.7.0
// predates --json). mcp_tool_call.arguments is probed as both an object and a
// JSON string, so it is also robust to that open item.
func codexLineRenderer(color bool) lineRenderer {
	dim := func(s string) string {
		if !color {
			return s
		}
		return ansiDim + s + ansiReset
	}
	return func(line []byte) (string, bool) {
		var event struct {
			Type string `json:"type"`
			Item struct {
				Type      string          `json:"type"`
				Text      string          `json:"text"`
				Command   string          `json:"command"`
				Tool      string          `json:"tool"`
				Server    string          `json:"server"`
				Arguments json.RawMessage `json:"arguments"`
				Query     string          `json:"query"`
				Changes   []struct {
					Path string `json:"path"`
					Kind string `json:"kind"`
				} `json:"changes"`
			} `json:"item"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			return "", false
		}
		switch event.Type {
		case "item.completed":
			if event.Item.Type == "agent_message" {
				if text := strings.TrimRight(event.Item.Text, "\n"); text != "" {
					return text + "\n", true
				}
			}
			return "", true
		case "item.started":
			if codexToolItemTypes[event.Item.Type] {
				var changePath string
				if len(event.Item.Changes) > 0 {
					changePath = event.Item.Changes[0].Path
				}
				hint := codexItemHint(
					event.Item.Command,
					event.Item.Tool,
					event.Item.Server,
					codexArgumentsHint(event.Item.Arguments),
					changePath,
					event.Item.Query,
				)
				return dim(codexItemTick(event.Item.Type, hint)) + "\n", true
			}
			return "", true
		default:
			return "", true
		}
	}
}

// codexItemTick formats a compact "→ kind hint" line, where kind is the item
// type discriminator and hint is the first salient field found.
func codexItemTick(kind, hint string) string {
	return toolTick(kind, hint)
}

// renderCodexEvent parses one codex-jsonl Thread Event into readable stream
// entries, mirroring codexLineRenderer: agent_message prose on
// item.completed and a tool_use tick on item.started for tool item types.
func renderCodexEvent(ev streamEventRecord) []StreamEvent {
	var event struct {
		Type string `json:"type"`
		Item struct {
			Type      string          `json:"type"`
			Text      string          `json:"text"`
			Command   string          `json:"command"`
			Tool      string          `json:"tool"`
			Server    string          `json:"server"`
			Arguments json.RawMessage `json:"arguments"`
			Query     string          `json:"query"`
			Changes   []struct {
				Path string `json:"path"`
				Kind string `json:"kind"`
			} `json:"changes"`
		} `json:"item"`
	}
	if err := json.Unmarshal([]byte(ev.Raw), &event); err != nil {
		return []StreamEvent{{
			AtMS: ev.AtMS,
			Type: "raw",
			Text: ev.Raw,
		}}
	}

	switch event.Type {
	case "item.completed":
		if event.Item.Type == "agent_message" {
			if text := strings.TrimRight(event.Item.Text, "\n"); text != "" {
				return []StreamEvent{{
					AtMS: ev.AtMS,
					Type: "assistant",
					Text: text,
				}}
			}
		}
	case "item.started":
		if codexToolItemTypes[event.Item.Type] {
			var changePath string
			if len(event.Item.Changes) > 0 {
				changePath = event.Item.Changes[0].Path
			}
			hint := codexItemHint(
				event.Item.Command,
				event.Item.Tool,
				event.Item.Server,
				codexArgumentsHint(event.Item.Arguments),
				changePath,
				event.Item.Query,
			)
			return []StreamEvent{{
				AtMS:     ev.AtMS,
				Type:     "tool_use",
				ToolName: event.Item.Type,
				ToolArgs: hint,
			}}
		}
	}
	return nil
}

// codexItemHint returns the first non-empty probe value, collapsed to a single
// line and truncated to ~80 chars, matching claudeToolHint.
func codexItemHint(values ...string) string {
	return collapseHint(firstNonEmpty(values...))
}

// codexArgumentsHint extracts a hint from mcp_tool_call.arguments, whose JSON
// type (object vs string) is unconfirmed against a live run. A JSON string is
// used verbatim; an object yields no hint (the tool/server name already names
// the call).
func codexArgumentsHint(args json.RawMessage) string {
	if len(args) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(args, &s); err == nil {
		return s
	}
	return ""
}

// codexTokenUsage is codex's Usage extraction rule (ADR-0160).
//
// Authoritative event: turn.completed's top-level usage object under snake_case
// keys (input_tokens, cached_input_tokens, output_tokens). That block is the
// whole-run total — codex emits it once per turn.
//
// Semantics: replace — read the last matching turn.completed; sum nothing. A
// present zero is a reported value (Has* true), not a Token-blind absence.
func codexTokenUsage(events []streamEventRecord) TokenUsage {
	var u TokenUsage
	found := false
	for _, ev := range events {
		var event struct {
			Type  string `json:"type"`
			Usage *struct {
				InputTokens       *int64 `json:"input_tokens"`
				CachedInputTokens *int64 `json:"cached_input_tokens"`
				OutputTokens      *int64 `json:"output_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(ev.Raw), &event); err != nil {
			continue
		}
		if event.Type != "turn.completed" || event.Usage == nil {
			continue
		}
		var next TokenUsage
		if v := event.Usage.InputTokens; v != nil {
			next.Input = *v
			next.HasInput = true
		}
		if v := event.Usage.OutputTokens; v != nil {
			next.Output = *v
			next.HasOutput = true
		}
		if v := event.Usage.CachedInputTokens; v != nil {
			next.CacheRead = *v
			next.HasCacheRead = true
		}
		u = next
		found = true
	}
	if !found {
		return TokenUsage{}
	}
	return u
}

// codexTurnCount is codex's Turn extraction rule (ADR-0165, ADR-0219).
//
// Authoritative events: the token_count events the capture-time Rollout splice
// merged in — one per model call, which is what a Turn means for claude and pi
// too. codex's own exec stream carries one turn.completed per headless run, so
// counting those answered 1 for every run no matter how long it ground; a run
// stored without its rollout reads turn-blind instead, which is the honest
// answer.
func codexTurnCount(events []streamEventRecord) TurnCount {
	count := 0
	for _, ev := range events {
		var event struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(ev.Raw), &event); err != nil {
			continue
		}
		if event.Type == codexTokenCountEvent {
			count++
		}
	}
	if count == 0 {
		return TurnCount{}
	}
	return TurnCount{Count: count, HasTurn: true}
}

// codexPeakInput is codex's Peak input extraction rule (ADR-0165, ADR-0219).
//
// Authoritative events: the spliced token_count events, each carrying that
// model call's own context under info.last_token_usage. The peak is the largest
// per-call context any call carried.
//
// One wire fact separates codex from claude and pi (ADR-0219): codex's
// input_tokens already includes the cached prefix, so cached_input_tokens is a
// breakdown of input_tokens, not a second addend — adding it would double-count
// the whole cached context. Only cache_write_input_tokens, the freshly written
// prefix, sits outside input_tokens.
func codexPeakInput(events []streamEventRecord) PeakInput {
	var peak int64
	found := false
	for _, ev := range events {
		var event struct {
			Type string `json:"type"`
			Info *struct {
				LastTokenUsage *struct {
					InputTokens           int64 `json:"input_tokens"`
					CacheWriteInputTokens int64 `json:"cache_write_input_tokens"`
				} `json:"last_token_usage"`
			} `json:"info"`
		}
		if err := json.Unmarshal([]byte(ev.Raw), &event); err != nil {
			continue
		}
		if event.Type != codexTokenCountEvent || event.Info == nil || event.Info.LastTokenUsage == nil {
			continue
		}
		usage := event.Info.LastTokenUsage
		if total := usage.InputTokens + usage.CacheWriteInputTokens; total > peak {
			peak = total
		}
		found = true
	}
	if !found {
		return PeakInput{}
	}
	return PeakInput{Tokens: peak, HasPeak: true}
}

// codexToolTimings derives per-tool durations from one stored Captured attempt
// stream: each tool item's item.started is paired with the item.completed
// carrying the same item id, and the gap between their arrival times is that
// invocation's duration. Ids — not order — do the pairing, and only the four
// tool item types (codexToolItemTypes) participate, so reasoning, todo_list,
// and agent_message prose contribute nothing to tool rows and fall into Model
// time. A tool item still open when the attempt ended (a killed run) adds no
// per-tool row but reports its open interval as a tool window, so Model time
// never absorbs the wait on a tool that was running at the end. Results
// aggregate per tool name, longest total first.
func codexToolTimings(events []streamEventRecord) ([]ToolTiming, []toolWindow) {
	return accumulateToolTimings(events, func(ev streamEventRecord) ([]toolOpen, []toolClose) {
		var msg struct {
			Type string `json:"type"`
			Item struct {
				ID     string `json:"id"`
				Type   string `json:"type"`
				Server string `json:"server"`
				Tool   string `json:"tool"`
			} `json:"item"`
		}
		if err := json.Unmarshal([]byte(ev.Raw), &msg); err != nil {
			return nil, nil
		}
		if msg.Item.ID == "" || !codexToolItemTypes[msg.Item.Type] {
			return nil, nil
		}
		switch msg.Type {
		case "item.started":
			name := codexToolName(msg.Item.Type, msg.Item.Server, msg.Item.Tool)
			return []toolOpen{{ID: msg.Item.ID, Name: name}}, nil
		case "item.completed":
			// The mcp server/tool fields may arrive on the completed event rather
			// than the started one; prefer a name the completed event names more
			// richly than the bare item type.
			completed := codexToolName(msg.Item.Type, msg.Item.Server, msg.Item.Tool)
			return nil, []toolClose{{
				ID: msg.Item.ID,
				Rename: func(pendingName string) string {
					if pendingName == msg.Item.Type && completed != msg.Item.Type {
						return completed
					}
					return pendingName
				},
			}}
		default:
			return nil, nil
		}
	})
}

// codexToolName names a codex tool row. command_execution, file_change, and
// web_search report coarsely under their item type — codex carries no finer
// per-call name pop is willing to invent for them. mcp_tool_call splits by
// server and tool so distinct MCP calls are distinguished, degrading to
// mcp:<tool> and then to the bare item type as those fields go missing; the
// mcp_tool_call field shapes are unconfirmed against a live run, so the
// fallback stays honest rather than fabricating a name.
func codexToolName(itemType, server, tool string) string {
	if itemType != "mcp_tool_call" {
		return itemType
	}
	server = strings.TrimSpace(server)
	tool = strings.TrimSpace(tool)
	switch {
	case server != "" && tool != "":
		return "mcp:" + server + "/" + tool
	case tool != "":
		return "mcp:" + tool
	default:
		return "mcp_tool_call"
	}
}
