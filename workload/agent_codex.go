package workload

import "encoding/json"

func normalizeCodexJSONL(raw string) AgentResult {
	var transcript string
	var diagnostics []string
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
			if detail := agentJSONDiagnostic(event.Error); detail != "" {
				appendAgentDiagnostic(&diagnostics, detail)
			} else {
				appendAgentDiagnostic(&diagnostics, event.Message)
			}
		}
		return true
	})
	return normalizedTranscript(transcript, diagnostics)
}
