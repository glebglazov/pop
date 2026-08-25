package tasks

import (
	"strings"
	"testing"
	"time"
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

// codexSpendCapStreamWithTypedField is a spend-capped codex capture carrying
// the token_count event codex actually sends ahead of the refusal: rate_limits
// names the workspace cap on rate_limit_reached_type before either diagnostic
// channel says so in prose (ADR-0234).
func codexSpendCapStreamWithTypedField(message string) string {
	return `{"type":"thread.started","thread_id":"t"}` + "\n" +
		`{"type":"turn.started"}` + "\n" +
		`{"type":"token_count","info":{},"rate_limits":{"rate_limit_reached_type":"` + codexSpendCapRateLimitType + `"}}` + "\n" +
		`{"type":"error","message":"` + message + `"}` + "\n" +
		`{"type":"turn.failed","error":{"message":"` + message + `"}}`
}

// The typed field is the reading: rate_limit_reached_type on the token_count
// event ahead of the refusal, unread before ADR-0234 (ADR-0231 recorded its
// existence and did not read it).
func TestCodexReadsItsSpendCapFromTheTypedRateLimitTypeField(t *testing.T) {
	t.Parallel()
	raw := codexSpendCapStreamWithTypedField(codexSpendCapMessage)

	class, refused := codexStructuredSpendCap(raw)
	if !refused {
		t.Fatal("the captured spend cap was not read from its typed field")
	}
	if class != QuotaWindowUnknown {
		t.Fatalf("class = %q, want %q — a spend cap names no allowance window", class, QuotaWindowUnknown)
	}

	v := normalizeCodexJSONL(raw).ProceedVerdict
	if v == nil || v.Kind != ProceedSpendCap {
		t.Fatalf("verdict = %#v, want a spend cap", v)
	}
}

// A reworded refusal costs pop nothing now: the "spend cap" substring is gone,
// the typed field beside it still says what happened, and the provider's own
// sentence still reaches the reason a human reads.
func TestCodexSpendCapSurvivesARewordedSentenceWithIntactTypedField(t *testing.T) {
	t.Parallel()
	reworded := "Your workspace has reached its spending ceiling for this billing period."
	if strings.Contains(strings.ToLower(reworded), spendCapSignal) {
		t.Fatalf("reworded message still carries the substring %q", spendCapSignal)
	}
	raw := codexSpendCapStreamWithTypedField(reworded)

	v := normalizeCodexJSONL(raw).ProceedVerdict
	if v == nil {
		t.Fatal("a reworded spend cap with an intact typed field went undetected")
	}
	if v.Kind != ProceedSpendCap {
		t.Fatalf("Kind = %q, want %q", v.Kind, ProceedSpendCap)
	}
	if v.Reason != reworded {
		t.Fatalf("Reason = %q, want the provider's reworded sentence %q", v.Reason, reworded)
	}
}

// And a capture with no token_count event at all is still read: the substring
// stays, beneath, exactly as it always has.
func TestCodexSpendCapFallsBackToTheSubstringWithoutTheTypedField(t *testing.T) {
	t.Parallel()
	raw := codexSpendCapStream()
	if _, refused := codexStructuredSpendCap(raw); refused {
		t.Fatal("a capture with no token_count event still read as structured")
	}

	v := normalizeCodexJSONL(raw).ProceedVerdict
	if v == nil || v.Kind != ProceedSpendCap {
		t.Fatalf("verdict = %#v, want a spend cap detected from the substring", v)
	}
	if v.Reason != codexSpendCapMessage {
		t.Fatalf("Reason = %q, want %q", v.Reason, codexSpendCapMessage)
	}
}

// TestBlindRefusalSignatureStillDetectsViaProse pins that declaring a Blind
// refusal signature changed nothing about the four providers that have no
// structured channel to read: each still finds its own refusal from its own
// prose detector, exactly as it did before this capability existed (ADR-0234).
func TestBlindRefusalSignatureStillDetectsViaProse(t *testing.T) {
	t.Parallel()
	for _, preset := range []string{"cursor", "kimi", "opencode", "pi"} {
		adapter, err := ResolveAgentAdapter(preset)
		if err != nil {
			t.Fatalf("resolve %s: %v", preset, err)
		}
		sig := adapter.RefusalSignatureCapability()
		if sig.Kind != CapabilityBlind {
			t.Fatalf("%s refusal-signature Kind = %v, want Blind", preset, sig.Kind)
		}
		if strings.TrimSpace(sig.Reason) == "" {
			t.Fatalf("%s Blind refusal-signature carries no reason naming why it has no structured channel", preset)
		}
	}

	// cursor: a bare stderr line, model-scoped rather than preset-scoped.
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.Local)
	if v := cursorSpentAllowanceReason(cursorAllowanceRefusalCapture(t), now); v == nil || v.Kind != ProceedModelRefused {
		t.Fatalf("cursor prose refusal = %#v, want an unchanged model-scoped refusal", v)
	}
	// kimi: a stderr-only quota diagnostic, never on its stream-json.
	if v := kimiProceedVerdict("Error: usage limit for this period reached, try again later."); v == nil || v.Kind != ProceedQuotaPause {
		t.Fatalf("kimi prose refusal = %#v, want an unchanged quota pause", v)
	}
	// opencode and pi share the opencode-go matcher over the raw capture.
	if v := opencodeGoQuotaPauseReason("5-hour usage limit reached"); v == nil || v.Kind != ProceedQuotaPause {
		t.Fatalf("opencode/pi prose refusal = %#v, want an unchanged quota pause", v)
	}
}
