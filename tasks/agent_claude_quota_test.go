package tasks

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// The instant claude's rate-limit event stated in the captured refusal:
// resetsAt 1787598000 — 2026-08-24T19:00:00Z, the moment the five-hour window
// actually reopened.
var claudeCaptureStatedReset = time.Unix(1787598000, 0).UTC()

// claudeCaptureRefusedAt is when the captured run was refused, from the drain
// that recorded it. It is an hour and two minutes before the stated reset, which
// is what makes this capture worth keeping: the blind hour pop used to fall back
// on expires while the window is still shut.
var claudeCaptureRefusedAt = time.Date(2026, 8, 24, 17, 58, 22, 0, time.UTC)

// claudeSessionLimitCapture returns the captured session-limit run as the
// adapter sees it: the raw lines of the real recorded stream, in order. The
// fixture is the run pop drained on 2026-08-24 — `claude --model opus --effort
// high`, exit 1 — trimmed to its twelve rate-limit events, the synthetic
// assistant message carrying the refusal, and the terminal result event; the
// prose and tool calls of the work itself are cut, and nothing is hand-written
// (ADR-0165).
func claudeSessionLimitCapture(t *testing.T) string {
	t.Helper()
	events, err := loadStreamFixture("claude-session-limit")
	if err != nil {
		t.Fatalf("load claude-session-limit fixture: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("claude-session-limit fixture is empty")
	}
	lines := make([]string, 0, len(events))
	for _, ev := range events {
		lines = append(lines, ev.Raw)
	}
	return strings.Join(lines, "\n")
}

// The refusal's own sentence — "resets 9pm (Europe/Madrid)" — is the one pop
// could not read, so the capture is dated from the epoch beside it instead.
func TestClaudeSessionLimitCaptureIsDatedFromItsRateLimitEvent(t *testing.T) {
	t.Parallel()
	raw := claudeSessionLimitCapture(t)

	result := NormalizeAgentOutput(AgentOutputClaudeStreamJSON, raw)
	if result.ProceedVerdict == nil {
		t.Fatal("no verdict from the captured session-limit run")
	}
	v := *result.ProceedVerdict
	if v.Kind != ProceedQuotaPause {
		t.Fatalf("Kind = %q, want %q", v.Kind, ProceedQuotaPause)
	}
	if v.Scope != ProceedScopePreset {
		t.Fatalf("Scope = %q, want %q", v.Scope, ProceedScopePreset)
	}
	if !strings.Contains(v.Reason, "You've hit your session limit") {
		t.Fatalf("Reason = %q, want the provider diagnostic", v.Reason)
	}
	want := claudeCaptureStatedReset.Add(quotaAssuranceOffset)
	if !v.ResetAt.Equal(want) {
		t.Fatalf("ResetAt = %s, want %s (the epoch the rate-limit event stated, padded)", v.ResetAt, want)
	}
	// The wire is where that instant came from: the rate-limit event alone
	// yields it, before the prose clause is consulted at all.
	if got := claudeRateLimitResetAt(raw, claudeCaptureRefusedAt); !got.Equal(want) {
		t.Fatalf("wire reset = %s, want %s — the capture is dated from its rate-limit event", got, want)
	}
}

// The incident this capture is from: the executor re-derived the instant from
// the reason, got nothing, and cooled the preset for a blind hour that expired
// two minutes before the window opened — so the retry was refused on the edge
// and bought another hour, this time an hour past the truth.
func TestClaudeQuotaCooldownOutlastsTheStatedResetRatherThanTheRefusal(t *testing.T) {
	t.Parallel()
	raw := claudeSessionLimitCapture(t)
	result := NormalizeAgentOutput(AgentOutputClaudeStreamJSON, raw)
	if result.ProceedVerdict == nil {
		t.Fatal("no verdict from the captured session-limit run")
	}

	v := stampDetectedVerdict(*result.ProceedVerdict, "claude", "opus")
	v = resolveProceedResetAt(v, claudeCaptureRefusedAt)
	if !v.ResetAt.Equal(claudeCaptureStatedReset.Add(quotaAssuranceOffset)) {
		t.Fatalf("ResetAt = %s, want the adapter's instant kept through resolution", v.ResetAt)
	}

	row := agentQuotaCooldownRow(quotaCooldownRequest(v), claudeCaptureRefusedAt, defaultUnclassedQuotaCeiling)
	until := row.ExhaustedUntil
	if row.StatedUntil.IsZero() {
		t.Fatalf("row recorded no stated instant, want the provider's %s: a read reset must never look like a guess", v.ResetAt)
	}
	if !until.After(claudeCaptureStatedReset) {
		t.Fatalf("cooldown until %s, want it to outlast the stated reset %s", until, claudeCaptureStatedReset)
	}
	if blind := claudeCaptureRefusedAt.Add(defaultUnclassedQuotaCeiling); until.Equal(blind) {
		t.Fatalf("cooldown fell back to the unclassed ceiling at %s", blind)
	}
	want := claudeCaptureStatedReset.Add(quotaAssuranceOffset)
	if !until.Equal(want) {
		t.Fatalf("cooldown until %s, want %s", until, want)
	}
	// One offset, not two: the row holds the instant the wait sleeps on
	// (ADR-0235).
	if !until.Equal(v.ResetAt) {
		t.Fatalf("cooldown until %s, want the verdict's reset %s", until, v.ResetAt)
	}
}

// Every rate-limit event but the last one in that capture reports a window pop
// is still spending in. Reading one as a reset would park a healthy drain, so
// only a rejection dates a pause.
func TestClaudeRateLimitWarningsDateNothing(t *testing.T) {
	t.Parallel()
	raw := claudeSessionLimitCapture(t)
	var allowed []string
	for _, line := range strings.Split(raw, "\n") {
		if strings.Contains(line, `"rate_limit_event"`) && !strings.Contains(line, `"status":"rejected"`) {
			allowed = append(allowed, line)
		}
	}
	if len(allowed) < 2 {
		t.Fatalf("fixture carries %d non-rejecting rate-limit events, want the run's warnings", len(allowed))
	}
	if got := claudeRateLimitResetAt(strings.Join(allowed, "\n"), claudeCaptureRefusedAt); !got.IsZero() {
		t.Fatalf("reset from allowed/allowed_warning events = %s, want zero", got)
	}
}

// A capture with no rate-limit event at all — an older claude, or a refusal that
// arrives without one — still falls back to the sentence, which is what pop read
// before there was anything better.
func TestClaudeQuotaResetFallsBackToTheProseClause(t *testing.T) {
	t.Parallel()
	raw := `{"type":"result","subtype":"error_during_execution","result":"You've hit your weekly limit · resets Mon 12:00am"}`
	now := time.Date(2026, 6, 11, 15, 0, 0, 0, time.Local) // Thu
	want := time.Date(2026, 6, 15, 0, 0, 0, 0, time.Local).Add(quotaAssuranceOffset) // next Mon, padded once

	if got := claudeRateLimitResetAt(raw, now); !got.IsZero() {
		t.Fatalf("wire reset = %s, want zero when the capture states none", got)
	}
	if got := claudeStreamQuotaResetAt(raw, "You've hit your weekly limit · resets Mon 12:00am", now); !got.Equal(want) {
		t.Fatalf("reset = %s, want the prose clause's %s", got, want)
	}
}

// The sentence pop could not read, read on its own terms: the hour without
// minutes, in the zone the message names rather than the machine's.
func TestClaudeQuotaResetAtReadsAnHourOnlyClauseInItsStatedZone(t *testing.T) {
	t.Parallel()
	madrid, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		t.Skipf("no zone database for Europe/Madrid: %v", err)
	}
	const reason = "You've hit your session limit \u00b7 resets 9pm (Europe/Madrid)"

	// A machine four zones west of the account still waits for the account's 9pm.
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("no zone database for America/New_York: %v", err)
	}
	now := time.Date(2026, 8, 24, 13, 58, 22, 0, newYork)
	want := time.Date(2026, 8, 24, 21, 0, 0, 0, madrid).Add(quotaAssuranceOffset)
	if got := claudeQuotaResetAt(reason, now); !got.Equal(want) {
		t.Fatalf("reset = %s, want %s", got, want)
	}

	// And the same sentence read from inside that zone lands on the same instant.
	if got := claudeQuotaResetAt(reason, now.In(madrid)); !got.Equal(want) {
		t.Fatalf("reset from Madrid = %s, want %s", got, want)
	}
	// It is the epoch the capture states, which is the point: the two channels
	// agree once the sentence is read the way it was written.
	if !want.Equal(claudeCaptureStatedReset.Add(quotaAssuranceOffset)) {
		t.Fatalf("prose reset %s disagrees with the stated epoch %s", want, claudeCaptureStatedReset)
	}
}

// An hour with a weekday, likewise — and a zone pop cannot load is not a reason
// to refuse the hour, only to read it locally.
func TestClaudeQuotaResetAtHourOnlyWeekdayAndUnknownZone(t *testing.T) {
	t.Parallel()
	loc := time.FixedZone("local", 2*60*60)
	now := time.Date(2026, 6, 11, 15, 0, 0, 0, loc) // Thu

	want := time.Date(2026, 6, 15, 0, 0, 0, 0, loc).Add(quotaAssuranceOffset) // next Mon, padded once
	if got := claudeQuotaResetAt("You've hit your weekly limit \u00b7 resets Mon 12am", now); !got.Equal(want) {
		t.Fatalf("weekday hour-only reset = %s, want %s", got, want)
	}

	wantLocal := time.Date(2026, 6, 11, 21, 0, 0, 0, loc).Add(quotaAssuranceOffset)
	if got := claudeQuotaResetAt("You've hit your session limit \u00b7 resets 9pm (Mars/Olympus_Mons)", now); !got.Equal(wantLocal) {
		t.Fatalf("unloadable zone reset = %s, want the local reading %s", got, wantLocal)
	}
}

// An epoch is not prose: it can state any instant at all, and the drain waits on
// it directly. One beyond the horizon the cooldown store would accept is read as
// garbage, so the sentence — and then the blind hour — answer instead.
func TestClaudeRateLimitResetBeyondTheHorizonIsGarbage(t *testing.T) {
	t.Parallel()
	now := claudeCaptureRefusedAt
	absurd := now.Add(30 * 24 * time.Hour).Unix()
	raw := fmt.Sprintf(`{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","resetsAt":%d,"rateLimitType":"five_hour"}}`, absurd)

	if got := claudeRateLimitResetAt(raw, now); !got.IsZero() {
		t.Fatalf("reset = %s, want zero for an epoch %s out", got, maxAgentQuotaResetHorizon)
	}
	// The horizon is the store's, so whatever survives here is an instant the
	// cooldown row will honour as a statement rather than discard.
	inside := now.Add(maxAgentQuotaResetHorizon - time.Hour).Truncate(time.Second)
	raw = fmt.Sprintf(`{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","resetsAt":%d,"rateLimitType":"weekly"}}`, inside.Unix())
	got := claudeRateLimitResetAt(raw, now)
	if !got.Equal(inside.Add(quotaAssuranceOffset)) {
		t.Fatalf("reset = %s, want %s", got, inside.Add(quotaAssuranceOffset))
	}
	req := AgentQuotaCooldownRequest{Preset: "claude", Stated: got, Class: QuotaWindowWeekly}
	if row := agentQuotaCooldownRow(req, now, defaultUnclassedQuotaCeiling); !row.ExhaustedUntil.After(inside) || row.StatedUntil.IsZero() {
		t.Fatalf("cooldown row %+v, want a stated instant outlasting %s", row, inside)
	}
}
