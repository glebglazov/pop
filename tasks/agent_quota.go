package tasks

import (
	"bufio"
	"regexp"
	"strings"
	"time"
)

const (
	opencodeGoQuotaSignal          = "5-hour usage limit reached"
	opencodeGoQuotaAssuranceOffset = 2 * time.Minute
)

var opencodeGoQuotaResetPattern = regexp.MustCompile(`(?i)\bResets in\s+([0-9]+)\s*min\b`)

// opencodeGoQuotaPauseReason scans the raw agent capture line-by-line and
// returns an AgentQuotaPause when any line contains the opencode-go quota
// signal. The full matching line becomes the pause reason so downstream reset
// parsing can inspect provider-specific phrasing.
func opencodeGoQuotaPauseReason(raw string) *AgentQuotaPause {
	scanner := bufio.NewScanner(strings.NewReader(raw))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.Contains(strings.ToLower(line), opencodeGoQuotaSignal) {
			return &AgentQuotaPause{Reason: line}
		}
	}
	return nil
}

// piQuotaResetAt parses the "Resets in <N>min" phrase reported by opencode-go
// through the pi preset. The reset instant is now + N + a fixed assurance
// offset. When the pattern is absent or unparseable, a zero time is returned.
func piQuotaResetAt(reason string, now time.Time) time.Time {
	m := opencodeGoQuotaResetPattern.FindStringSubmatch(reason)
	if m == nil {
		return time.Time{}
	}
	minutes, err := time.ParseDuration(m[1] + "m")
	if err != nil {
		return time.Time{}
	}
	return now.Add(minutes).Add(opencodeGoQuotaAssuranceOffset)
}
