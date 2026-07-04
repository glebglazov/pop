package tasks

import (
	"encoding/json"
	"regexp"
	"sort"
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
	var pause *AgentQuotaPause
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
				pause = codexQuotaPauseReason(detail)
			}
			appendAgentDiagnostic(&diagnostics, detail)
		}
		return true
	})
	if pause != nil {
		return AgentResult{QuotaPause: pause}
	}
	return normalizedTranscript(transcript, diagnostics)
}

// codexQuotaPauseReason detects codex's usage-limit message in an error or
// turn.failed diagnostic, the codex analog of claudeQuotaPauseReason. Confirmed
// against a live limit-hit: codex exec --json aborts the turn (exit 1) and emits
// both {"type":"error","message":...} and {"type":"turn.failed","error":
// {"message":...}}, each carrying "You've hit your usage limit. ... try again at
// <time>." The reset time and upsell URLs vary, so the stable anchor is the
// leading sentence; the full message is kept as the pause reason so the reset
// time reaches the user.
func codexQuotaPauseReason(message string) *AgentQuotaPause {
	if strings.Contains(message, "You've hit your usage limit") {
		return &AgentQuotaPause{Reason: message}
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
	return nextQuotaLocalTime(now, hour, minute)
}

func agentQuotaResetAt(preset, reason string, now time.Time) time.Time {
	switch preset {
	case "codex":
		return codexQuotaResetAt(reason, now)
	case "claude":
		return claudeQuotaResetAt(reason, now)
	case "pi":
		return piQuotaResetAt(reason, now)
	default:
		return time.Time{}
	}
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
	tick := "→ " + kind
	if hint != "" {
		tick += " " + hint
	}
	return tick
}

// codexItemHint returns the first non-empty probe value, collapsed to a single
// line and truncated to ~80 chars, matching claudeToolHint.
func codexItemHint(values ...string) string {
	hint := firstNonEmpty(values...)
	hint = strings.TrimSpace(strings.ReplaceAll(hint, "\n", " "))
	if len(hint) > 80 {
		hint = hint[:77] + "..."
	}
	return hint
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
	type pendingUse struct {
		name string
		atMS int64
	}
	pending := map[string]pendingUse{}
	totals := map[string]*ToolTiming{}
	var windows []toolWindow
	for _, ev := range events {
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
			continue
		}
		if msg.Item.ID == "" || !codexToolItemTypes[msg.Item.Type] {
			continue
		}
		switch msg.Type {
		case "item.started":
			pending[msg.Item.ID] = pendingUse{
				name: codexToolName(msg.Item.Type, msg.Item.Server, msg.Item.Tool),
				atMS: ev.AtMS,
			}
		case "item.completed":
			use, ok := pending[msg.Item.ID]
			if !ok {
				continue
			}
			delete(pending, msg.Item.ID)
			// The mcp server/tool fields may arrive on the completed event rather
			// than the started one; prefer a name the completed event names more
			// richly than the bare item type.
			if completed := codexToolName(msg.Item.Type, msg.Item.Server, msg.Item.Tool); use.name == msg.Item.Type && completed != msg.Item.Type {
				use.name = completed
			}
			agg := totals[use.name]
			if agg == nil {
				agg = &ToolTiming{Name: use.name}
				totals[use.name] = agg
			}
			agg.Count++
			agg.Total += time.Duration(ev.AtMS-use.atMS) * time.Millisecond
			windows = append(windows, toolWindow{StartMS: use.atMS, EndMS: ev.AtMS})
		}
	}
	for _, use := range pending {
		windows = append(windows, toolWindow{StartMS: use.atMS, EndMS: openWindowEndMS})
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
	return out, windows
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
