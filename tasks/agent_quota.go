package tasks

import (
	"bufio"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	opencodeGoFiveHourQuotaSignal   = "5-hour usage limit reached"
	opencodeGoWeeklyQuotaSignal     = "weekly usage limit reached"
	opencodeGoMonthlyQuotaSignal    = "monthly usage limit reached"
	opencodeGoQuotaAssuranceOffset  = 2 * time.Minute
	opencodeGoFiveHourQuotaFallback = time.Hour
	opencodeGoWeeklyQuotaFallback   = 24 * time.Hour
	opencodeGoMonthlyQuotaFallback  = 30 * 24 * time.Hour
)

var (
	opencodeGoQuotaResetCompoundPattern = regexp.MustCompile(`(?i)\bResets in\s+([0-9]+)\s*hr\s+([0-9]+)\s*min\b`)
	opencodeGoQuotaResetMinutesPattern  = regexp.MustCompile(`(?i)\bResets in\s+([0-9]+)\s*min\b`)
	opencodeGoQuotaResetDaysPattern     = regexp.MustCompile(`(?i)\bResets in\s+([0-9]+)\s*days?\b`)
)

// opencodeGoQuotaPauseReason scans the raw agent capture line-by-line and
// returns an AgentQuotaPause when any line contains an opencode-go quota
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
		if opencodeGoQuotaSignalInLine(line) {
			return &AgentQuotaPause{Reason: line}
		}
	}
	return nil
}

func opencodeGoQuotaSignalInLine(line string) bool {
	lower := strings.ToLower(line)
	return strings.Contains(lower, opencodeGoFiveHourQuotaSignal) ||
		strings.Contains(lower, opencodeGoWeeklyQuotaSignal) ||
		strings.Contains(lower, opencodeGoMonthlyQuotaSignal)
}

// piQuotaResetAt derives PauseResetAt from opencode-go quota diagnostics.
// When the diagnostic includes "Resets in <N>min", "Resets in <H>hr <M>min",
// or "Resets in <N> days", the reset instant is now + parsed duration + a fixed
// assurance offset. When the reset phrase is absent or unparseable, a
// signal-specific backoff applies: one hour for the five-hour signal, one day
// for the weekly signal, thirty days for the monthly signal (each plus the
// assurance offset).
func piQuotaResetAt(reason string, now time.Time) time.Time {
	if duration, ok := opencodeGoQuotaResetDuration(reason); ok {
		return now.Add(duration).Add(opencodeGoQuotaAssuranceOffset)
	}
	if fallback := opencodeGoQuotaFallbackDuration(reason); fallback > 0 {
		return now.Add(fallback).Add(opencodeGoQuotaAssuranceOffset)
	}
	return time.Time{}
}

func opencodeGoQuotaResetDuration(reason string) (time.Duration, bool) {
	if m := opencodeGoQuotaResetCompoundPattern.FindStringSubmatch(reason); m != nil {
		hours, err1 := strconv.Atoi(m[1])
		minutes, err2 := strconv.Atoi(m[2])
		if err1 == nil && err2 == nil {
			return time.Duration(hours)*time.Hour + time.Duration(minutes)*time.Minute, true
		}
	}
	if m := opencodeGoQuotaResetMinutesPattern.FindStringSubmatch(reason); m != nil {
		minutes, err := strconv.Atoi(m[1])
		if err == nil {
			return time.Duration(minutes) * time.Minute, true
		}
	}
	if m := opencodeGoQuotaResetDaysPattern.FindStringSubmatch(reason); m != nil {
		days, err := strconv.Atoi(m[1])
		if err == nil {
			return time.Duration(days) * 24 * time.Hour, true
		}
	}
	return 0, false
}

func opencodeGoQuotaFallbackDuration(reason string) time.Duration {
	lower := strings.ToLower(reason)
	if strings.Contains(lower, opencodeGoMonthlyQuotaSignal) {
		return opencodeGoMonthlyQuotaFallback
	}
	if strings.Contains(lower, opencodeGoWeeklyQuotaSignal) {
		return opencodeGoWeeklyQuotaFallback
	}
	if strings.Contains(lower, opencodeGoFiveHourQuotaSignal) {
		return opencodeGoFiveHourQuotaFallback
	}
	return 0
}
