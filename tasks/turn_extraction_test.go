package tasks

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func loadTurnFixture(t *testing.T, name string) []streamEventRecord {
	t.Helper()
	path := filepath.Join("testdata", "streams", name)
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip %s: %v", path, err)
	}
	defer zr.Close()
	dec := json.NewDecoder(zr)
	var events []streamEventRecord
	for dec.More() {
		var ev streamEventRecord
		if err := dec.Decode(&ev); err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		events = append(events, ev)
	}
	return events
}

func TestClaudeTurnCountDedupesAssistantMessageID(t *testing.T) {
	events := []streamEventRecord{
		{Type: "event", AtMS: 1, Raw: `{"type":"assistant","message":{"id":"msg_1","role":"assistant"}}`},
		{Type: "event", AtMS: 2, Raw: `{"type":"assistant","message":{"id":"msg_1","role":"assistant"}}`},
		{Type: "event", AtMS: 3, Raw: `{"type":"assistant","message":{"id":"msg_2","role":"assistant"}}`},
	}
	tc := claudeTurnCount(events)
	if tc.Count != 2 || !tc.HasTurn {
		t.Fatalf("turns = %+v, want 2 reported", tc)
	}
}

func TestCursorTurnCountDistinctModelCallID(t *testing.T) {
	events := []streamEventRecord{
		{Type: "event", AtMS: 1, Raw: `{"type":"assistant","model_call_id":"call-a"}`},
		{Type: "event", AtMS: 2, Raw: `{"type":"tool_call","model_call_id":"call-a"}`},
		{Type: "event", AtMS: 3, Raw: `{"type":"tool_call","model_call_id":"call-a"}`},
		{Type: "event", AtMS: 4, Raw: `{"type":"assistant","model_call_id":"call-b"}`},
	}
	tc := cursorTurnCount(events)
	if tc.Count != 2 || !tc.HasTurn {
		t.Fatalf("turns = %+v, want 2 reported", tc)
	}
}

func TestPiTurnCountUsesTurnEndAssistantOnly(t *testing.T) {
	events := []streamEventRecord{
		{Type: "event", AtMS: 1, Raw: `{"type":"turn_end","message":{"role":"assistant"}}`},
		{Type: "event", AtMS: 2, Raw: `{"type":"message_end","message":{"role":"assistant"}}`},
		{Type: "event", AtMS: 3, Raw: `{"type":"turn_end","message":{"role":"toolResult"}}`},
		{Type: "event", AtMS: 4, Raw: `{"type":"turn_end","message":{"role":"assistant"}}`},
	}
	tc := piTurnCount(events)
	if tc.Count != 2 || !tc.HasTurn {
		t.Fatalf("turns = %+v, want 2 reported", tc)
	}
}

func TestClaudePeakInputSumsCacheFields(t *testing.T) {
	events := []streamEventRecord{
		{Type: "event", AtMS: 1, Raw: `{"type":"assistant","message":{"id":"a","usage":{"input_tokens":100,"cache_read_input_tokens":50,"cache_creation_input_tokens":5}}}`},
		{Type: "event", AtMS: 2, Raw: `{"type":"assistant","message":{"id":"b","usage":{"input_tokens":6,"cache_read_input_tokens":9000,"cache_creation_input_tokens":0}}}`},
	}
	peak := claudePeakInput(events)
	if !peak.HasPeak || peak.Tokens != 9006 {
		t.Fatalf("peak = %+v, want 9006 reported", peak)
	}
}

func TestPiPeakInputSumsCacheFields(t *testing.T) {
	events := []streamEventRecord{
		{Type: "event", AtMS: 1, Raw: `{"type":"turn_end","message":{"role":"assistant","usage":{"input":6,"cacheRead":100,"cacheWrite":4}}}`},
		{Type: "event", AtMS: 2, Raw: `{"type":"turn_end","message":{"role":"assistant","usage":{"input":1,"cacheRead":200,"cacheWrite":0}}}`},
	}
	peak := piPeakInput(events)
	if !peak.HasPeak || peak.Tokens != 201 {
		t.Fatalf("peak = %+v, want 201 reported", peak)
	}
}

func TestSampledRunTurnCounts(t *testing.T) {
	cases := []struct {
		agent    string
		fixture  string
		wantTurn int
	}{
		{"claude", "claude.events.jsonl.gz", 8},
		{"cursor", "cursor.events.jsonl.gz", 8},
		{"pi", "pi.events.jsonl.gz", 11},
		{"codex", "codex.events.jsonl.gz", 1},
	}
	for _, tc := range cases {
		t.Run(tc.agent, func(t *testing.T) {
			events := loadTurnFixture(t, tc.fixture)
			got := extractTurnCount(tc.agent, events)
			if got.Count != tc.wantTurn || !got.HasTurn {
				t.Fatalf("turns = %+v, want %d reported", got, tc.wantTurn)
			}
		})
	}
}

func TestSampledRunPeakInput(t *testing.T) {
	claudeEvents := loadTurnFixture(t, "claude.events.jsonl.gz")
	claudePeak := extractPeakInput("claude", claudeEvents)
	if !claudePeak.HasPeak || claudePeak.Tokens != 40038 {
		t.Fatalf("claude peak = %+v, want 40038 reported", claudePeak)
	}

	piEvents := loadTurnFixture(t, "pi.events.jsonl.gz")
	piPeak := extractPeakInput("pi", piEvents)
	if !piPeak.HasPeak || piPeak.Tokens != 11879 {
		t.Fatalf("pi peak = %+v, want 11879 reported", piPeak)
	}
}

func TestCodexTurnCountUsesTurnCompleted(t *testing.T) {
	events := []streamEventRecord{
		{Type: "event", AtMS: 1, Raw: `{"type":"turn.started"}`},
		{Type: "event", AtMS: 2, Raw: `{"type":"turn.completed"}`},
		{Type: "event", AtMS: 3, Raw: `{"type":"turn.started"}`},
		{Type: "event", AtMS: 4, Raw: `{"type":"turn.completed"}`},
	}
	tc := codexTurnCount(events)
	if tc.Count != 2 || !tc.HasTurn {
		t.Fatalf("turns = %+v, want 2 reported", tc)
	}
}

func TestClaudePartialCostReadsResultTotalCostUSD(t *testing.T) {
	events := []streamEventRecord{
		{Type: "event", AtMS: 1, Raw: `{"type":"result","total_cost_usd":0.42}`},
	}
	c := claudePartialCost(events)
	if !c.HasCost || c.Dollars != 0.42 {
		t.Fatalf("cost = %+v", c)
	}
}

func TestExtractTurnCountBlindForUnsupportedAdapters(t *testing.T) {
	events := []streamEventRecord{
		{Type: "event", AtMS: 1, Raw: `{"type":"assistant","message":{"id":"msg_1"}}`},
	}
	for _, agent := range []string{"kimi", "opencode"} {
		tc := extractTurnCount(agent, events)
		if tc.HasTurn {
			t.Fatalf("%s should be turn-blind, got %+v", agent, tc)
		}
	}

	reportedZero := extractTurnCount("claude", nil)
	if !reportedZero.HasTurn || reportedZero.Count != 0 {
		t.Fatalf("reported zero turns = %+v", reportedZero)
	}
}

func TestExtractPeakInputBlindForCursorAndUnsupportedAdapters(t *testing.T) {
	events := []streamEventRecord{
		{Type: "event", AtMS: 1, Raw: `{"type":"result","usage":{"inputTokens":73366,"outputTokens":1,"cacheReadTokens":220800,"cacheWriteTokens":0}}`},
	}
	cursorPeak := extractPeakInput("cursor", events)
	if cursorPeak.HasPeak {
		t.Fatalf("cursor should be peak-blind, got %+v", cursorPeak)
	}

	for _, agent := range []string{"codex", "kimi", "opencode"} {
		peak := extractPeakInput(agent, events)
		if peak.HasPeak {
			t.Fatalf("%s should be peak-blind, got %+v", agent, peak)
		}
	}

	reportedZero := extractPeakInput("claude", []streamEventRecord{
		{Type: "event", AtMS: 1, Raw: `{"type":"assistant","message":{"id":"a","usage":{"input_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}`},
	})
	if !reportedZero.HasPeak || reportedZero.Tokens != 0 {
		t.Fatalf("reported zero peak = %+v", reportedZero)
	}
}
