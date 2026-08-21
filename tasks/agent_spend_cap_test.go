package tasks

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// codexSpendCapMessage is the refusal as codex printed it (2026-08-21): a
// billing ceiling somebody else set, with the only instruction that lifts it.
const codexSpendCapMessage = "You hit your spend cap set by the owner of your workspace. Ask an owner to increase your spend cap to continue."

// codexSpendCapStream is the capture a spend-capped codex run leaves behind:
// the turn aborts on both diagnostic channels and the process exits 1.
func codexSpendCapStream() string {
	return `{"type":"thread.started","thread_id":"t"}` + "\n" +
		`{"type":"turn.started"}` + "\n" +
		`{"type":"error","message":"` + codexSpendCapMessage + `"}` + "\n" +
		`{"type":"turn.failed","error":{"message":"` + codexSpendCapMessage + `"}}`
}

const codexUsageLimitMessage = "You've hit your usage limit. Upgrade to Pro or try again at 2:28 AM."

// TestSpendCapIsItsOwnVerdictFlavour separates the three stops a refused account
// can be in. A usage limit refills at a stated time, a spend cap waits on a
// person but pop still dates it, and an authentication failure waits on a person
// with nothing to date — so they may share neither kind nor recovery.
func TestSpendCapIsItsOwnVerdictFlavour(t *testing.T) {
	t.Parallel()
	capped := normalizeCodexJSONL(codexSpendCapStream()).ProceedVerdict
	if capped == nil {
		t.Fatal("spend-cap refusal produced no proceed verdict")
	}
	if capped.Kind != ProceedSpendCap {
		t.Fatalf("kind = %q, want %q", capped.Kind, ProceedSpendCap)
	}
	if capped.Scope != ProceedScopePreset {
		t.Fatalf("scope = %q, want preset so the walk continues past it", capped.Scope)
	}
	if _, ok := capped.TimeHealing(); !ok {
		t.Fatalf("verdict = %#v, want time-healing so it enters the recovery wait", *capped)
	}
	if capped.ConsumesAttempt {
		t.Fatal("a capped account says nothing about the task, so it must not charge the retry cap")
	}

	limited := normalizeCodexJSONL(`{"type":"turn.failed","error":{"message":"` + codexUsageLimitMessage + `"}}`).ProceedVerdict
	if limited == nil || limited.Kind != ProceedQuotaPause {
		t.Fatalf("usage limit = %#v, want a quota pause, not a spend cap", limited)
	}
	if auth := NewAuthFailureVerdict("codex", "logged out"); auth.Kind == capped.Kind || auth.Recovery == capped.Recovery {
		t.Fatalf("spend cap %#v must differ from an auth failure %#v in kind and recovery", *capped, auth)
	}
}

// TestSpendCapReasonNamesWhatWouldLiftIt keeps the provider's own sentence on
// the verdict: the reason a human reads has to say the spending was capped and
// that an owner raising it is the way out.
func TestSpendCapReasonNamesWhatWouldLiftIt(t *testing.T) {
	t.Parallel()
	v := normalizeCodexJSONL(codexSpendCapStream()).ProceedVerdict
	if v == nil {
		t.Fatal("no proceed verdict")
	}
	if v.Reason != codexSpendCapMessage {
		t.Fatalf("reason = %q, want the provider's own sentence %q", v.Reason, codexSpendCapMessage)
	}
}

// TestSpendCapCoolsForPopsOwnHour pins where the hour comes from: nothing in the
// refusal names a reset, the adapter parses none, and pop supplies one hour from
// the moment it was told. Hitting the cap again starts another hour.
func TestSpendCapCoolsForPopsOwnHour(t *testing.T) {
	t.Parallel()
	v := normalizeCodexJSONL(codexSpendCapStream()).ProceedVerdict.WithPreset("codex")
	now := time.Date(2026, 8, 21, 14, 0, 0, 0, time.UTC)
	if parsed := agentQuotaResetAt("codex", v.Reason, now); !parsed.IsZero() {
		t.Fatalf("adapter parsed %s out of a spend cap; the hour must be pop's own", parsed)
	}

	first := resolveProceedResetAt(v, now)
	if want := now.Add(time.Hour); !first.ResetAt.Equal(want) {
		t.Fatalf("ResetAt = %s, want %s", first.ResetAt, want)
	}

	// The same cap, met again once the first hour has elapsed.
	later := first.ResetAt.Add(time.Minute)
	second := resolveProceedResetAt(v, later)
	if want := later.Add(time.Hour); !second.ResetAt.Equal(want) {
		t.Fatalf("second cap ResetAt = %s, want a fresh hour at %s", second.ResetAt, want)
	}
}

// TestSpendCapAdvancesTheWalkAndCoolsThePreset drives a spend cap through the
// shared fallback walk verify and review both run on: the capped preset hands
// the turn to the next agent instead of ending anything, is recorded as cooling
// for pop's hour, and is skipped without invocation while it cools.
func TestSpendCapAdvancesTheWalkAndCoolsThePreset(t *testing.T) {
	taskSetDir := t.TempDir()
	d, runner := reviewRunnerDeps(t,
		scriptedReviewRun{output: codexSpendCapStream(), exitCode: 1},
		scriptedReviewRun{output: claudeReviewStream("## Naming\\nAll good.")},
	)

	var out bytes.Buffer
	result, err := walkOverPresets(t, d, taskSetDir, &out, "codex", "claude")
	if err != nil {
		t.Fatalf("runAgentFallbackWalk: %v", err)
	}
	if !strings.Contains(result.Answer, "All good.") {
		t.Fatalf("answer = %q, want the next agent's document", result.Answer)
	}
	if runner.calls != 2 {
		t.Fatalf("agent invocations = %d, want the capped preset tried once and handed on", runner.calls)
	}
	if len(result.Unavailable) != 1 || result.Unavailable[0].Kind != ProceedSpendCap {
		t.Fatalf("unavailable = %#v, want one spend cap", result.Unavailable)
	}
	if !strings.Contains(out.String(), "stopped by a spend cap") {
		t.Fatalf("output = %q, want the spend cap named as pop's own wait", out.String())
	}

	cooldowns, err := readAgentCooldowns(d)
	if err != nil {
		t.Fatalf("read cooldowns: %v", err)
	}
	until, cooling := cooldowns["codex"]
	if !cooling {
		t.Fatalf("cooldowns = %#v, want codex cooling", cooldowns)
	}
	if wait := time.Until(until.ExhaustedUntil); wait < 55*time.Minute || wait > 70*time.Minute {
		t.Fatalf("codex cools for %s, want about pop's hour", wait)
	}

	// While it cools nothing invokes it again, on this role or any other: the
	// cooldown store is machine-global.
	before := runner.calls
	second, err := walkOverPresets(t, d, taskSetDir, &out, "codex")
	if err != nil {
		t.Fatalf("second walk: %v", err)
	}
	if runner.calls != before {
		t.Fatalf("agent invocations = %d, want the cooling preset skipped before spawn", runner.calls)
	}
	if len(second.Unavailable) != 1 || second.Answer != "" {
		t.Fatalf("second walk = %#v, want the cooling preset reported unavailable", second)
	}
}

// TestImplementSpendCapFallsThroughToTheNextAgent is the drain the bug was found
// in: codex first, capped; kimi-shaped healthy agent second. The set finishes,
// the capped preset is invoked once rather than three times, and it is left
// cooling for every group that reads the store.
func TestImplementSpendCapFallsThroughToTheNextAgent(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	codexCount := installSpendCappedCodexAgent(t, env.root)
	installAgentShim(t, env.root, "claude", `#!/bin/sh
if [ "$1" = auth ] && [ "$2" = status ]; then printf '{"loggedIn":true}\n'; exit 0; fi
TASK=$(cat "$(printf '%s' "$*" | sed -n 's|.*Read the file \([^ ]*\) in full:.*|\1|p' | head -1)" | sed -n 's|^.*You are implementing the task at: ||p' | head -1 | awk '{print $1}')
if [ -n "$TASK" ] && [ -f "$TASK" ]; then sed -i '' 's/- \[ \]/- [x]/g' "$TASK" 2>/dev/null || sed -i 's/- \[ \]/- [x]/g' "$TASK"; fi
printf 'SUMMARY_START\nclaude done\nSUMMARY_END\nTASK_COMPLETE\n'
`)

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, "", &buf)
	opts.AgentPresets = []string{"codex", "claude"}
	opts.AgentExplicit = true
	opts.MaxTries = 3

	d := env.deps()
	result, err := RunTaskSetWith(d, nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !result.TaskSetDone || len(result.Completed) != 1 {
		t.Fatalf("result = %#v, want the next agent to have finished the work", result)
	}
	if got := strings.TrimSpace(readFileString(t, codexCount)); got != "1" {
		t.Fatalf("codex attempts = %q, want 1 — a cap is the same answer every retry", got)
	}
	if !strings.Contains(buf.String(), "stopped by a spend cap") {
		t.Fatalf("output missing the spend-cap fall-through:\n%s", buf.String())
	}

	cooldowns, err := readAgentCooldowns(d)
	if err != nil {
		t.Fatalf("read cooldowns: %v", err)
	}
	until, cooling := cooldowns["codex"]
	if !cooling {
		t.Fatalf("cooldowns = %#v, want codex cooling after the cap", cooldowns)
	}
	if wait := time.Until(until.ExhaustedUntil); wait < 55*time.Minute || wait > 70*time.Minute {
		t.Fatalf("codex cools for %s, want about pop's hour", wait)
	}
	// This is the fall-through that costs nothing: the cap was abandoned at the
	// first refusal rather than spent, so the night's severe listing stays empty
	// (ADR-0231).
	if caps, err := AllSpentRetryCaps(d); err != nil {
		t.Fatalf("AllSpentRetryCaps: %v", err)
	} else if len(caps) != 0 {
		t.Fatalf("spent caps = %#v, want none for a refusal that spent no budget", caps)
	}

	assertTaskDone(t, env.execFixture(), "01-a")
}

// installSpendCappedCodexAgent stubs a codex whose every attempt is refused by a
// workspace spend cap, counting its invocations so a test can prove the retry
// cap was abandoned rather than spent.
func installSpendCappedCodexAgent(t *testing.T, root string) string {
	t.Helper()
	// ADR-0145: PATH stub — callers stay serial deliberately.
	dir := filepath.Join(root, ".agent-bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	count := filepath.Join(dir, "codex.count")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = login ]; then exit 0; fi\n" +
		"printf 'x' >> " + shellQuote(count) + "\n" +
		"wc -c < " + shellQuote(count) + " | tr -d ' ' > " + shellQuote(count+".n") + "\n" +
		"printf '%s\\n' " + shellQuote(codexSpendCapStream()) + "\n" +
		"exit 1\n"
	installAgentShim(t, root, "codex", script)
	return count + ".n"
}
