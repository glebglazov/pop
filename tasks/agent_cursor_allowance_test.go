package tasks

import (
	"strings"
	"testing"
	"time"
)

// cursorAllowanceRefusalCapture returns the captured cursor-agent spent-allowance
// run as the adapter sees it: the raw lines of the real recorded stream, in
// order. The fixture is the run pop drained on 2026-08-09 —
// `cursor --model claude-opus-5-thinking-medium`, exit 1, three events — not a
// hand-written line (ADR-0165).
func cursorAllowanceRefusalCapture(t *testing.T) string {
	t.Helper()
	events, err := loadStreamFixture("cursor-spent-allowance")
	if err != nil {
		t.Fatalf("load cursor-spent-allowance fixture: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("cursor-spent-allowance fixture is empty")
	}
	lines := make([]string, 0, len(events))
	for _, ev := range events {
		lines = append(lines, ev.Raw)
	}
	return strings.Join(lines, "\n")
}

// The refusal the capture carries names one model and leaves the CLI healthy, so
// the tier walk — not Agent fallback — is what should answer it.
func TestCursorSpentAllowanceCaptureYieldsModelScopedTimeHealingVerdict(t *testing.T) {
	t.Parallel()
	raw := cursorAllowanceRefusalCapture(t)
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.Local)

	v := cursorSpentAllowanceReason(raw, now)
	if v == nil {
		t.Fatal("no verdict from the captured spent-allowance run")
	}
	if v.Kind != ProceedModelRefused {
		t.Fatalf("Kind = %q, want %q", v.Kind, ProceedModelRefused)
	}
	if v.Scope != ProceedScopeModel {
		t.Fatalf("Scope = %q, want %q", v.Scope, ProceedScopeModel)
	}
	if v.Recovery != ProceedRecoveryTime {
		t.Fatalf("Recovery = %q, want %q", v.Recovery, ProceedRecoveryTime)
	}
	if v.ConsumesAttempt {
		t.Fatal("a spent-allowance refusal must not charge the retry cap")
	}
	if !strings.Contains(v.Reason, "hit your usage limit") {
		t.Fatalf("Reason = %q, want the provider diagnostic", v.Reason)
	}
	want := time.Date(2026, 9, 4, 0, 0, 0, 0, time.Local)
	if !v.ResetAt.Equal(want) {
		t.Fatalf("ResetAt = %s, want %s (the 9/4/2026 the message states)", v.ResetAt, want)
	}
	// The message names the family ("Opus"), never the ladder entry, so the
	// invocation's pinned model is what the skip is keyed by.
	if v.Model != "" {
		t.Fatalf("Model = %q, want it left for the caller to stamp", v.Model)
	}
	if got := stampDetectedVerdict(*v, "cursor", "claude-opus-5-thinking-medium"); got.Model != "claude-opus-5-thinking-medium" {
		t.Fatalf("stamped Model = %q, want the pinned ladder entry", got.Model)
	}
}

// The whole normalization path must reach the same verdict, since that is what a
// drain actually calls.
func TestNormalizeCursorStreamJSONDetectsSpentAllowance(t *testing.T) {
	t.Parallel()
	res := normalizeCursorStreamJSON(cursorAllowanceRefusalCapture(t))
	if res.ProceedVerdict == nil {
		t.Fatal("normalizeCursorStreamJSON returned no proceed verdict")
	}
	if res.ProceedVerdict.Scope != ProceedScopeModel {
		t.Fatalf("Scope = %q, want %q", res.ProceedVerdict.Scope, ProceedScopeModel)
	}
}

// An unrecognised refusal must degrade to exactly today's behaviour (ADR-0153):
// no verdict at all, so the run is an ordinary failure.
func TestCursorUnrecognisedRefusalYieldsNoVerdict(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		"Error: something else went wrong entirely",
		"ActionRequiredError: You must accept the updated terms of service.",
		`{"type":"result","subtype":"success"}`,
	} {
		if v := cursorSpentAllowanceReason(raw, time.Now()); v != nil {
			t.Fatalf("raw %q produced verdict %+v, want none", raw, v)
		}
		if res := normalizeCursorStreamJSON(raw); res.ProceedVerdict != nil {
			t.Fatalf("raw %q produced verdict %+v through normalization, want none", raw, res.ProceedVerdict)
		}
	}
}

func TestCursorQuotaResetAt(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.Local)
	cases := []struct {
		name   string
		reason string
		want   time.Time
	}{
		{"stated date", "…reset when your monthly cycle ends on 9/4/2026.", time.Date(2026, 9, 4, 0, 0, 0, 0, time.Local)},
		{"no date", "You've hit your usage limit for Opus.", time.Time{}},
		{"date already behind now", "…ends on 1/2/2026.", time.Time{}},
		{"nonsense month", "…ends on 19/4/2026.", time.Time{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cursorQuotaResetAt(tc.reason, now); !got.Equal(tc.want) {
				t.Fatalf("cursorQuotaResetAt = %s, want %s", got, tc.want)
			}
		})
	}
}

// The capture disproves the blindness cursor's adapter used to declare.
func TestCursorAdapterParsesQuotaReset(t *testing.T) {
	t.Parallel()
	adapter, err := ResolveAgentAdapter("cursor")
	if err != nil {
		t.Fatal(err)
	}
	cap := adapter.QuotaResetCapability()
	if cap.Kind != CapabilitySupported {
		t.Fatalf("cursor quota-reset capability = %v, want Supported", cap.Kind)
	}
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.Local)
	if got := cap.resetAt("…ends on 9/4/2026.", now); got.IsZero() {
		t.Fatal("cursor quota-reset capability parsed no instant from a dated diagnostic")
	}
}

// The skip horizon caps at 24 hours whatever the message claims, because a
// spent-allowance probe is cheap and a month-long park would sit blind through a
// top-up or a plan change (ADR-0168).
func TestModelSkipCooldownUntilCapsTheHorizon(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		resetAt time.Time
		want    time.Time
	}{
		{"captured 9/4/2026 clamps to 24h", time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC), now.Add(maxModelSkipHorizon)},
		{"nearer date honoured as-is", now.Add(45 * time.Minute), now.Add(45*time.Minute + agentQuotaResetSkew)},
		{"no date keeps the one hour default", time.Time{}, now.Add(defaultAgentQuotaRetryAfter)},
		{"date already behind now keeps the default", now.Add(-time.Minute), now.Add(defaultAgentQuotaRetryAfter)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := modelSkipCooldownUntil(tc.resetAt, now); !got.Equal(tc.want) {
				t.Fatalf("modelSkipCooldownUntil = %s, want %s", got, tc.want)
			}
		})
	}
}

// The clamp must not swallow what the provider said: the two instants disagree
// by design, so both are recorded and both are rendered.
func TestModelSkipRetainsStatedResetBesideCappedExpiry(t *testing.T) {
	t.Parallel()
	d := newTestDeps(t)
	stated := time.Now().Add(26 * 24 * time.Hour)

	if err := updateAgentModelCooldown(d, "cursor", "claude-opus-5-thinking-medium", stated, false); err != nil {
		t.Fatalf("updateAgentModelCooldown: %v", err)
	}

	now := time.Now()
	active, err := ActiveAgentModelCooldownsWith(d, now)
	if err != nil {
		t.Fatalf("ActiveAgentModelCooldownsWith: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("active = %#v, want 1 entry", active)
	}
	row := active[0]
	if diff := row.Until.Sub(now.Add(maxModelSkipHorizon)); diff < -time.Minute || diff > time.Minute {
		t.Fatalf("Until = %s, want ~24h out (capped)", row.Until)
	}
	if diff := row.StatedUntil.Sub(stated); diff < -time.Second || diff > time.Second {
		t.Fatalf("StatedUntil = %s, want the stated %s", row.StatedUntil, stated)
	}

	got := FormatModelSkipHorizon(ModelSkipHorizon{Until: row.Until, StatedUntil: row.StatedUntil}, now)
	if !strings.HasPrefix(got, FormatModelSkipRemaining(row.Until, now)) || !strings.Contains(got, "stated 25d") {
		t.Fatalf("rendered skip = %q, want both the capped expiry and the stated reset", got)
	}
}

// A skip pop enforces for exactly as long as the provider said needs no second
// number: the surfaces stay quiet when the two agree.
func TestFormatModelSkipHorizonQuietWhenInstantsAgree(t *testing.T) {
	t.Parallel()
	now := time.Now()
	until := now.Add(47 * time.Minute)
	for _, h := range []ModelSkipHorizon{
		{Until: until},
		{Until: until, StatedUntil: until.Add(30 * time.Second)},
		{StatedUntil: now.Add(time.Hour)}, // permanent
	} {
		if got := FormatModelSkipHorizon(h, now); strings.Contains(got, "stated") {
			t.Fatalf("rendered %q for %+v, want no stated clause", got, h)
		}
	}
}
