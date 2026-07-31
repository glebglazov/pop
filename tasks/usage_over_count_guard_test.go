package tasks

import (
	"testing"
)

func TestCheckUsageOverCountGuardPiInapplicable(t *testing.T) {
	events := piDeltaOverCountFixtureEvents()
	extracted := piTokenUsage(events)
	if err := checkUsageOverCountGuard("pi", events, extracted); err != nil {
		t.Fatalf("pi guard should be inapplicable: %v", err)
	}
}

func TestCheckUsageOverCountGuardCursorInapplicable(t *testing.T) {
	events := []streamEventRecord{
		{Type: "event", AtMS: 100, Raw: `{"type":"result","subtype":"success","usage":{"inputTokens":10,"outputTokens":20}}`},
	}
	extracted := cursorTokenUsage(events)
	if err := checkUsageOverCountGuard("cursor", events, extracted); err != nil {
		t.Fatalf("cursor guard should be inapplicable: %v", err)
	}
}

func TestCheckUsageOverCountGuardClaudeInapplicable(t *testing.T) {
	events := []streamEventRecord{
		{Type: "event", AtMS: 100, Raw: `{"type":"result","usage":{"input_tokens":10,"output_tokens":20}}`},
	}
	extracted := claudeTokenUsage(events)
	if err := checkUsageOverCountGuard("claude", events, extracted); err != nil {
		t.Fatal(err)
	}
}

func TestCheckUsageOverCountGuardUnknownAdapterInapplicable(t *testing.T) {
	if err := checkUsageOverCountGuard("codex", nil, TokenUsage{}); err != nil {
		t.Fatal(err)
	}
}

func TestRunSpendAppliesOverCountGuard(t *testing.T) {
	events := piDeltaOverCountFixtureEvents()
	run := capturedRun{
		meta:   capturedRunMeta{Agent: "pi"},
		events: events,
	}
	spend, err := runSpend(run)
	if err != nil {
		t.Fatal(err)
	}
	if spend.Tokens.Input != 180 {
		t.Fatalf("tokens = %+v", spend.Tokens)
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
