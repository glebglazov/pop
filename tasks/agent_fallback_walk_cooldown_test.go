package tasks

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
)

// walkOverPresets runs the shared fallback walk in the Reviewer's role over the
// given presets, which is the cheapest way to drive the walk itself: the role
// only decides what is filed and what counts as an answer, and neither is what
// these tests are about.
func walkOverPresets(t *testing.T, d *Deps, taskSetDir string, out *bytes.Buffer, presets ...string) (agentWalkResult, error) {
	t.Helper()
	return runAgentFallbackWalk(d, agentFallbackWalk{
		role:            reviewerRole(d, out, taskSetDir, "demo", "sha1"),
		sel:             verifierSelection{Agents: presets, Effort: "heavy"},
		runtimePath:     "/rt",
		prompt:          "prompt",
		out:             out,
		errOut:          out,
		timeout:         time.Minute,
		maxTries:        1,
		retryDelays:     append([]time.Duration(nil), config.DefaultTaskAttemptRetryDelays...),
		quotaRetryAfter: time.Hour,
	})
}

// TestFallbackWalkSkipsACoolingPresetAndFallsThrough pins the walk reading the
// machine-global cooldown store the way an implement attempt does: a preset a
// quota pause already condemned is never invoked again while it cools, the skip
// is reported as a quota pause carrying that cooldown's until, and the next
// preset in the list answers.
func TestFallbackWalkSkipsACoolingPresetAndFallsThrough(t *testing.T) {
	taskSetDir := t.TempDir()
	d, runner := reviewRunnerDeps(t, scriptedReviewRun{output: claudeReviewStream("## Naming\\nAll good.")})
	until := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	if err := updateAgentCooldown(d, "codex", until); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	result, err := walkOverPresets(t, d, taskSetDir, &out, "codex", "claude")
	if err != nil {
		t.Fatalf("runAgentFallbackWalk: %v", err)
	}
	if runner.calls != 1 {
		t.Fatalf("agent invocations = %d, want only the non-cooling preset invoked", runner.calls)
	}
	if !strings.HasPrefix(result.Agent, "claude") {
		t.Fatalf("answering agent = %q, want claude", result.Agent)
	}
	if !strings.Contains(result.Answer, "All good.") {
		t.Fatalf("answer = %q, want the second preset's document", result.Answer)
	}
	if len(result.Unavailable) != 1 {
		t.Fatalf("unavailable verdicts = %#v, want one for the cooling preset", result.Unavailable)
	}
	v := result.Unavailable[0]
	if v.Preset != "codex" || v.Kind != ProceedQuotaPause || v.Scope != ProceedScopePreset {
		t.Fatalf("verdict = %#v, want a preset-scoped quota pause for codex", v)
	}
	if !v.ResetAt.Equal(until) {
		t.Fatalf("verdict ResetAt = %s, want the cooldown's until %s", v.ResetAt, until)
	}
	if !strings.Contains(v.Reason, until.Format(time.RFC3339)) {
		t.Fatalf("verdict Reason = %q, want the cooldown's until named", v.Reason)
	}
	if !strings.Contains(out.String(), "quota-paused; trying next") {
		t.Fatalf("output = %q, want the cooling preset's fall-through named", out.String())
	}
}

// TestFallbackWalkInvokesAPresetWhoseCooldownElapsed keeps the skip time-healing:
// once the recorded until is in the past the preset is tried again, so a stale
// entry cannot retire an agent from verification for good.
func TestFallbackWalkInvokesAPresetWhoseCooldownElapsed(t *testing.T) {
	taskSetDir := t.TempDir()
	d, runner := reviewRunnerDeps(t, scriptedReviewRun{output: claudeReviewStream("## Naming\\nAll good.")})
	if err := updateAgentCooldown(d, "claude", time.Now().UTC().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	result, err := walkOverPresets(t, d, taskSetDir, &out, "claude")
	if err != nil {
		t.Fatalf("runAgentFallbackWalk: %v", err)
	}
	if runner.calls != 1 {
		t.Fatalf("agent invocations = %d, want the elapsed cooldown ignored", runner.calls)
	}
	if len(result.Unavailable) != 0 {
		t.Fatalf("unavailable verdicts = %#v, want none", result.Unavailable)
	}
}

// TestFallbackWalkReportsEveryPresetCoolingAsUnavailable: with the whole list
// cooling the walk invokes nothing and hands the caller a verdict per preset,
// which is what lets a role turn an exhausted list into its own quota pause.
func TestFallbackWalkReportsEveryPresetCoolingAsUnavailable(t *testing.T) {
	taskSetDir := t.TempDir()
	d, runner := reviewRunnerDeps(t)
	for _, preset := range []string{"codex", "claude"} {
		if err := updateAgentCooldown(d, preset, time.Now().UTC().Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}

	var out bytes.Buffer
	result, err := walkOverPresets(t, d, taskSetDir, &out, "codex", "claude")
	if err != nil {
		t.Fatalf("runAgentFallbackWalk: %v", err)
	}
	if runner.calls != 0 {
		t.Fatalf("agent invocations = %d, want none when every preset is cooling", runner.calls)
	}
	if result.Answer != "" || result.Agent != "" {
		t.Fatalf("result = %#v, want no answer", result)
	}
	if len(result.Unavailable) != 2 {
		t.Fatalf("unavailable verdicts = %#v, want one per cooling preset", result.Unavailable)
	}
}
