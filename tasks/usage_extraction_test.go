package tasks

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestClaudeTokenUsageSumsAPIUsageBlocks(t *testing.T) {
	events := []streamEventRecord{
		{Type: "event", AtMS: 5, Raw: `{"type":"system","subtype":"init"}`},
		{Type: "event", AtMS: 100, Raw: `{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}`},
		{Type: "event", AtMS: 200, Raw: `{"type":"result","subtype":"success","result":"ok","usage":{"input_tokens":100,"output_tokens":50,"cache_read_input_tokens":10,"cache_creation_input_tokens":5}}`},
		{Type: "event", AtMS: 300, Raw: `{"type":"result","subtype":"success","usage":{"input_tokens":25,"output_tokens":75}}`},
	}
	u := claudeTokenUsage(events)
	if u.Input != 125 || !u.HasInput {
		t.Fatalf("input = %d/%v", u.Input, u.HasInput)
	}
	if u.Output != 125 || !u.HasOutput {
		t.Fatalf("output = %d/%v", u.Output, u.HasOutput)
	}
	if u.CacheRead != 10 || !u.HasCacheRead {
		t.Fatalf("cache read = %d/%v", u.CacheRead, u.HasCacheRead)
	}
	if u.CacheWrite != 5 || !u.HasCacheWrite {
		t.Fatalf("cache write = %d/%v", u.CacheWrite, u.HasCacheWrite)
	}
}

func TestCursorTokenUsageReadsTerminalResultOnce(t *testing.T) {
	events := []streamEventRecord{
		{Type: "event", AtMS: 5, Raw: `{"type":"system","subtype":"init"}`},
		{Type: "event", AtMS: 50, Raw: `{"type":"assistant","message":{"content":[{"type":"text","text":"chunk"}]}}`},
		// A decoy with the same camelCase keys on a non-result event must not
		// be read — authoritative usage lives only on the terminal result.
		{Type: "event", AtMS: 80, Raw: `{"type":"assistant","usage":{"inputTokens":999,"outputTokens":999,"cacheReadTokens":999,"cacheWriteTokens":999}}`},
		{Type: "event", AtMS: 100, Raw: `{"type":"result","subtype":"success","result":"ok","usage":{"inputTokens":73366,"outputTokens":3726,"cacheReadTokens":220800,"cacheWriteTokens":0}}`},
	}
	u := cursorTokenUsage(events)
	if !u.HasUsage() {
		t.Fatal("expected usage")
	}
	if u.Input != 73366 || !u.HasInput {
		t.Fatalf("input = %d/%v", u.Input, u.HasInput)
	}
	if u.Output != 3726 || !u.HasOutput {
		t.Fatalf("output = %d/%v", u.Output, u.HasOutput)
	}
	if u.CacheRead != 220800 || !u.HasCacheRead {
		t.Fatalf("cache read = %d/%v", u.CacheRead, u.HasCacheRead)
	}
	if u.CacheWrite != 0 || !u.HasCacheWrite {
		t.Fatalf("cache write = %d/%v (reported zero)", u.CacheWrite, u.HasCacheWrite)
	}
}

func TestCursorTokenUsageReplaceDoesNotSumMultipleResults(t *testing.T) {
	events := []streamEventRecord{
		{Type: "event", AtMS: 100, Raw: `{"type":"result","subtype":"success","usage":{"inputTokens":10,"outputTokens":20}}`},
		{Type: "event", AtMS: 200, Raw: `{"type":"result","subtype":"success","usage":{"inputTokens":100,"outputTokens":200,"cacheReadTokens":5}}`},
	}
	u := cursorTokenUsage(events)
	if u.Input != 100 || u.Output != 200 || u.CacheRead != 5 {
		t.Fatalf("expected last result to replace, got %+v", u)
	}
}

func TestPiTokenUsageSumsMessageEndIgnoresUpdateDeltas(t *testing.T) {
	// Many message_update deltas carry a *cumulative* usage block. Summing
	// them is the exact failure ADR-0160 guards against (~4× over-count).
	var events []streamEventRecord
	events = append(events, streamEventRecord{
		Type: "event", AtMS: 1,
		Raw: `{"type":"message_start","message":{"role":"assistant","usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0}}}`,
	})
	for i := 0; i < 50; i++ {
		// Cumulative figures grow on every delta — a field-name map would
		// sum these and report 50× the real per-message total.
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

	u := piTokenUsage(events)
	if u.Input != 180 || !u.HasInput {
		t.Fatalf("input = %d/%v (must sum message_end only, not 50 deltas)", u.Input, u.HasInput)
	}
	if u.Output != 60 || !u.HasOutput {
		t.Fatalf("output = %d/%v", u.Output, u.HasOutput)
	}
	if u.CacheRead != 110 || !u.HasCacheRead {
		t.Fatalf("cache read = %d/%v", u.CacheRead, u.HasCacheRead)
	}
	if u.CacheWrite != 5 || !u.HasCacheWrite {
		t.Fatalf("cache write = %d/%v", u.CacheWrite, u.HasCacheWrite)
	}
}

func TestExtractTokenUsageTokenBlindForUnknownAdapter(t *testing.T) {
	events := []streamEventRecord{
		{Type: "event", AtMS: 1, Raw: `{"type":"result","usage":{"inputTokens":0,"outputTokens":0}}`},
	}
	blind := extractTokenUsage("codex", events)
	if blind.HasUsage() {
		t.Fatalf("codex should be token-blind, got %+v", blind)
	}

	// Reported zero (cursor result with zero counts) must still set Has*.
	reportedZero := extractTokenUsage("cursor", events)
	if !reportedZero.HasUsage() {
		t.Fatal("reported zero must not be token-blind")
	}
	if reportedZero.Input != 0 || !reportedZero.HasInput {
		t.Fatalf("reported zero input = %d/%v", reportedZero.Input, reportedZero.HasInput)
	}
	if reportedZero.Output != 0 || !reportedZero.HasOutput {
		t.Fatalf("reported zero output = %d/%v", reportedZero.Output, reportedZero.HasOutput)
	}
}

func TestPiPartialCostSumsMessageEndTotalIgnoresComponents(t *testing.T) {
	// Component keys must not be summed alongside total — that double-counts.
	events := []streamEventRecord{
		{Type: "event", AtMS: 100, Raw: `{"type":"message_end","message":{"role":"assistant","cost":{"total":0.05,"input":0.03,"output":0.02}}}`},
		{Type: "event", AtMS: 200, Raw: `{"type":"message_end","message":{"role":"assistant","cost":{"total":0.10,"input":0.06,"output":0.04,"cacheRead":0.01,"cacheWrite":0.02}}}`},
	}
	c := piPartialCost(events)
	if !c.HasCost {
		t.Fatal("expected cost")
	}
	if diff := c.Dollars - 0.15; diff > 0.0001 || diff < -0.0001 {
		t.Fatalf("cost = %v, want 0.15 (total only, not components)", c.Dollars)
	}
}

func TestPiPartialCostIgnoresMessageUpdateDeltas(t *testing.T) {
	var events []streamEventRecord
	for i := 0; i < 20; i++ {
		events = append(events, streamEventRecord{
			Type: "event", AtMS: int64(10 + i),
			Raw: `{"type":"message_update","message":{"role":"assistant","cost":{"total":0.99,"input":0.50,"output":0.49}}}`,
		})
	}
	events = append(events, streamEventRecord{
		Type: "event", AtMS: 100,
		Raw: `{"type":"message_end","message":{"role":"assistant","cost":{"total":0.05,"input":0.03,"output":0.02}}}`,
	})
	c := piPartialCost(events)
	if c.Dollars != 0.05 {
		t.Fatalf("cost = %v, want message_end total only", c.Dollars)
	}
}

func TestExtractPartialCostNoCostForClaudeCursorOrUnknown(t *testing.T) {
	events := []streamEventRecord{
		{Type: "event", AtMS: 1, Raw: `{"type":"result","usage":{"input_tokens":1,"output_tokens":1}}`},
	}
	for _, agent := range []string{"claude", "cursor", "codex"} {
		c := extractPartialCost(agent, events)
		if c.HasCost {
			t.Fatalf("%s should report no cost, got %+v", agent, c)
		}
	}
}

func TestExtractPartialCostDispatchesPi(t *testing.T) {
	c := extractPartialCost("pi", []streamEventRecord{
		{Type: "event", AtMS: 1, Raw: `{"type":"message_end","message":{"cost":{"total":1.25}}}`},
	})
	if !c.HasCost || c.Dollars != 1.25 {
		t.Fatalf("pi cost = %+v", c)
	}
}

func TestExtractTokenUsageDispatchesAllThreeAdapters(t *testing.T) {
	claude := extractTokenUsage("claude", []streamEventRecord{
		{Type: "event", AtMS: 1, Raw: `{"type":"result","usage":{"input_tokens":1,"output_tokens":2}}`},
	})
	if claude.Input != 1 || claude.Output != 2 {
		t.Fatalf("claude = %+v", claude)
	}
	cursor := extractTokenUsage("cursor", []streamEventRecord{
		{Type: "event", AtMS: 1, Raw: `{"type":"result","usage":{"inputTokens":3,"outputTokens":4}}`},
	})
	if cursor.Input != 3 || cursor.Output != 4 {
		t.Fatalf("cursor = %+v", cursor)
	}
	pi := extractTokenUsage("pi", []streamEventRecord{
		{Type: "event", AtMS: 1, Raw: `{"type":"message_end","message":{"usage":{"input":5,"output":6}}}`},
	})
	if pi.Input != 5 || pi.Output != 6 {
		t.Fatalf("pi = %+v", pi)
	}
}

func TestListSpendRunsIgnoresLegacyAttemptLayout(t *testing.T) {
	env := timingsFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	taskSetDir := env.demoDir()

	// Legacy attempt-NNN layout — must not appear in spend runs.
	writeTimingStreamWithEvents(t, taskStreamDir(taskSetDir, "01-a.md"), "attempt-001.jsonl.gz", "claude", "", 1, base, "completed", 60_000, []streamEventRecord{
		{Type: "event", AtMS: 100, Raw: `{"type":"result","usage":{"input_tokens":999,"output_tokens":999}}`},
	})

	// New streams/runs/ layout — the only substrate spend reads.
	writeSpendRun(t, taskSetDir, "01-a.md", "01-a", "claude", base.Add(time.Minute), []streamEventRecord{
		{Type: "event", AtMS: 100, Raw: `{"type":"result","usage":{"input_tokens":42,"output_tokens":7}}`},
	})

	runs, err := listSpendRuns(env.deps(), taskSetDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("spend runs = %d, want 1 (legacy excluded)", len(runs))
	}
	if isLegacyRun(runs[0]) {
		t.Fatal("spend run must not be legacy")
	}
	spend, err := runSpend(runs[0])
	if err != nil {
		t.Fatal(err)
	}
	if spend.Tokens.Input != 42 || spend.Tokens.Output != 7 {
		t.Fatalf("tokens = %+v", spend.Tokens)
	}
	if spend.Cost.HasCost {
		t.Fatalf("claude should report no cost, got %+v", spend.Cost)
	}
}

func TestRunSpendRejectsLegacy(t *testing.T) {
	run := capturedRun{
		meta:       capturedRunMeta{Agent: "claude", RunID: "legacy:01-a:attempt-001"},
		legacyPath: "/tmp/streams/01-a/attempt-001.jsonl.gz",
		events: []streamEventRecord{
			{Type: "event", AtMS: 1, Raw: `{"type":"result","usage":{"input_tokens":1,"output_tokens":1}}`},
		},
	}
	if _, err := runSpend(run); err == nil {
		t.Fatal("expected error for legacy run")
	}
}

// writeSpendRun writes a streams/runs/ Captured run pair with the given events.
func writeSpendRun(t *testing.T, taskSetDir, taskFile, taskID, agent string, start time.Time, events []streamEventRecord) {
	t.Helper()
	dir := capturedRunsDir(taskSetDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	runID := uuid.New().String()
	meta := capturedRunMeta{
		RunID:     runID,
		Phase:     "implement",
		TaskSetID: "demo",
		TaskID:    taskID,
		TaskFile:  taskFile,
		StartTime: start.UTC(),
		EndTime:   start.Add(time.Minute).UTC(),
		Outcome:   "completed",
		Agent:     agent,
		Attempt:   1,
	}
	metaData, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, runID+".meta.json"), metaData, 0o644); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	for _, ev := range events {
		line, err := json.Marshal(ev)
		if err != nil {
			t.Fatal(err)
		}
		raw = append(raw, line...)
		raw = append(raw, '\n')
	}
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	if _, err := zw.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, runID+".events.jsonl.gz"), gz.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}
