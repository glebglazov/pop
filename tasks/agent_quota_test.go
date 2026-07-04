package tasks

import (
	"strings"
	"testing"
	"time"
)

func TestOpencodeGoQuotaPauseReasonDetectsSignal(t *testing.T) {
	for _, line := range []string{
		"429 5-hour usage limit reached. Resets in 7min. Upgrade to continue.",
		"5-HOUR USAGE LIMIT REACHED",
		"prefix 5-hour usage limit reached suffix",
	} {
		t.Run(line, func(t *testing.T) {
			pause := opencodeGoQuotaPauseReason(line)
			if pause == nil {
				t.Fatal("expected quota pause")
			}
			if !strings.Contains(strings.ToLower(pause.Reason), "5-hour usage limit reached") {
				t.Fatalf("reason = %q", pause.Reason)
			}
		})
	}
}

func TestOpencodeGoQuotaPauseReasonIgnoresUnrelatedOutput(t *testing.T) {
	for _, raw := range []string{
		"an ordinary error occurred\n",
		"429 too many requests\n",
		"5 hour usage limit reached\n",
		"SUMMARY_START\ndone\nSUMMARY_END\nTASK_COMPLETE\n",
	} {
		t.Run(raw, func(t *testing.T) {
			if pause := opencodeGoQuotaPauseReason(raw); pause != nil {
				t.Fatalf("unexpected quota pause: %#v", pause)
			}
		})
	}
}

func TestOpencodeGoQuotaPauseReasonScansLineByLine(t *testing.T) {
	raw := `{"type":"message_end","message":{"role":"assistant","content":[]}}
429 5-hour usage limit reached. Resets in 12min.
`
	pause := opencodeGoQuotaPauseReason(raw)
	if pause == nil {
		t.Fatal("expected quota pause from plain line")
	}
	if !strings.Contains(pause.Reason, "429") {
		t.Fatalf("reason = %q, want full diagnostic line", pause.Reason)
	}
}

func TestPiQuotaResetAtParsesResetsInNMin(t *testing.T) {
	now := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	reason := "429 5-hour usage limit reached. Resets in 7min. Upgrade to continue."
	want := now.Add(7 * time.Minute).Add(opencodeGoQuotaAssuranceOffset)
	if got := piQuotaResetAt(reason, now); !got.Equal(want) {
		t.Fatalf("reset = %s, want %s", got, want)
	}
	if got := agentQuotaResetAt("pi", reason, now); !got.Equal(want) {
		t.Fatalf("reset via preset = %s, want %s", got, want)
	}
}

func TestPiQuotaResetAtReturnsZeroWhenPatternMissing(t *testing.T) {
	now := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	for _, reason := range []string{
		"429 5-hour usage limit reached.",
		"Resets in a few minutes.",
		"",
	} {
		t.Run(reason, func(t *testing.T) {
			if got := piQuotaResetAt(reason, now); !got.IsZero() {
				t.Fatalf("reset = %s, want zero", got)
			}
		})
	}
}

func TestNormalizePiJSONLDetectsOpencodeGoQuotaPause(t *testing.T) {
	raw := "429 5-hour usage limit reached. Resets in 7min. Upgrade to continue.\n"
	result := NormalizeAgentOutput(AgentOutputPiJSONL, raw)
	if result.QuotaPause == nil {
		t.Fatal("expected quota pause")
	}
	if !strings.Contains(result.QuotaPause.Reason, "5-hour usage limit reached") {
		t.Fatalf("reason = %q", result.QuotaPause.Reason)
	}
}

func TestNormalizePiJSONLNonLimitErrorIsNotQuotaPause(t *testing.T) {
	raw := `{"type":"message_end","message":{"role":"assistant","errorMessage":"400 bad request"}}` + "\n"
	result := NormalizeAgentOutput(AgentOutputPiJSONL, raw)
	if result.QuotaPause != nil {
		t.Fatalf("unexpected quota pause: %#v", result.QuotaPause)
	}
}

func TestNormalizeOpenCodeJSONDetectsQuotaPauseFromPlainLine(t *testing.T) {
	raw := "429 5-hour usage limit reached. Resets in 7min. Upgrade to continue.\n"
	result := NormalizeAgentOutput(AgentOutputOpenCodeJSON, raw)
	if result.QuotaPause == nil {
		t.Fatal("expected quota pause")
	}
	if !strings.Contains(result.QuotaPause.Reason, "5-hour usage limit reached") {
		t.Fatalf("reason = %q", result.QuotaPause.Reason)
	}
	if result.Output != "" {
		t.Fatalf("output = %q, want empty on quota pause", result.Output)
	}
}

func TestNormalizeOpenCodeJSONDetectsQuotaPauseFromJSONError(t *testing.T) {
	raw := `{"type":"step_start","sessionID":"1","part":{}}` + "\n" +
		`{"type":"error","sessionID":"1","error":{"message":"429 5-hour usage limit reached. Resets in 12min. Upgrade to continue."}}` + "\n"
	result := NormalizeAgentOutput(AgentOutputOpenCodeJSON, raw)
	if result.QuotaPause == nil {
		t.Fatal("expected quota pause from JSON error diagnostic")
	}
	if !strings.Contains(result.QuotaPause.Reason, "5-hour usage limit reached") {
		t.Fatalf("reason = %q", result.QuotaPause.Reason)
	}
}

func TestNormalizeOpenCodeJSONNonQuotaErrorIsNotQuotaPause(t *testing.T) {
	raw := `{"type":"error","sessionID":"1","error":{"message":"opencode failed"}}` + "\n"
	result := NormalizeAgentOutput(AgentOutputOpenCodeJSON, raw)
	if result.QuotaPause != nil {
		t.Fatalf("unexpected quota pause: %#v", result.QuotaPause)
	}
	if result.Output != "opencode failed\n" {
		t.Fatalf("output = %q, want diagnostic fallback", result.Output)
	}
}

func TestOpencodeQuotaResetAtSharesPiLogic(t *testing.T) {
	now := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	reason := "429 5-hour usage limit reached. Resets in 7min. Upgrade to continue."
	want := now.Add(7 * time.Minute).Add(opencodeGoQuotaAssuranceOffset)
	if got := agentQuotaResetAt("opencode", reason, now); !got.Equal(want) {
		t.Fatalf("reset = %s, want %s", got, want)
	}
}

func TestOpencodeQuotaResetAtReturnsZeroWhenPatternMissing(t *testing.T) {
	now := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	for _, reason := range []string{
		"429 5-hour usage limit reached.",
		"",
	} {
		t.Run(reason, func(t *testing.T) {
			if got := agentQuotaResetAt("opencode", reason, now); !got.IsZero() {
				t.Fatalf("reset = %s, want zero", got)
			}
		})
	}
}
