package tasks

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCheckUsageOverCountGuardPiDeltaOverCountFailsLoudly(t *testing.T) {
	events := piDeltaOverCountFixtureEvents()

	// Correct extraction passes the guard.
	correct := piTokenUsage(events)
	if status, err := checkUsageOverCountGuard("pi", events, correct); err != nil {
		t.Fatalf("correct extraction: %v", err)
	} else if status != OverCountGuardOK {
		t.Fatalf("correct extraction guard = %v, want OK", status)
	}

	// Summing message_update deltas is the ~4× failure mode ADR-0160 guards.
	wrong := piTokenUsageSumDeltas(events)
	if wrong.Input <= correct.Input {
		t.Fatalf("fixture must over-count input: wrong=%d correct=%d", wrong.Input, correct.Input)
	}
	_, err := checkUsageOverCountGuard("pi", events, wrong)
	if err == nil {
		t.Fatal("expected over-count guard error for delta sum")
	}
	if !strings.Contains(err.Error(), "usage over-count guard") {
		t.Fatalf("error = %q, want loud guard failure", err)
	}
}

func TestCheckUsageOverCountGuardCursorPassesWhenExtractedMatchesTerminal(t *testing.T) {
	events := []streamEventRecord{
		{Type: "event", AtMS: 100, Raw: `{"type":"result","subtype":"success","usage":{"inputTokens":10,"outputTokens":20}}`},
	}
	extracted := cursorTokenUsage(events)
	status, err := checkUsageOverCountGuard("cursor", events, extracted)
	if err != nil {
		t.Fatal(err)
	}
	if status != OverCountGuardOK {
		t.Fatalf("guard status = %v, want OK", status)
	}
}

func TestCheckUsageOverCountGuardCursorRejectsSummedResults(t *testing.T) {
	events := []streamEventRecord{
		{Type: "event", AtMS: 100, Raw: `{"type":"result","subtype":"success","usage":{"inputTokens":10,"outputTokens":20}}`},
		{Type: "event", AtMS: 200, Raw: `{"type":"result","subtype":"success","usage":{"inputTokens":100,"outputTokens":200}}`},
	}
	// A mistaken accumulate across result events exceeds the terminal total.
	wrong := cursorTokenUsageSumResults(events)
	if _, err := checkUsageOverCountGuard("cursor", events, wrong); err == nil {
		t.Fatal("expected over-count guard error for summed cursor results")
	}
}

func TestCheckUsageOverCountGuardClaudeInapplicable(t *testing.T) {
	events := []streamEventRecord{
		{Type: "event", AtMS: 100, Raw: `{"type":"result","usage":{"input_tokens":10,"output_tokens":20}}`},
	}
	extracted := claudeTokenUsage(events)
	status, err := checkUsageOverCountGuard("claude", events, extracted)
	if err != nil {
		t.Fatal(err)
	}
	if status != OverCountGuardInapplicable {
		t.Fatalf("guard status = %v, want Inapplicable", status)
	}
}

func TestCheckUsageOverCountGuardUnknownAdapterInapplicable(t *testing.T) {
	status, err := checkUsageOverCountGuard("codex", nil, TokenUsage{})
	if err != nil {
		t.Fatal(err)
	}
	if status != OverCountGuardInapplicable {
		t.Fatalf("guard status = %v, want Inapplicable", status)
	}
}

func TestRunSpendTokensAppliesOverCountGuard(t *testing.T) {
	events := piDeltaOverCountFixtureEvents()
	run := capturedRun{
		meta: capturedRunMeta{Agent: "pi"},
		events: events,
	}
	tokens, status, err := runSpendTokens(run)
	if err != nil {
		t.Fatal(err)
	}
	if status != OverCountGuardOK {
		t.Fatalf("guard status = %v, want OK", status)
	}
	if tokens.Input != 180 {
		t.Fatalf("tokens = %+v", tokens)
	}
}

// piDeltaOverCountFixtureEvents returns a pi stream where many message_update
// deltas carry cumulative usage. Summing those deltas over-counts; summing
// message_end only is correct.
func piDeltaOverCountFixtureEvents() []streamEventRecord {
	var events []streamEventRecord
	events = append(events, streamEventRecord{
		Type: "event", AtMS: 1,
		Raw: `{"type":"message_start","message":{"role":"assistant","usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0}}}`,
	})
	for i := 0; i < 50; i++ {
		events = append(events, streamEventRecord{
			Type: "event", AtMS: int64(10 + i),
			Raw: `{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"x"},"message":{"role":"assistant","usage":{"input":100,"output":20,"cacheRead":50,"cacheWrite":5}}}`,
		})
	}
	events = append(events,
		streamEventRecord{
			Type: "event", AtMS: 100,
			Raw: `{"type":"message_end","message":{"role":"assistant","usage":{"input":100,"output":20,"cacheRead":50,"cacheWrite":5},"stopReason":"toolUse"}}`,
		},
		streamEventRecord{
			Type: "event", AtMS: 200,
			Raw: `{"type":"message_end","message":{"role":"toolResult","toolName":"read"}}`,
		},
		streamEventRecord{
			Type: "event", AtMS: 300,
			Raw: `{"type":"message_end","message":{"role":"assistant","usage":{"input":80,"output":40,"cacheRead":60,"cacheWrite":0},"stopReason":"stop"}}`,
		},
	)
	return events
}

// piTokenUsageSumDeltas is the mistaken extraction rule that sums every
// message_update cumulative block — the regression ADR-0160 guards against.
func piTokenUsageSumDeltas(events []streamEventRecord) TokenUsage {
	var u TokenUsage
	for _, ev := range events {
		var event struct {
			Type    string `json:"type"`
			Message struct {
				Usage *struct {
					Input      *int64 `json:"input"`
					Output     *int64 `json:"output"`
					CacheRead  *int64 `json:"cacheRead"`
					CacheWrite *int64 `json:"cacheWrite"`
				} `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(ev.Raw), &event); err != nil {
			continue
		}
		if event.Type != "message_update" || event.Message.Usage == nil {
			continue
		}
		usage := event.Message.Usage
		if v := usage.Input; v != nil {
			u.Input += *v
			u.HasInput = true
		}
		if v := usage.Output; v != nil {
			u.Output += *v
			u.HasOutput = true
		}
		if v := usage.CacheRead; v != nil {
			u.CacheRead += *v
			u.HasCacheRead = true
		}
		if v := usage.CacheWrite; v != nil {
			u.CacheWrite += *v
			u.HasCacheWrite = true
		}
	}
	return u
}

// cursorTokenUsageSumResults is a mistaken accumulate rule for cursor.
func cursorTokenUsageSumResults(events []streamEventRecord) TokenUsage {
	var u TokenUsage
	for _, ev := range events {
		var event struct {
			Type  string `json:"type"`
			Usage *struct {
				InputTokens      *int64 `json:"inputTokens"`
				OutputTokens     *int64 `json:"outputTokens"`
				CacheReadTokens  *int64 `json:"cacheReadTokens"`
				CacheWriteTokens *int64 `json:"cacheWriteTokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(ev.Raw), &event); err != nil {
			continue
		}
		if event.Type != "result" || event.Usage == nil {
			continue
		}
		if v := event.Usage.InputTokens; v != nil {
			u.Input += *v
			u.HasInput = true
		}
		if v := event.Usage.OutputTokens; v != nil {
			u.Output += *v
			u.HasOutput = true
		}
		if v := event.Usage.CacheReadTokens; v != nil {
			u.CacheRead += *v
			u.HasCacheRead = true
		}
		if v := event.Usage.CacheWriteTokens; v != nil {
			u.CacheWrite += *v
			u.HasCacheWrite = true
		}
	}
	return u
}
