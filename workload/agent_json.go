package workload

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
)

func normalizeResultStreamJSON(raw string, quotaReason func(string) *AgentQuotaPause) AgentResult {
	var assistantText []string
	var resultText string
	scanAgentJSONLines(raw, nil, func(line []byte) bool {
		var event struct {
			Type    string `json:"type"`
			Result  string `json:"result"`
			Message struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			return false
		}
		switch event.Type {
		case "assistant":
			for _, content := range event.Message.Content {
				if content.Type == "text" && content.Text != "" {
					assistantText = append(assistantText, content.Text)
				}
			}
		case "result":
			if event.Result != "" {
				resultText = event.Result
			}
		}
		return true
	})
	if quotaReason != nil {
		if pause := quotaReason(resultText); pause != nil {
			return AgentResult{QuotaPause: pause}
		}
	}
	if resultText != "" {
		return normalizedTranscript(resultText, nil)
	}
	return normalizedTranscript(strings.Join(assistantText, "\n"), nil)
}

func scanAgentJSONLines(raw string, diagnostics *[]string, handle func([]byte) bool) {
	scanner := bufio.NewScanner(strings.NewReader(raw))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) != 0 && !handle(line) {
			if diagnostics != nil {
				appendAgentDiagnostic(diagnostics, string(line))
			}
		}
	}
	if err := scanner.Err(); err != nil && diagnostics != nil {
		appendAgentDiagnostic(diagnostics, err.Error())
	}
}

func normalizedTranscript(transcript string, diagnostics []string) AgentResult {
	if transcript != "" {
		return AgentResult{Output: strings.TrimRight(transcript, "\n") + "\n"}
	}
	if len(diagnostics) != 0 {
		return AgentResult{Output: strings.Join(diagnostics, "\n") + "\n"}
	}
	return AgentResult{}
}

func appendAgentDiagnostic(diagnostics *[]string, diagnostic string) {
	if diagnostic = strings.TrimSpace(diagnostic); diagnostic != "" {
		*diagnostics = append(*diagnostics, diagnostic)
	}
}

func agentJSONDiagnostic(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var detail struct {
		Message      string `json:"message"`
		ErrorMessage string `json:"errorMessage"`
	}
	if err := json.Unmarshal(raw, &detail); err == nil {
		if detail.Message != "" {
			return detail.Message
		}
		return detail.ErrorMessage
	}
	return string(raw)
}
