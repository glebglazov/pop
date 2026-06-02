package workload

import "encoding/json"

func normalizePiJSONL(raw string) AgentResult {
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
			transcript = message
		}
		return true
	})
	return normalizedTranscript(transcript, diagnostics)
}
