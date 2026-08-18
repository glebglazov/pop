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
		{"codex", "codex.events.jsonl.gz", 13},
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

	codexEvents := loadTurnFixture(t, "codex.events.jsonl.gz")
	codexPeak := extractPeakInput("codex", codexEvents)
	if !codexPeak.HasPeak || codexPeak.Tokens != 77726 {
		t.Fatalf("codex peak = %+v, want 77726 reported", codexPeak)
	}
}

func TestCodexTurnCountCountsSplicedTokenCounts(t *testing.T) {
	spliced := []streamEventRecord{
		{Type: "event", AtMS: 1, Raw: `{"type":"turn.started"}`},
		{Type: "event", AtMS: 2, Raw: `{"type":"token_count","info":{"last_token_usage":{"input_tokens":10}}}`},
		{Type: "event", AtMS: 3, Raw: `{"type":"token_count","info":{"last_token_usage":{"input_tokens":20}}}`},
		{Type: "event", AtMS: 4, Raw: `{"type":"token_count","info":{"last_token_usage":{"input_tokens":30}}}`},
		{Type: "event", AtMS: 5, Raw: `{"type":"turn.completed","usage":{"input_tokens":30}}`},
	}
	if tc := codexTurnCount(spliced); tc.Count != 3 || !tc.HasTurn {
		t.Fatalf("turns = %+v, want 3 reported", tc)
	}

	// An unspliced run carries codex's one turn.completed per headless run; the
	// retired rule read that as a single turn, which was wrong for every run.
	unspliced := []streamEventRecord{
		{Type: "event", AtMS: 1, Raw: `{"type":"turn.started"}`},
		{Type: "event", AtMS: 2, Raw: `{"type":"turn.completed","usage":{"input_tokens":30}}`},
	}
	if tc := codexTurnCount(unspliced); tc.HasTurn {
		t.Fatalf("unspliced turns = %+v, want turn-blind", tc)
	}
}

func TestCodexPeakInputTakesMaxPerCallContext(t *testing.T) {
	// cached_input_tokens is a breakdown of input_tokens on codex's wire, so the
	// per-call figure adds only cache_write_input_tokens (ADR-0219).
	events := []streamEventRecord{
		{Type: "event", AtMS: 1, Raw: `{"type":"token_count","info":{"last_token_usage":{"input_tokens":200,"cached_input_tokens":150,"cache_write_input_tokens":0}}}`},
		{Type: "event", AtMS: 2, Raw: `{"type":"token_count","info":{"last_token_usage":{"input_tokens":300,"cached_input_tokens":290,"cache_write_input_tokens":7}}}`},
		{Type: "event", AtMS: 3, Raw: `{"type":"token_count","info":{"last_token_usage":{"input_tokens":100,"cached_input_tokens":0,"cache_write_input_tokens":0}}}`},
		{Type: "event", AtMS: 4, Raw: `{"type":"turn.completed","usage":{"input_tokens":600,"cached_input_tokens":440,"output_tokens":9}}`},
	}
	peak := codexPeakInput(events)
	if !peak.HasPeak || peak.Tokens != 307 {
		t.Fatalf("peak = %+v, want 307 reported", peak)
	}

	unspliced := []streamEventRecord{events[3]}
	if blind := codexPeakInput(unspliced); blind.HasPeak {
		t.Fatalf("unspliced peak = %+v, want peak-blind", blind)
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

	for _, agent := range []string{"kimi", "opencode"} {
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
