package tasks

import (
	"encoding/json"
	"strings"
)

// streamShapeBytesWitness reports whether raw fixture events carry stream-derived
// data for one capability, independent of the adapter's declared stance or
// Extract rule. The fixture gate fires on witness-present + Blind.
func streamShapeBytesWitness(cap streamShapeCapability, events []streamEventRecord) bool {
	switch cap {
	case streamShapeUsage:
		return witnessUsageBytes(events)
	case streamShapeCost:
		return witnessCostBytes(events)
	case streamShapeToolTimings:
		return witnessToolTimingsBytes(events)
	case streamShapeActualModel:
		return witnessActualModelBytes(events)
	case streamShapeStreamRender:
		return witnessStreamRenderBytes(events)
	case streamShapeTurn:
		return witnessTurnBytes(events)
	default:
		return false
	}
}

func witnessUsageBytes(events []streamEventRecord) bool {
	for _, ev := range events {
		var probe struct {
			Type    string `json:"type"`
			Usage   json.RawMessage `json:"usage"`
			Message *struct {
				Usage json.RawMessage `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(ev.Raw), &probe); err != nil {
			continue
		}
		switch probe.Type {
		case "result", "turn.completed":
			if len(probe.Usage) > 0 && jsonHasUsageKeys(probe.Usage) {
				return true
			}
		case "message_end":
			if probe.Message != nil && len(probe.Message.Usage) > 0 && jsonHasUsageKeys(probe.Message.Usage) {
				return true
			}
		}
	}
	return false
}

func jsonHasUsageKeys(raw json.RawMessage) bool {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return false
	}
	for _, key := range []string{
		"input_tokens", "output_tokens", "cache_read_input_tokens", "cache_creation_input_tokens",
		"inputTokens", "outputTokens", "cacheReadTokens", "cacheWriteTokens",
		"cached_input_tokens", "input", "output", "cacheRead", "cacheWrite",
	} {
		if _, ok := m[key]; ok {
			return true
		}
	}
	return false
}

func witnessCostBytes(events []streamEventRecord) bool {
	for _, ev := range events {
		var probe struct {
			Type          string `json:"type"`
			TotalCostUSD  *float64 `json:"total_cost_usd"`
			ModelUsage    json.RawMessage `json:"modelUsage"`
			Message       *struct {
				Cost  *struct{ Total *float64 `json:"total"` } `json:"cost"`
				Usage *struct {
					Cost *struct{ Total *float64 `json:"total"` } `json:"cost"`
				} `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(ev.Raw), &probe); err != nil {
			continue
		}
		if probe.TotalCostUSD != nil {
			return true
		}
		if len(probe.ModelUsage) > 0 && strings.Contains(string(probe.ModelUsage), "costUSD") {
			return true
		}
		if probe.Message != nil {
			if probe.Message.Cost != nil && probe.Message.Cost.Total != nil {
				return true
			}
			if probe.Message.Usage != nil && probe.Message.Usage.Cost != nil && probe.Message.Usage.Cost.Total != nil {
				return true
			}
		}
	}
	return false
}

func witnessToolTimingsBytes(events []streamEventRecord) bool {
	type pairKey struct{ kind, id string }
	opens := make(map[pairKey]bool)
	for _, ev := range events {
		var probe struct {
			Type    string `json:"type"`
			Subtype string `json:"subtype"`
			CallID  string `json:"call_id"`
			Item    *struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			} `json:"item"`
			ToolCallId string          `json:"toolCallId"`
			ToolName   string          `json:"toolName"`
			Message    *struct {
				Content []struct {
					Type      string `json:"type"`
					ID        string `json:"id"`
					ToolUseID string `json:"tool_use_id"`
				} `json:"content"`
			} `json:"message"`
			ToolCall json.RawMessage `json:"tool_call"`
		}
		if err := json.Unmarshal([]byte(ev.Raw), &probe); err != nil {
			continue
		}
		switch probe.Type {
		case "assistant":
			if probe.Message != nil {
				for _, c := range probe.Message.Content {
					if c.Type == "tool_use" && c.ID != "" {
						opens[pairKey{"claude", c.ID}] = true
					}
				}
			}
		case "user":
			if probe.Message != nil {
				for _, c := range probe.Message.Content {
					if c.Type == "tool_result" && c.ToolUseID != "" {
						if opens[pairKey{"claude", c.ToolUseID}] {
							return true
						}
					}
				}
			}
		case "tool_call":
			id := probe.CallID
			if id == "" && len(probe.ToolCall) > 0 {
				var meta struct{ ToolCallId string `json:"toolCallId"` }
				json.Unmarshal(probe.ToolCall, &meta)
				id = meta.ToolCallId
			}
			if id == "" {
				continue
			}
			switch probe.Subtype {
			case "started":
				opens[pairKey{"cursor", id}] = true
			case "completed":
				if opens[pairKey{"cursor", id}] {
					return true
				}
			}
		case "item.started", "item.completed":
			if probe.Item != nil && probe.Item.ID != "" && codexToolItemTypes[probe.Item.Type] {
				key := pairKey{"codex", probe.Item.ID}
				if probe.Type == "item.started" {
					opens[key] = true
				} else if opens[key] {
					return true
				}
			}
		case "tool_execution_start":
			if probe.ToolCallId != "" {
				opens[pairKey{"pi", probe.ToolCallId}] = true
			}
		case "tool_execution_end":
			if probe.ToolCallId != "" && opens[pairKey{"pi", probe.ToolCallId}] {
				return true
			}
		}
	}
	return false
}

func witnessActualModelBytes(events []streamEventRecord) bool {
	for _, ev := range events {
		var probe struct {
			Type    string `json:"type"`
			Subtype string `json:"subtype"`
			Model   string `json:"model"`
		}
		if err := json.Unmarshal([]byte(ev.Raw), &probe); err != nil {
			continue
		}
		if probe.Type == "system" && probe.Subtype == "init" && strings.TrimSpace(probe.Model) != "" {
			return true
		}
	}
	return false
}

func witnessStreamRenderBytes(events []streamEventRecord) bool {
	for _, ev := range events {
		var probe struct {
			Type    string `json:"type"`
			Message *struct {
				Content []struct {
					Type string `json:"type"`
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(ev.Raw), &probe); err != nil {
			continue
		}
		if probe.Type == "assistant" && probe.Message != nil {
			for _, c := range probe.Message.Content {
				if c.Type == "tool_use" {
					return true
				}
			}
		}
		if probe.Type == "user" && probe.Message != nil {
			for _, c := range probe.Message.Content {
				if c.Type == "tool_result" {
					return true
				}
			}
		}
	}
	return false
}

func witnessTurnBytes(events []streamEventRecord) bool {
	for _, ev := range events {
		var probe struct {
			Type        string `json:"type"`
			ModelCallID string `json:"model_call_id"`
			Message     *struct {
				ID   string `json:"id"`
				Role string `json:"role"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(ev.Raw), &probe); err != nil {
			continue
		}
		switch probe.Type {
		case "assistant":
			if probe.Message != nil && probe.Message.ID != "" {
				return true
			}
			if probe.ModelCallID != "" {
				return true
			}
		case "tool_call":
			if probe.ModelCallID != "" {
				return true
			}
		case "turn_end":
			if probe.Message != nil && probe.Message.Role == "assistant" {
				return true
			}
		case "turn.started", "turn.completed":
			return true
		}
	}
	return false
}
