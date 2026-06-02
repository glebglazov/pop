package workload

import (
	"encoding/json"
	"strings"
)

func normalizeOpenCodeJSON(raw string) AgentResult {
	var transcript strings.Builder
	var diagnostics []string
	scanAgentJSONLines(raw, nil, func(line []byte) bool {
		var event struct {
			Type  string          `json:"type"`
			Error json.RawMessage `json:"error"`
			Part  struct {
				Text string `json:"text"`
			} `json:"part"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			return false
		}
		switch event.Type {
		case "text":
			transcript.WriteString(event.Part.Text)
		case "error":
			appendAgentDiagnostic(&diagnostics, agentJSONDiagnostic(event.Error))
		}
		return true
	})
	return normalizedTranscript(transcript.String(), diagnostics)
}
