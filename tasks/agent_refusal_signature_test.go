package tasks

import (
	"strings"
	"testing"
)

// claudeCaptureReworded is the same capture with the provider's sentence
// copy-edited — the failure ADR-0234 was written for, where three English
// literals were the entire detector.
func claudeCaptureReworded(t *testing.T, raw string) string {
	t.Helper()
	reworded := strings.ReplaceAll(raw, "You've hit your session limit", "Your session allowance is used up")
	for _, marker := range claudeRefusalSignature.Markers {
		if strings.Contains(reworded, marker.Sentence) {
			t.Fatalf("reworded capture still carries the marker %q", marker.Sentence)
		}
	}
	return reworded
}

// claudeCaptureWithoutTypedFields is the same capture as an older claude — or a
// changed event schema — would leave it: no rate-limit events at all, and no
// refusal status on the terminal result. The sentence is all that is left.
func claudeCaptureWithoutTypedFields(raw string) string {
	var lines []string
	for _, line := range strings.Split(raw, "\n") {
		if strings.Contains(line, `"rate_limit_event"`) {
			continue
		}
		lines = append(lines, strings.ReplaceAll(line, `"api_error_status":429`, `"api_error_status":0`))
	}
	return strings.Join(lines, "\n")
}

// The typed fields are the reading: the terminal result's 429 together with a
// rate-limit event reporting a rejection, and the window class off the same
// event (ADR-0234).
func TestClaudeReadsItsRefusalFromTheTypedFieldsOfItsCapture(t *testing.T) {
	t.Parallel()
	raw := claudeSessionLimitCapture(t)

	class, refused := claudeStructuredRefusal(raw)
	if !refused {
		t.Fatal("the captured refusal was not read from its typed fields")
	}
	if class != QuotaWindowFiveHour {
		t.Fatalf("window class = %q, want %q — the rateLimitType the event states", class, QuotaWindowFiveHour)
	}

	result := NormalizeAgentOutput(AgentOutputClaudeStreamJSON, raw)
	if result.ProceedVerdict == nil {
		t.Fatal("no verdict from the captured session-limit run")
	}
	if got := result.ProceedVerdict.WindowClass; got != QuotaWindowFiveHour {
		t.Fatalf("verdict WindowClass = %q, want %q", got, QuotaWindowFiveHour)
	}
}

// A copy-edit costs pop nothing now: the sentence is gone, the fields beside it
// still say what happened, and the class still comes from the typed field.
func TestClaudeRefusalSurvivesARewordedMarker(t *testing.T) {
	t.Parallel()
	raw := claudeCaptureReworded(t, claudeSessionLimitCapture(t))

	result := NormalizeAgentOutput(AgentOutputClaudeStreamJSON, raw)
	if result.ProceedVerdict == nil {
		t.Fatal("a reworded refusal with intact typed fields went undetected")
	}
	v := *result.ProceedVerdict
	if v.Kind != ProceedQuotaPause {
		t.Fatalf("Kind = %q, want %q", v.Kind, ProceedQuotaPause)
	}
	if v.WindowClass != QuotaWindowFiveHour {
		t.Fatalf("WindowClass = %q, want %q from the typed field", v.WindowClass, QuotaWindowFiveHour)
	}
	if !strings.Contains(v.Reason, "Your session allowance is used up") {
		t.Fatalf("Reason = %q, want the provider's reworded sentence", v.Reason)
	}
}

// And a capture with no typed fields at all is still read, because the markers
// were demoted rather than deleted — they are the detection and the class alike.
func TestClaudeRefusalSurvivesAbsentTypedFields(t *testing.T) {
	t.Parallel()
	raw := claudeCaptureWithoutTypedFields(claudeSessionLimitCapture(t))

	if _, refused := claudeStructuredRefusal(raw); refused {
		t.Fatal("the stripped capture still reads as a structured refusal")
	}
	result := NormalizeAgentOutput(AgentOutputClaudeStreamJSON, raw)
	if result.ProceedVerdict == nil {
		t.Fatal("a refusal carrying only its marker sentence went undetected")
	}
	if got := result.ProceedVerdict.WindowClass; got != QuotaWindowFiveHour {
		t.Fatalf("WindowClass = %q, want %q — the window the sentence itself names", got, QuotaWindowFiveHour)
	}
}

// Each marker names its own limit, so a capture that states no rate-limit type
// is still classified. This is the reading the demoted sentences keep earning
// their line for.
func TestClaudeWindowClassFallsBackToTheMarkerSentence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		sentence string
		want     AgentQuotaWindowClass
	}{
		{"You've hit your session limit · resets 9pm", QuotaWindowFiveHour},
		{"You've hit your weekly limit · resets Mon 12am", QuotaWindowWeekly},
		{"You've hit your Opus limit · resets Mon 12am", QuotaWindowOpus},
	} {
		v := claudeRefusalSignature.detectRefusal("", tc.sentence)
		if v == nil {
			t.Fatalf("%q went undetected", tc.sentence)
		}
		if v.WindowClass != tc.want {
			t.Fatalf("%q: WindowClass = %q, want %q", tc.sentence, v.WindowClass, tc.want)
		}
	}
}

// The pair is the reading. A 429 with no rejection is a transient overload of an
// API still willing to serve this account, and parking a drain on one would cost
// it an hour it never owed.
func TestClaudeRefusalNeedsBothTypedFields(t *testing.T) {
	t.Parallel()
	raw := claudeCaptureReworded(t, claudeSessionLimitCapture(t))
	var kept []string
	for _, line := range strings.Split(raw, "\n") {
		if strings.Contains(line, `"status":"rejected"`) {
			continue
		}
		kept = append(kept, line)
	}
	overloaded := strings.Join(kept, "\n")
	if !strings.Contains(overloaded, `"api_error_status":429`) {
		t.Fatal("the overload capture lost its 429")
	}

	if _, refused := claudeStructuredRefusal(overloaded); refused {
		t.Fatal("a 429 with no rejecting rate-limit event was read as a refusal")
	}
	if result := NormalizeAgentOutput(AgentOutputClaudeStreamJSON, overloaded); result.ProceedVerdict != nil {
		t.Fatalf("verdict %+v from a capture that refused nothing", *result.ProceedVerdict)
	}
}
