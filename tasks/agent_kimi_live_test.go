package tasks

import (
	"strings"
	"testing"
	"time"
)

// Sample lines below are taken verbatim from real kimi-code runs captured with
//
//	kimi -p '<prompt>' --output-format stream-json
//
// in a scratch repo holding hello.txt ("hello from pop") and two.txt (two
// lines). Two prompts were run: one read-only ("Read hello.txt … reply with its
// exact contents"), one mixing Bash and Read. The retry (`turn.step.retrying`)
// and version (`system.version`) meta shapes are the object literals compiled
// into that binary's stream-json writer (PromptJsonWriter in
// src/cli/prompt-render.ts) — the local runs never hit a provider retry.
//
// The vocabulary is small and fully OpenAI-shaped: assistant lines (prose,
// tool_calls, or both), tool result lines, and meta lines. There is no init,
// model, or result event, and thinking never reaches this stream.

const (
	kimiSampleToolCallLine   = `{"role":"assistant","tool_calls":[{"type":"function","id":"Read_0","function":{"name":"Read","arguments":"{\"path\":\"hello.txt\"}"}}]}`
	kimiSampleToolResultLine = `{"role":"tool","tool_call_id":"Read_0","content":"1\thello from pop"}`
	kimiSampleProseLine      = `{"role":"assistant","content":"hello from pop"}`
	kimiSampleResumeHintLine = `{"role":"meta","type":"session.resume_hint","session_id":"session_9a205820-83c9-457f-a3bd-7cc88008356b","command":"kimi -r session_9a205820-83c9-457f-a3bd-7cc88008356b","content":"To resume this session: kimi -r session_9a205820-83c9-457f-a3bd-7cc88008356b"}`
	kimiSampleRetryLine      = `{"role":"meta","type":"turn.step.retrying","failed_attempt":1,"next_attempt":2,"max_attempts":5,"delay_ms":2000,"error_name":"ProviderOverloadedError","error_message":"engine is currently overloaded","status_code":429}`
	kimiSampleVersionLine    = `{"role":"meta","type":"system.version","version":"0.9.12"}`
	kimiSampleTwoToolsLine   = `{"role":"assistant","tool_calls":[{"type":"function","id":"Bash_0","function":{"name":"Bash","arguments":"{\"command\":\"wc -l two.txt\"}"}},{"type":"function","id":"Read_1","function":{"name":"Read","arguments":"{\"path\":\"hello.txt\"}"}}]}`
	kimiSampleSummaryLine    = `{"role":"assistant","content":"Summary: ` + "`two.txt`" + ` has 2 lines, and ` + "`hello.txt`" + ` contains the single line \"hello from pop\"."}`
)

func TestKimiLineRendererToolCallTick(t *testing.T) {
	render := kimiLineRenderer(false)
	got, handled := render([]byte(kimiSampleToolCallLine))
	if !handled {
		t.Fatal("assistant tool_calls line should be handled")
	}
	if got != "→ Read hello.txt\n" {
		t.Fatalf("got %q, want %q", got, "→ Read hello.txt\n")
	}
}

func TestKimiLineRendererTicksEveryToolCallOnOneLine(t *testing.T) {
	render := kimiLineRenderer(false)
	got, _ := render([]byte(kimiSampleTwoToolsLine))
	want := "→ Bash wc -l two.txt\n→ Read hello.txt\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestKimiLineRendererProse(t *testing.T) {
	render := kimiLineRenderer(false)
	got, handled := render([]byte(kimiSampleProseLine))
	if !handled {
		t.Fatal("assistant content line should be handled")
	}
	if got != "hello from pop\n" {
		t.Fatalf("got %q, want %q", got, "hello from pop\n")
	}
}

func TestKimiLineRendererProsePrecedesTicksOnOneLine(t *testing.T) {
	render := kimiLineRenderer(false)
	// kimi flushes an assistant message as one line carrying whatever the step
	// produced, so prose and the tool calls it introduces can arrive together.
	line := `{"role":"assistant","content":"Let me check the file.","tool_calls":[{"type":"function","id":"Read_0","function":{"name":"Read","arguments":"{\"path\":\"hello.txt\"}"}}]}`
	got, _ := render([]byte(line))
	want := "Let me check the file.\n→ Read hello.txt\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestKimiLineRendererSkipsToolResultsAndMeta(t *testing.T) {
	render := kimiLineRenderer(false)
	for _, line := range []string{
		kimiSampleToolResultLine,
		kimiSampleResumeHintLine,
		kimiSampleRetryLine,
		kimiSampleVersionLine,
	} {
		got, handled := render([]byte(line))
		if !handled {
			t.Fatalf("structured line %q should be handled", line)
		}
		if got != "" {
			t.Fatalf("line %q should render nothing, got %q", line, got)
		}
	}
}

func TestKimiLineRendererPassesPlainStderrThrough(t *testing.T) {
	render := kimiLineRenderer(false)
	// kimi's Bash tool echoes command output to stderr, which shares the
	// capture with stream-json.
	got, handled := render([]byte("       2 two.txt"))
	if handled {
		t.Fatal("plain stderr line should be unhandled so the writer passes it through raw")
	}
	if got != "" {
		t.Fatalf("unhandled line should render nothing, got %q", got)
	}
}

func TestKimiLineRendererBareTickWhenArgumentsUnrecognized(t *testing.T) {
	render := kimiLineRenderer(false)
	for _, args := range []string{`{}`, `{\"replace_all\":true}`, `{\"path\": `} {
		line := `{"role":"assistant","tool_calls":[{"type":"function","id":"X_0","function":{"name":"Mystery","arguments":"` + args + `"}}]}`
		got, _ := render([]byte(line))
		if got != "→ Mystery\n" {
			t.Fatalf("arguments %q: got %q, want %q", args, got, "→ Mystery\n")
		}
	}
}

func TestKimiLineRendererTruncatesHint(t *testing.T) {
	render := kimiLineRenderer(false)
	long := strings.Repeat("x", 200)
	line := `{"role":"assistant","tool_calls":[{"type":"function","id":"Bash_0","function":{"name":"Bash","arguments":"{\"command\":\"` + long + `\"}"}}]}`
	got, _ := render([]byte(line))
	want := "→ Bash " + strings.Repeat("x", 77) + "...\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestKimiLineRendererColorStylesToolTick(t *testing.T) {
	render := kimiLineRenderer(true)
	got, _ := render([]byte(kimiSampleToolCallLine))
	want := ansiDim + "→ Read hello.txt" + ansiReset + "\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestNormalizeKimiStreamJSONTakesLastAssistantProse(t *testing.T) {
	raw := strings.Join([]string{
		kimiSampleTwoToolsLine,
		kimiSampleToolResultLine,
		kimiSampleSummaryLine,
		kimiSampleResumeHintLine,
	}, "\n") + "\n"
	result := NormalizeAgentOutput(AgentOutputKimiStreamJSON, raw)
	want := "Summary: `two.txt` has 2 lines, and `hello.txt` contains the single line \"hello from pop\".\n"
	if result.Output != want {
		t.Fatalf("output = %q, want %q", result.Output, want)
	}
	if result.Unavailability != nil {
		t.Fatalf("unexpected unavailability: %#v", result.Unavailability)
	}
}

func TestNormalizeKimiStreamJSONSurvivesRetryAndStderrNoise(t *testing.T) {
	raw := strings.Join([]string{
		kimiSampleVersionLine,
		kimiSampleRetryLine,
		kimiSampleToolCallLine,
		"       2 two.txt",
		kimiSampleToolResultLine,
		kimiSampleProseLine,
		kimiSampleResumeHintLine,
		"To resume this session: kimi -r session_9a205820",
	}, "\n") + "\n"
	result := NormalizeAgentOutput(AgentOutputKimiStreamJSON, raw)
	if result.Output != "hello from pop\n" {
		t.Fatalf("output = %q, want %q", result.Output, "hello from pop\n")
	}
}

func TestNormalizeKimiStreamJSONFailureKeepsRawCapture(t *testing.T) {
	// kimi has no result event: a failed run is exit code plus stderr, so the
	// raw capture stays what the completion contract assesses.
	raw := "Error: provider request failed after 5 attempts\n"
	result := NormalizeAgentOutput(AgentOutputKimiStreamJSON, raw)
	if result.Output != raw {
		t.Fatalf("output = %q, want raw capture %q", result.Output, raw)
	}
}

func TestKimiLiveRenderWriterRendersWholeCapture(t *testing.T) {
	var live, capture strings.Builder
	now := time.Unix(0, 0)
	clock := func() time.Time { return now }
	w := newLiveRenderWriter(&live, &capture, kimiLineRenderer(false), clock)
	raw := strings.Join([]string{
		kimiSampleTwoToolsLine,
		"       2 two.txt",
		kimiSampleToolResultLine,
		kimiSampleProseLine,
		kimiSampleResumeHintLine,
	}, "\n") + "\n"
	if _, err := w.Write([]byte(raw)); err != nil {
		t.Fatal(err)
	}
	w.Flush()
	// Both ticks of one assistant line share its gap marker; the plain stderr
	// line passes through unprefixed.
	want := strings.Join([]string{
		" +0.0s  → Bash wc -l two.txt",
		"        → Read hello.txt",
		"       2 two.txt",
		" +0.0s  hello from pop",
		"",
	}, "\n")
	if live.String() != want {
		t.Fatalf("live render =\n%q\nwant\n%q", live.String(), want)
	}
	if capture.String() != raw {
		t.Fatal("capture must hold the raw stream unchanged")
	}
}

func TestKimiQuotaSignalsPauseWithADRBackoffs(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		line    string
		backoff time.Duration
	}{
		{
			name:    "period",
			line:    "Error: You have reached the usage limit for this period. Please try again later.",
			backoff: time.Hour,
		},
		{
			name:    "billing cycle",
			line:    "Error: You have reached the usage limit for this billing cycle.",
			backoff: 24 * time.Hour,
		},
		{
			name:    "monthly",
			line:    "Error: monthly usage limit exceeded for your plan.",
			backoff: 7 * 24 * time.Hour,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := strings.Join([]string{kimiSampleToolCallLine, tt.line}, "\n") + "\n"
			result := NormalizeAgentOutput(AgentOutputKimiStreamJSON, raw)
			if result.Unavailability == nil {
				t.Fatal("expected an agent quota pause")
			}
			if result.Unavailability.Reason != tt.line {
				t.Fatalf("reason = %q, want the whole diagnostic line %q", result.Unavailability.Reason, tt.line)
			}
			want := now.Add(tt.backoff).Add(quotaAssuranceOffset)
			if got := agentQuotaResetAt("kimi", result.Unavailability.Reason, now); !got.Equal(want) {
				t.Fatalf("reset = %s, want %s", got, want)
			}
		})
	}
}

func TestKimiTransientOverloadNeverPauses(t *testing.T) {
	for _, line := range []string{
		"Error: engine is currently overloaded, please try again later",
		"429 too many requests",
		kimiSampleRetryLine,
		"Error: request failed with status 500",
	} {
		t.Run(line, func(t *testing.T) {
			raw := strings.Join([]string{kimiSampleProseLine, line}, "\n") + "\n"
			result := NormalizeAgentOutput(AgentOutputKimiStreamJSON, raw)
			if result.Unavailability != nil {
				t.Fatalf("unexpected quota pause: %#v", result.Unavailability)
			}
			if got := kimiQuotaResetAt(line, time.Now()); !got.IsZero() {
				t.Fatalf("reset = %s, want zero time", got)
			}
		})
	}
}
