package workload

import "strings"

func normalizeClaudeStreamJSON(raw string) AgentResult {
	return normalizeResultStreamJSON(raw, claudeQuotaPauseReason)
}

func claudeQuotaPauseReason(result string) *AgentQuotaPause {
	for _, marker := range []string{
		"You've hit your session limit",
		"You've hit your weekly limit",
		"You've hit your Opus limit",
	} {
		if strings.Contains(result, marker) {
			return &AgentQuotaPause{Reason: result}
		}
	}
	return nil
}
