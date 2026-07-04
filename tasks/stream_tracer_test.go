package tasks

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func streamFixture(t *testing.T) *execFixture {
	t.Helper()
	return setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open"},
	})
}

func TestStreamWholeSetSpansAttemptsOrderedByStartTime(t *testing.T) {
	env := streamFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	streamDir := taskStreamDir(env.demoDir(), "01-a.md")

	claudeEvents := []streamEventRecord{
		{Type: "event", AtMS: 5, Raw: `{"type":"system","subtype":"init","model":"claude-sonnet-4-20250514"}`},
		{Type: "event", AtMS: 100, Raw: `{"type":"assistant","message":{"content":[{"type":"text","text":"Let me start by reading the file."}]}}`},
		{Type: "event", AtMS: 1500, Raw: `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu_1","name":"Read","input":{"file_path":"/tmp/test.go"}}]}}`},
		{Type: "event", AtMS: 2500, Raw: `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu_1","content":[{"type":"text","text":"file contents"}]}]}}`},
		{Type: "event", AtMS: 3000, Raw: `{"type":"assistant","message":{"content":[{"type":"text","text":"Now I'll make the changes."}]}}`},
	}

	// 01-a: two legacy attempts at base and base+10m.
	writeTimingStreamWithEvents(t, streamDir, "attempt-001.jsonl.gz", "claude", "", 1, base.Add(10*time.Minute), "failed", 45_000, claudeEvents)
	writeTimingStreamWithEvents(t, streamDir, "attempt-002.jsonl.gz", "claude", "", 2, base, "timed_out", 83_000, claudeEvents)
	// 02-b: a legacy attempt at base-5m, earlier than 01-a's earliest run.
	writeTimingStream(t, taskStreamDir(env.demoDir(), "02-b.md"), "attempt-001.jsonl.gz", "claude", 1, base.Add(-5*time.Minute), "completed", 500)

	result, err := StreamWith(env.deps(), nil, nil, StreamOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "demo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskSetID != "demo" {
		t.Fatalf("task set = %q", result.TaskSetID)
	}

	// Tasks should be ordered by their earliest run, so 02-b (base-5m) comes first.
	if len(result.Tasks) != 2 || result.Tasks[0].TaskID != "02-b" || result.Tasks[1].TaskID != "01-a" {
		t.Fatalf("tasks = %#v", result.Tasks)
	}

	// Within each task, attempts are ordered by start time.
	attemptsA := result.Tasks[1].Attempts
	if len(attemptsA) != 2 {
		t.Fatalf("01-a attempts = %d, want 2", len(attemptsA))
	}
	if attemptsA[0].Timing.Start.After(attemptsA[1].Timing.Start) {
		t.Fatalf("01-a attempts not ordered by start time")
	}
	if attemptsA[0].Timing.Outcome != "timed_out" || attemptsA[1].Timing.Outcome != "failed" {
		t.Fatalf("01-a attempt outcomes = %q, %q", attemptsA[0].Timing.Outcome, attemptsA[1].Timing.Outcome)
	}

	attemptsB := result.Tasks[0].Attempts
	if len(attemptsB) != 1 || attemptsB[0].Timing.Outcome != "completed" {
		t.Fatalf("02-b attempts = %#v", attemptsB)
	}

	// Verify events were parsed.
	if len(attemptsA[0].Events) == 0 {
		t.Fatalf("no events parsed for attempt")
	}
}

// writeCapturedRunDirect writes a new-format captured run pair with explicit
// phase and timing. It is useful for testing set-scope ordering across phases.
func writeCapturedRunDirect(t *testing.T, taskSetDir, taskFile, taskID, phase string, attempt int, start time.Time, outcome string) {
	t.Helper()
	dir := capturedRunsDir(taskSetDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	runID := uuid.New().String()
	meta := capturedRunMeta{
		RunID:     runID,
		Phase:     phase,
		TaskSetID: "demo",
		TaskID:    taskID,
		TaskFile:  taskFile,
		StartTime: start.UTC(),
		EndTime:   start.Add(time.Minute).UTC(),
		Outcome:   outcome,
		Agent:     "claude",
		Attempt:   attempt,
	}
	metaData, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, runID+".meta.json"), metaData, 0o644); err != nil {
		t.Fatal(err)
	}
	var events bytes.Buffer
	enc := json.NewEncoder(&events)
	if err := enc.Encode(streamEventRecord{Type: "event", AtMS: 0, Raw: `{"type":"system","subtype":"init"}`}); err != nil {
		t.Fatal(err)
	}
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	if _, err := zw.Write(events.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, runID+".events.jsonl.gz"), gz.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestStreamWholeSetOrdersTasksByEarliestRun(t *testing.T) {
	env := streamFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	// 02-b has the earliest run, so it should appear before 01-a even though
	// the manifest lists 01-a first.
	writeTimingStream(t, taskStreamDir(env.demoDir(), "02-b.md"), "attempt-001.jsonl.gz", "claude", 1, base.Add(-5*time.Minute), "completed", 60_000)
	writeTimingStream(t, taskStreamDir(env.demoDir(), "01-a.md"), "attempt-001.jsonl.gz", "claude", 1, base, "failed", 30_000)

	result, err := StreamWith(env.deps(), nil, nil, StreamOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "demo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(result.Tasks))
	}
	if result.Tasks[0].TaskID != "02-b" || result.Tasks[1].TaskID != "01-a" {
		t.Fatalf("tasks not ordered by earliest run: %#v", result.Tasks)
	}
}

func TestStreamWholeSetMixesNewAndLegacyLayouts(t *testing.T) {
	env := streamFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	// New-format run for 01-a at base+5m.
	writeCapturedRunDirect(t, env.demoDir(), "01-a.md", "01-a", "implement", 1, base.Add(5*time.Minute), "completed")
	// Legacy run for 02-b at base (earlier than 01-a's new-format run).
	writeTimingStream(t, taskStreamDir(env.demoDir(), "02-b.md"), "attempt-001.jsonl.gz", "claude", 1, base, "failed", 30_000)

	result, err := StreamWith(env.deps(), nil, nil, StreamOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "demo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(result.Tasks))
	}
	// 02-b's legacy run is earlier, so 02-b comes first.
	if result.Tasks[0].TaskID != "02-b" || result.Tasks[1].TaskID != "01-a" {
		t.Fatalf("tasks not ordered by start time: %#v", result.Tasks)
	}
	// Verify the legacy run synthesized meta carries the expected fields.
	legacy := result.Tasks[0].Attempts[0]
	if legacy.Timing.Outcome != "failed" {
		t.Fatalf("legacy outcome = %q, want failed", legacy.Timing.Outcome)
	}
}

func TestStreamWholeSetTieBreaksImplementBeforeVerify(t *testing.T) {
	env := streamFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	// 01-a verify run at base.
	writeCapturedRunDirect(t, env.demoDir(), "01-a.md", "01-a", "verify", 1, base, "completed")
	// 02-b implement run at the same timestamp; implement should sort first.
	writeCapturedRunDirect(t, env.demoDir(), "02-b.md", "02-b", "implement", 1, base, "completed")

	result, err := StreamWith(env.deps(), nil, nil, StreamOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "demo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(result.Tasks))
	}
	if result.Tasks[0].TaskID != "02-b" || result.Tasks[1].TaskID != "01-a" {
		t.Fatalf("implement did not sort before verify at equal start time: %#v", result.Tasks)
	}
}

func TestStreamWholeSetIncludesVerifyRunsAsSyntheticTask(t *testing.T) {
	env := streamFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	// Implement run for 01-a.
	writeCapturedRunDirect(t, env.demoDir(), "01-a.md", "01-a", "implement", 1, base, "completed")
	// Set-level verify run (no task file) at base+5m.
	writeCapturedRunDirect(t, env.demoDir(), "", "", "verify", 1, base.Add(5*time.Minute), "completed")

	result, err := StreamWith(env.deps(), nil, nil, StreamOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "demo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d: %#v", len(result.Tasks), result.Tasks)
	}
	// Verify run should appear as a synthetic task ordered by its start time.
	if result.Tasks[0].TaskID != "01-a" || result.Tasks[1].TaskID != "verify" {
		t.Fatalf("tasks not ordered correctly: %#v", result.Tasks)
	}
	if result.Tasks[1].File != "" || result.Tasks[1].Title != "Verify" {
		t.Fatalf("verify task stream = %#v", result.Tasks[1])
	}
	if len(result.Tasks[1].Attempts) != 1 {
		t.Fatalf("expected 1 verify attempt, got %d", len(result.Tasks[1].Attempts))
	}
}

func TestStreamSingleTaskTarget(t *testing.T) {
	env := streamFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	claudeEvents := []streamEventRecord{
		{Type: "event", AtMS: 5, Raw: `{"type":"system","subtype":"init","model":"claude-sonnet-4-20250514"}`},
		{Type: "event", AtMS: 100, Raw: `{"type":"assistant","message":{"content":[{"type":"text","text":"Hello world"}]}}`},
		{Type: "event", AtMS: 2000, Raw: `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu_1","name":"Bash","input":{"command":"ls -la"}}]}}`},
		{Type: "event", AtMS: 3000, Raw: `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu_1","content":[{"type":"text","text":"file1.txt\nfile2.txt"}]}]}}`},
	}

	writeTimingStreamWithEvents(t, taskStreamDir(env.demoDir(), "02-b.md"), "attempt-001.jsonl.gz", "claude", "", 1, base, "completed", 60_000, claudeEvents)
	writeTimingStream(t, taskStreamDir(env.demoDir(), "01-a.md"), "attempt-001.jsonl.gz", "claude", 1, base, "failed", 30_000)

	result, err := StreamWith(env.deps(), nil, nil, StreamOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "demo/02-b.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tasks) != 1 || result.Tasks[0].TaskID != "02-b" {
		t.Fatalf("tasks = %#v, want only 02-b", result.Tasks)
	}
	if len(result.Tasks[0].Attempts) != 1 || result.Tasks[0].Attempts[0].Timing.Outcome != "completed" {
		t.Fatalf("attempts = %#v", result.Tasks[0].Attempts)
	}

	// Verify events were parsed correctly
	events := result.Tasks[0].Attempts[0].Events
	if len(events) < 3 {
		t.Fatalf("expected at least 3 events, got %d", len(events))
	}

	// Check system init event
	if events[0].Type != "system" || !strings.Contains(events[0].Text, "init") {
		t.Fatalf("first event should be system init, got %+v", events[0])
	}

	// Check assistant text event
	if events[1].Type != "assistant" || events[1].Text != "Hello world" {
		t.Fatalf("second event should be assistant text, got %+v", events[1])
	}

	// Check tool_use event
	if events[2].Type != "tool_use" || events[2].ToolName != "Bash" {
		t.Fatalf("third event should be tool_use Bash, got %+v", events[2])
	}
}

func TestStreamEmptyCase(t *testing.T) {
	env := streamFixture(t)

	result, err := StreamWith(env.deps(), nil, nil, StreamOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "demo",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Should have tasks but no attempts
	if len(result.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(result.Tasks))
	}
	for _, task := range result.Tasks {
		if len(task.Attempts) != 0 {
			t.Fatalf("task %s should have no attempts, got %d", task.TaskID, len(task.Attempts))
		}
	}

	// Render should show "no captured attempt streams"
	var buf bytes.Buffer
	RenderStream(&buf, result, RenderStreamOptions{})
	out := buf.String()

	if !strings.Contains(out, "no captured attempt streams for demo") {
		t.Fatalf("output should show no captured streams message:\n%s", out)
	}
}

func TestStreamIncludesClaudeTokenUsage(t *testing.T) {
	env := streamFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	streamDir := taskStreamDir(env.demoDir(), "01-a.md")
	events := []streamEventRecord{
		{Type: "event", AtMS: 5, Raw: `{"type":"system","subtype":"init"}`},
		{Type: "event", AtMS: 100, Raw: `{"type":"result","subtype":"success","result":"ok","usage":{"input_tokens":1234,"output_tokens":567,"cache_read_input_tokens":89,"cache_creation_input_tokens":12}}`},
	}
	writeTimingStreamWithEvents(t, streamDir, "attempt-001.jsonl.gz", "claude", "", 1, base, "completed", 60_000, events)

	result, err := StreamWith(env.deps(), nil, nil, StreamOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "demo/01-a.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	at := result.Tasks[0].Attempts[0].Timing
	if !at.Tokens.HasUsage() {
		t.Fatal("expected token usage")
	}
	if at.Tokens.Input != 1234 || at.Tokens.Output != 567 || at.Tokens.CacheRead != 89 || at.Tokens.CacheWrite != 12 {
		t.Fatalf("tokens = %+v", at.Tokens)
	}

	var buf bytes.Buffer
	RenderStream(&buf, result, RenderStreamOptions{})
	out := buf.String()
	if !strings.Contains(out, "in 1234 / out 567 / cache 89r 12w") {
		t.Fatalf("output missing token summary:\n%s", out)
	}
}

func TestStreamRejectsInvalidTargets(t *testing.T) {
	env := streamFixture(t)

	cases := map[string]struct {
		target string
		code   int
	}{
		"unknown set":       {"missing", ExitNoRunnable},
		"unknown task file": {"demo/99-z.md", ExitNoRunnable},
		"bare filename":     {"01-a.md", ExitSetup},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := StreamWith(env.deps(), nil, nil, StreamOptions{
				ResolveInput: ResolveInput{CWD: env.root},
				Target:       tc.target,
			})
			assertExitCode(t, err, tc.code)
		})
	}
}

func TestRenderStreamShowsTimingHeaderAndEventReplay(t *testing.T) {
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	result := &StreamResult{
		TaskSetID: "demo",
		Tasks: []TaskStream{
			{
				TaskID: "01-a",
				File:   "01-a.md",
				Title:  "A",
				Attempts: []AttemptStream{
					{
						Timing: AttemptTiming{
							Agent:       "claude",
							ActualModel: "claude-sonnet-4-20250514",
							Start:       base,
							Outcome:     "completed",
							Duration:    90 * time.Second,
							Tools: []ToolTiming{
								{Name: "Bash", Count: 2, Total: 10 * time.Second},
							},
							Model: 80 * time.Second,
						},
						Events: []StreamEvent{
							{AtMS: 5, Type: "system", Text: "system init (model: claude-sonnet-4-20250514)"},
							{AtMS: 100, Type: "assistant", Text: "Let me read the file"},
							{AtMS: 1500, Type: "tool_use", ToolName: "Read", ToolArgs: `{"file_path":"/tmp/test.go"}`},
							{AtMS: 2500, Type: "tool_result", Text: "file contents here"},
							{AtMS: 5000, Type: "assistant", Text: "Now I'll fix it"},
						},
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	RenderStream(&buf, result, RenderStreamOptions{})
	out := buf.String()

	// Should show timing header
	if !strings.Contains(out, "2026-06-10T12:00:00Z") {
		t.Fatalf("output missing timestamp:\n%s", out)
	}
	if !strings.Contains(out, "claude") {
		t.Fatalf("output missing agent:\n%s", out)
	}
	if !strings.Contains(out, "completed") {
		t.Fatalf("output missing outcome:\n%s", out)
	}

	// Should show tool timings
	if !strings.Contains(out, "Bash") {
		t.Fatalf("output missing tool name:\n%s", out)
	}
	if !strings.Contains(out, "×2") {
		t.Fatalf("output missing tool count:\n%s", out)
	}

	// Should show event replay with offsets
	if !strings.Contains(out, "+5ms") {
		t.Fatalf("output missing +5ms offset:\n%s", out)
	}
	if !strings.Contains(out, "+100ms") {
		t.Fatalf("output missing +100ms offset:\n%s", out)
	}
	if !strings.Contains(out, "+1s") {
		t.Fatalf("output missing +1s offset:\n%s", out)
	}
	if !strings.Contains(out, "+2s") {
		t.Fatalf("output missing +2s offset:\n%s", out)
	}
	if !strings.Contains(out, "+5s") {
		t.Fatalf("output missing +5s offset:\n%s", out)
	}

	// Should show assistant text
	if !strings.Contains(out, "Let me read the file") {
		t.Fatalf("output missing assistant text:\n%s", out)
	}

	// Should show tool use
	if !strings.Contains(out, "→ Read") {
		t.Fatalf("output missing tool use indicator:\n%s", out)
	}
}

func TestRenderStreamHandlesMixedOutcomes(t *testing.T) {
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	result := &StreamResult{
		TaskSetID: "demo",
		Tasks: []TaskStream{
			{
				TaskID: "01-a",
				File:   "01-a.md",
				Title:  "A",
				Attempts: []AttemptStream{
					{
						Timing: AttemptTiming{
							Agent:    "claude",
							Start:    base,
							Outcome:  "failed",
							Duration: 45 * time.Second,
						},
						Events: []StreamEvent{
							{AtMS: 100, Type: "assistant", Text: "Trying something"},
						},
					},
					{
						Timing: AttemptTiming{
							Agent:    "claude",
							Start:    base.Add(10 * time.Minute),
							Outcome:  "completed",
							Duration: 30 * time.Second,
						},
						Events: []StreamEvent{
							{AtMS: 100, Type: "assistant", Text: "Fixed it"},
						},
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	RenderStream(&buf, result, RenderStreamOptions{})
	out := buf.String()

	// Should show both attempts
	if strings.Count(out, "failed") != 1 {
		t.Fatalf("output should show one failed attempt:\n%s", out)
	}
	if strings.Count(out, "completed") != 1 {
		t.Fatalf("output should show one completed attempt:\n%s", out)
	}

	// Should show both event replays
	if !strings.Contains(out, "Trying something") {
		t.Fatalf("output missing first attempt text:\n%s", out)
	}
	if !strings.Contains(out, "Fixed it") {
		t.Fatalf("output missing second attempt text:\n%s", out)
	}
}

func TestFormatOffset(t *testing.T) {
	cases := []struct {
		ms   int64
		want string
	}{
		{0, "+0ms"},
		{500, "+500ms"},
		{999, "+999ms"},
		{1000, "+1s"},
		{1500, "+1s"},
		{59000, "+59s"},
		{60000, "+1m0s"},
		{90000, "+1m30s"},
		{3661000, "+61m1s"},
	}
	for _, tc := range cases {
		got := formatOffset(tc.ms)
		if got != tc.want {
			t.Errorf("formatOffset(%d) = %q, want %q", tc.ms, got, tc.want)
		}
	}
}

func TestTruncatePayloadLineThreshold(t *testing.T) {
	// Build a 50-line payload that exceeds the default 40-line threshold.
	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i+1))
	}
	payload := strings.Join(lines, "\n")

	truncated := truncatePayload(payload, false)
	if truncated == payload {
		t.Fatal("expected payload to be truncated")
	}
	if !strings.Contains(truncated, "elided") {
		t.Fatalf("expected elision marker, got:\n%s", truncated)
	}
	// Head and tail should be present.
	if !strings.Contains(truncated, "line 1") {
		t.Fatalf("missing head line:\n%s", truncated)
	}
	if !strings.Contains(truncated, "line 50") {
		t.Fatalf("missing tail line:\n%s", truncated)
	}
	// Middle lines should be elided.
	if strings.Contains(truncated, "line 25") {
		t.Fatalf("middle line should be elided:\n%s", truncated)
	}

	// Full mode returns the payload verbatim.
	full := truncatePayload(payload, true)
	if full != payload {
		t.Fatalf("--full should return payload verbatim")
	}
}

func TestTruncatePayloadByteThreshold(t *testing.T) {
	// Single-line payload longer than the default 4 KB byte threshold.
	payload := strings.Repeat("x", 5000)

	truncated := truncatePayload(payload, false)
	if truncated == payload {
		t.Fatal("expected payload to be truncated")
	}
	if !strings.Contains(truncated, "elided") {
		t.Fatalf("expected elision marker, got:\n%s", truncated)
	}
	if !strings.HasPrefix(truncated, strings.Repeat("x", streamTruncationLimits.HeadBytes)) {
		t.Fatal("missing head bytes")
	}
	if !strings.HasSuffix(truncated, strings.Repeat("x", streamTruncationLimits.TailBytes)) {
		t.Fatal("missing tail bytes")
	}

	full := truncatePayload(payload, true)
	if full != payload {
		t.Fatal("--full should return payload verbatim")
	}
}

func TestTruncatePayloadSmallPayloadUnchanged(t *testing.T) {
	payload := "small payload\nwith a few lines"

	if got := truncatePayload(payload, false); got != payload {
		t.Fatalf("small payload changed in default mode: %q", got)
	}
	if got := truncatePayload(payload, true); got != payload {
		t.Fatalf("small payload changed in full mode: %q", got)
	}
}

func TestRenderStreamTruncatesLargeToolResult(t *testing.T) {
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, fmt.Sprintf("result line %d", i+1))
	}
	resultText := strings.Join(lines, "\n")

	res := &StreamResult{
		TaskSetID: "demo",
		Tasks: []TaskStream{
			{
				TaskID: "01-a",
				File:   "01-a.md",
				Title:  "A",
				Attempts: []AttemptStream{
					{
						Timing: AttemptTiming{
							Agent:    "claude",
							Start:    base,
							Outcome:  "completed",
							Duration: 10 * time.Second,
						},
						Events: []StreamEvent{
							{AtMS: 100, Type: "tool_result", Text: resultText},
						},
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	RenderStream(&buf, res, RenderStreamOptions{})
	out := buf.String()

	if !strings.Contains(out, "elided") {
		t.Fatalf("expected elision marker, got:\n%s", out)
	}
	if !strings.Contains(out, "result line 1") {
		t.Fatalf("missing head:\n%s", out)
	}
	if !strings.Contains(out, "result line 50") {
		t.Fatalf("missing tail:\n%s", out)
	}
	if strings.Contains(out, "result line 25") {
		t.Fatalf("middle should be elided:\n%s", out)
	}

	buf.Reset()
	RenderStream(&buf, res, RenderStreamOptions{Full: true})
	fullOut := buf.String()

	if strings.Contains(fullOut, "elided") {
		t.Fatalf("--full should not contain elision marker:\n%s", fullOut)
	}
	if !strings.Contains(fullOut, "result line 25") {
		t.Fatalf("--full should contain middle lines:\n%s", fullOut)
	}
}

func TestRenderStreamTruncatesLargeToolUseArgs(t *testing.T) {
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, fmt.Sprintf("arg line %d", i+1))
	}
	args := strings.Join(lines, "\n")

	res := &StreamResult{
		TaskSetID: "demo",
		Tasks: []TaskStream{
			{
				TaskID: "01-a",
				File:   "01-a.md",
				Title:  "A",
				Attempts: []AttemptStream{
					{
						Timing: AttemptTiming{
							Agent:    "claude",
							Start:    base,
							Outcome:  "completed",
							Duration: 10 * time.Second,
						},
						Events: []StreamEvent{
							{AtMS: 100, Type: "tool_use", ToolName: "Write", ToolArgs: args},
						},
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	RenderStream(&buf, res, RenderStreamOptions{})
	out := buf.String()

	if !strings.Contains(out, "elided") {
		t.Fatalf("expected elision marker, got:\n%s", out)
	}

	buf.Reset()
	RenderStream(&buf, res, RenderStreamOptions{Full: true})
	fullOut := buf.String()

	if strings.Contains(fullOut, "elided") {
		t.Fatalf("--full should not contain elision marker:\n%s", fullOut)
	}
	if !strings.Contains(fullOut, "arg line 25") {
		t.Fatalf("--full should contain middle lines:\n%s", fullOut)
	}
}

func TestStreamRawSingleTaskSingleAttemptRoundTrip(t *testing.T) {
	env := streamFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	// Write one gzipped attempt stream for the first task.
	streamDir := taskStreamDir(env.demoDir(), "01-a.md")
	writeTimingStreamWithEvents(t, streamDir, "attempt-001.jsonl.gz", "claude", "claude --model opus4.8", 1, base, "completed", 60_000, []streamEventRecord{
		{Type: "event", AtMS: 5, Raw: `{"type":"system","subtype":"init"}`},
		{Type: "event", AtMS: 100, Raw: `{"type":"assistant","message":{"content":[{"type":"text","text":"working"}]}}`},
	})

	// Read the raw gzip file to compare against.
	rawPath := filepath.Join(streamDir, "attempt-001.jsonl.gz")
	rawGz, err := os.ReadFile(rawPath)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(rawGz))
	if err != nil {
		t.Fatal(err)
	}
	wantJSONL, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	_ = zr.Close()
	wantJSONL = bytes.TrimSpace(wantJSONL) // StreamRawWith trims trailing whitespace

	var buf bytes.Buffer
	err = StreamRawWith(env.deps(), nil, nil, StreamOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "demo/01-a.md",
	}, &buf)
	if err != nil {
		t.Fatal(err)
	}

	got := strings.TrimSpace(buf.String())
	if got != string(wantJSONL) {
		t.Fatalf("raw output mismatch:\ngot:\n%s\n\nwant:\n%s", got, string(wantJSONL))
	}
}

func TestStreamRawMultiAttemptDelimiter(t *testing.T) {
	env := streamFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	streamDir := taskStreamDir(env.demoDir(), "01-a.md")
	// Write two attempts with different start times.
	writeTimingStreamWithEvents(t, streamDir, "attempt-001.jsonl.gz", "claude", "", 1, base, "failed", 45_000, []streamEventRecord{
		{Type: "event", AtMS: 5, Raw: `{"type":"system","subtype":"init"}`},
	})
	writeTimingStreamWithEvents(t, streamDir, "attempt-002.jsonl.gz", "claude", "", 2, base.Add(10*time.Minute), "completed", 30_000, []streamEventRecord{
		{Type: "event", AtMS: 5, Raw: `{"type":"system","subtype":"init"}`},
	})

	var buf bytes.Buffer
	err := StreamRawWith(env.deps(), nil, nil, StreamOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "demo/01-a.md",
	}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	// Should contain both attempt files' JSONL content.
	if !strings.Contains(out, `"attempt":1`) {
		t.Fatalf("output missing attempt 1 header:\n%s", out)
	}
	if !strings.Contains(out, `"attempt":2`) {
		t.Fatalf("output missing attempt 2 header:\n%s", out)
	}

	// Should contain delimiter between the two attempts.
	if !strings.Contains(out, `{"type":"delimiter","file":"attempt-002.jsonl.gz"}`) {
		t.Fatalf("output missing delimiter:\n%s", out)
	}

	// Verify the delimiter appears after attempt 1 and before attempt 2.
	attempt1Pos := strings.Index(out, `"attempt":1`)
	delimPos := strings.Index(out, `"type":"delimiter"`)
	attempt2Pos := strings.Index(out, `"attempt":2`)
	if delimPos < attempt1Pos {
		t.Fatalf("delimiter before attempt 1")
	}
	if attempt2Pos < delimPos {
		t.Fatalf("attempt 2 before delimiter")
	}
}

func TestStreamRawWholeSetMultipleTasks(t *testing.T) {
	env := streamFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	// Write attempt for task 01-a.
	dirA := taskStreamDir(env.demoDir(), "01-a.md")
	writeTimingStream(t, dirA, "attempt-001.jsonl.gz", "claude", 1, base, "completed", 60_000)

	// Write attempt for task 02-b.
	dirB := taskStreamDir(env.demoDir(), "02-b.md")
	writeTimingStream(t, dirB, "attempt-001.jsonl.gz", "claude", 1, base.Add(5*time.Minute), "failed", 30_000)

	var buf bytes.Buffer
	err := StreamRawWith(env.deps(), nil, nil, StreamOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "demo",
	}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	// Both tasks should appear.
	if !strings.Contains(out, `"attempt":1`) {
		t.Fatalf("output missing attempt data:\n%s", out)
	}

	// Count delimiters: one between the two tasks' attempts.
	delimCount := strings.Count(out, `"type":"delimiter"`)
	if delimCount != 1 {
		t.Fatalf("expected 1 delimiter between tasks, got %d:\n%s", delimCount, out)
	}
}

func TestStreamRawWholeSetOrdersChronologically(t *testing.T) {
	env := streamFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	// 02-b run is earlier than 01-a run, so it should be emitted first.
	writeTimingStream(t, taskStreamDir(env.demoDir(), "02-b.md"), "attempt-001.jsonl.gz", "claude", 1, base.Add(-5*time.Minute), "completed", 60_000)
	writeTimingStream(t, taskStreamDir(env.demoDir(), "01-a.md"), "attempt-001.jsonl.gz", "claude", 1, base, "failed", 30_000)

	var buf bytes.Buffer
	err := StreamRawWith(env.deps(), nil, nil, StreamOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "demo",
	}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	// Verify the order of header records in the raw output.
	starts := rawHeaderStartTimes(t, out)
	if len(starts) != 2 {
		t.Fatalf("expected 2 header start times, got %d:\n%s", len(starts), out)
	}
	if !starts[0].Equal(base.Add(-5 * time.Minute)) {
		t.Fatalf("first header start = %v, want %v", starts[0], base.Add(-5*time.Minute))
	}
	if !starts[1].Equal(base) {
		t.Fatalf("second header start = %v, want %v", starts[1], base)
	}

	// Delimiter separates the two runs.
	if strings.Count(out, `"type":"delimiter"`) != 1 {
		t.Fatalf("expected 1 delimiter:\n%s", out)
	}
}

// rawHeaderStartTimes extracts the start_time from every header record in raw
// JSONL output, in order.
func rawHeaderStartTimes(t *testing.T, out string) []time.Time {
	t.Helper()
	var starts []time.Time
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec struct {
			Type      string    `json:"type"`
			StartTime time.Time `json:"start_time"`
		}
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if rec.Type == "header" {
			starts = append(starts, rec.StartTime)
		}
	}
	return starts
}

func TestStreamRawLastBareSetReturnsLatestRun(t *testing.T) {
	env := streamFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	writeTimingStream(t, taskStreamDir(env.demoDir(), "01-a.md"), "attempt-001.jsonl.gz", "claude", 1, base.Add(-5*time.Minute), "failed", 30_000)
	writeTimingStream(t, taskStreamDir(env.demoDir(), "02-b.md"), "attempt-001.jsonl.gz", "claude", 1, base, "completed", 60_000)

	var buf bytes.Buffer
	err := StreamRawWith(env.deps(), nil, nil, StreamOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "demo",
		Last:         true,
	}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	if strings.Contains(out, `"type":"delimiter"`) {
		t.Fatalf("--last --raw should emit no delimiter:\n%s", out)
	}
	starts := rawHeaderStartTimes(t, out)
	if len(starts) != 1 || !starts[0].Equal(base) {
		t.Fatalf("--last --raw starts = %v, want latest %v", starts, base)
	}
}

func TestStreamRawEmptyNoCapturedStreams(t *testing.T) {
	env := streamFixture(t)

	var buf bytes.Buffer
	err := StreamRawWith(env.deps(), nil, nil, StreamOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "demo",
	}, &buf)
	if err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if !strings.Contains(out, "no captured attempt streams for demo") {
		t.Fatalf("expected 'no captured attempt streams' message, got:\n%s", out)
	}
}

func TestStreamRawRoundTripStoredGzip(t *testing.T) {
	env := streamFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	streamDir := taskStreamDir(env.demoDir(), "01-a.md")
	writeTimingStream(t, streamDir, "attempt-001.jsonl.gz", "claude", 1, base, "completed", 60_000)

	// Read the stored gzip and decompress to get the original JSONL.
	rawPath := filepath.Join(streamDir, "attempt-001.jsonl.gz")
	rawGz, err := os.ReadFile(rawPath)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(rawGz))
	if err != nil {
		t.Fatal(err)
	}
	originalJSONL, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	_ = zr.Close()

	// StreamRawWith should emit exactly the same decompressed bytes.
	var buf bytes.Buffer
	err = StreamRawWith(env.deps(), nil, nil, StreamOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "demo/01-a.md",
	}, &buf)
	if err != nil {
		t.Fatal(err)
	}

	got := strings.TrimSpace(buf.String())
	want := strings.TrimSpace(string(originalJSONL))
	if got != want {
		t.Fatalf("raw bytes round-trip mismatch:\ngot:  %q\nwant: %q", got, want)
	}

	// Re-compress the raw output and verify it decompresses back to the same bytes,
	// confirming the gzip round-trip is lossless.
	var reGz bytes.Buffer
	zw := gzip.NewWriter(&reGz)
	if _, err := zw.Write([]byte(buf.String())); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	zr2, err := gzip.NewReader(&reGz)
	if err != nil {
		t.Fatal(err)
	}
	reRead, err := io.ReadAll(zr2)
	if err != nil {
		t.Fatal(err)
	}
	_ = zr2.Close()

	if string(reRead) != buf.String() {
		t.Fatalf("re-compress/re-decompress round-trip mismatch:\ngot:  %q\nwant: %q", string(reRead), buf.String())
	}
}

func TestStreamRawRejectsInvalidTargets(t *testing.T) {
	env := streamFixture(t)

	tests := []struct {
		name   string
		target string
		code   int
	}{
		{"unknown set", "missing", ExitNoRunnable},
		{"unknown task file", "demo/99-z.md", ExitNoRunnable},
		{"bare filename", "01-a.md", ExitSetup},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := StreamRawWith(env.deps(), nil, nil, StreamOptions{
				ResolveInput: ResolveInput{CWD: env.root},
				Target:       tc.target,
			}, &buf)
			assertExitCode(t, err, tc.code)
		})
	}
}

func TestStreamLastMultiAttemptTask(t *testing.T) {
	env := streamFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	streamDir := taskStreamDir(env.demoDir(), "01-a.md")

	// Three attempts for task 01-a with different start times.
	writeTimingStream(t, streamDir, "attempt-001.jsonl.gz", "claude", 1, base.Add(-20*time.Minute), "failed", 30_000)
	writeTimingStream(t, streamDir, "attempt-002.jsonl.gz", "claude", 2, base.Add(-10*time.Minute), "failed", 45_000)
	writeTimingStream(t, streamDir, "attempt-003.jsonl.gz", "claude", 3, base, "completed", 60_000)

	// Without --last we get all three.
	result, err := StreamWith(env.deps(), nil, nil, StreamOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "demo/01-a.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tasks[0].Attempts) != 3 {
		t.Fatalf("without --last: expected 3 attempts, got %d", len(result.Tasks[0].Attempts))
	}

	// With --last we get only the most recent (attempt-003, completed).
	result, err = StreamWith(env.deps(), nil, nil, StreamOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "demo/01-a.md",
		Last:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tasks[0].Attempts) != 1 {
		t.Fatalf("with --last: expected 1 attempt, got %d", len(result.Tasks[0].Attempts))
	}
	last := result.Tasks[0].Attempts[0]
	if last.Timing.Outcome != "completed" {
		t.Fatalf("with --last: expected outcome 'completed', got %q", last.Timing.Outcome)
	}
	if !last.Timing.Start.Equal(base) {
		t.Fatalf("with --last: expected start %v, got %v", base, last.Timing.Start)
	}

	// Render output should only mention the most recent attempt.
	var buf bytes.Buffer
	RenderStream(&buf, result, RenderStreamOptions{})
	out := buf.String()
	if strings.Count(out, "Attempt starting") != 1 {
		t.Fatalf("with --last: expected 1 'Attempt starting' line, got %d:\n%s", strings.Count(out, "Attempt starting"), out)
	}
	if strings.Contains(out, "failed") {
		t.Fatalf("with --last: output should not contain 'failed':\n%s", out)
	}
}

func TestStreamLastBareSet(t *testing.T) {
	env := streamFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	// Task 01-a: two attempts, the latest at base.
	dirA := taskStreamDir(env.demoDir(), "01-a.md")
	writeTimingStream(t, dirA, "attempt-001.jsonl.gz", "claude", 1, base.Add(-20*time.Minute), "failed", 30_000)
	writeTimingStream(t, dirA, "attempt-002.jsonl.gz", "claude", 2, base, "completed", 60_000)

	// Task 02-b: one attempt earlier than 01-a's latest.
	dirB := taskStreamDir(env.demoDir(), "02-b.md")
	writeTimingStream(t, dirB, "attempt-001.jsonl.gz", "claude", 1, base.Add(-10*time.Minute), "failed", 15_000)

	// With --last at set scope, only the single most recent run overall is returned.
	result, err := StreamWith(env.deps(), nil, nil, StreamOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "demo",
		Last:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tasks) != 1 {
		t.Fatalf("expected 1 task (the one with the latest run), got %d", len(result.Tasks))
	}
	if result.Tasks[0].TaskID != "01-a" {
		t.Fatalf("expected latest run from 01-a, got %s", result.Tasks[0].TaskID)
	}
	if len(result.Tasks[0].Attempts) != 1 {
		t.Fatalf("expected 1 attempt with --last, got %d", len(result.Tasks[0].Attempts))
	}
	last := result.Tasks[0].Attempts[0]
	if last.Timing.Outcome != "completed" {
		t.Fatalf("expected 'completed', got %q", last.Timing.Outcome)
	}
	if !last.Timing.Start.Equal(base) {
		t.Fatalf("expected latest start %v, got %v", base, last.Timing.Start)
	}
}

func TestStreamLastWithRaw(t *testing.T) {
	env := streamFixture(t)
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	streamDir := taskStreamDir(env.demoDir(), "01-a.md")

	// Two attempts: --last should emit only the most recent.
	writeTimingStream(t, streamDir, "attempt-001.jsonl.gz", "claude", 1, base.Add(-10*time.Minute), "failed", 45_000)
	writeTimingStream(t, streamDir, "attempt-002.jsonl.gz", "claude", 2, base, "completed", 60_000)

	var buf bytes.Buffer
	err := StreamRawWith(env.deps(), nil, nil, StreamOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "demo/01-a.md",
		Last:         true,
	}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	// Should contain only the latest attempt (attempt=2).
	if !strings.Contains(out, `"attempt":2`) {
		t.Fatalf("--last --raw output should contain attempt 2:\n%s", out)
	}
	if strings.Contains(out, `"attempt":1`) {
		t.Fatalf("--last --raw output should NOT contain attempt 1:\n%s", out)
	}
	// No delimiter needed for a single attempt.
	if strings.Contains(out, "delimiter") {
		t.Fatalf("--last --raw output should not contain delimiter:\n%s", out)
	}
}

func TestStreamLastWithFull(t *testing.T) {
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	// Build a result with two attempts and apply --last filtering like StreamWith does.
	result := &StreamResult{
		TaskSetID: "demo",
		Tasks: []TaskStream{
			{
				TaskID: "01-a",
				File:   "01-a.md",
				Title:  "A",
				Attempts: []AttemptStream{
					{
						Timing: AttemptTiming{Agent: "claude", Start: base.Add(-10 * time.Minute), Outcome: "failed", Duration: 45 * time.Second},
						Events: []StreamEvent{{AtMS: 100, Type: "tool_result", Text: strings.Repeat("long output line\n", 50)}},
					},
					{
						Timing: AttemptTiming{Agent: "claude", Start: base, Outcome: "completed", Duration: 60 * time.Second},
						Events: []StreamEvent{{AtMS: 100, Type: "tool_result", Text: "short result"}},
					},
				},
			},
		},
	}

	// Simulate --last filtering.
	result.Tasks[0].Attempts = result.Tasks[0].Attempts[len(result.Tasks[0].Attempts)-1:]

	// Render with --full (no truncation).
	var buf bytes.Buffer
	RenderStream(&buf, result, RenderStreamOptions{Full: true})
	out := buf.String()

	// Should have only the latest attempt (completed).
	if !strings.Contains(out, "completed") {
		t.Fatalf("--last --full output should contain 'completed':\n%s", out)
	}
	if strings.Contains(out, "failed") {
		t.Fatalf("--last --full output should NOT contain 'failed':\n%s", out)
	}
	// Full mode renders the payload verbatim.
	if strings.Contains(out, "elided") {
		t.Fatalf("--last --full should not contain elision marker:\n%s", out)
	}
}

func TestHumanizeBytes(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{2 * 1024 * 1024, "2.0 MB"},
	}
	for _, tc := range cases {
		got := humanizeBytes(tc.n)
		if got != tc.want {
			t.Errorf("humanizeBytes(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}
